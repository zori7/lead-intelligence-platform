# Lead Intelligence Platform

A lead management dashboard with on-demand AI analysis. A LangGraph workflow reads each lead's website, profiles the company, scores fit and buying intent against your ideal customer profile, and suggests next steps. Progress streams live to the browser. Everything runs locally against any OpenAI-compatible LLM server.

[SPEC.md](SPEC.md) is the full technical specification: architecture (§3), API (§7), gRPC contract (§8), workflow (§9), security (§12) and design decisions (§15).

> Lead endpoints have no user authentication. The application is designed as a local, single-user tool (SPEC.md §12). Admin endpoints require `ADMIN_TOKEN`.

## Stack

| Layer | Technology |
|---|---|
| Frontend | SvelteKit (SPA mode) · Svelte 5 · TypeScript · TanStack Query · Tailwind CSS v4 · shadcn-svelte · GSAP |
| API | Go · chi · pgx · goose |
| AI service | Python · LangGraph · LangChain (OpenAI-compatible client) · httpx |
| Go ↔ Python | gRPC / Protobuf (server-streaming) |
| Database | PostgreSQL 17 |
| LLM | Any OpenAI-compatible server (Ollama, LM Studio, vLLM, llama.cpp), configured by URL |

```
Browser ──HTTP/SSE──▶ nginx (web) ──/api──▶ Go API ──gRPC stream──▶ Python AI ──▶ LLM server
                                              │                         └──────▶ lead's website
                                              ▼
                                          PostgreSQL
```

Go is the only service that writes to the database; the Python service is stateless and returns results on the stream Go opened (SPEC.md §3, §8.2).

## Quick start (Docker)

Requirements: Docker with Compose v2, and an OpenAI-compatible LLM server.

### 1. Start an LLM server

Any OpenAI-compatible server works. With [Ollama](https://ollama.com):

```bash
ollama pull qwen3:8b      # or any instruction-tuned model; larger models give better analyses
ollama serve              # listens on :11434 (the desktop app starts it automatically)
```

The workflow asks for structured output (JSON schema). Models from roughly 7–8B parameters work; 14B and above give noticeably better reasoning. LM Studio, vLLM and llama.cpp's `llama-server` work the same way: note the base URL ending in `/v1`.

### 2. Configure and start the stack

```bash
cp .env.example .env
# Edit .env:
#   ADMIN_TOKEN=   output of: openssl rand -hex 24
#   LLM_BASE_URL=  http://host.docker.internal:11434/v1   (default; matches Ollama on this machine)
docker compose up --build          # or: make stack (detached, waits until healthy)
```

Open <http://localhost:5173>.

### 3. Choose a model

Analyses are blocked until a default model is chosen. Open <http://localhost:5173/admin>, enter `ADMIN_TOKEN`, pick a default model from the list your LLM server reports, and save. Each workflow step can override the model and temperature; empty fields inherit the default.

Then create a lead with a website and click **Analyze**.

### What runs

| Service | Image | Host port | Notes |
|---|---|---|---|
| `web` | nginx (non-root) serving the static SPA | `127.0.0.1:5173` | Proxies `/api` to Go, SSE-safe |
| `api` | Go binary on distroless (non-root) | `127.0.0.1:8080` | Applies migrations on start |
| `ai` | Python 3.14 slim (non-root) | `127.0.0.1:50051` | gRPC; reached by `api` on the compose network |
| `postgres` | `postgres:17-alpine` | `127.0.0.1:5432` | Data in the `pgdata` volume |

All ports bind to localhost only. `api`, `ai` and `postgres` are published for the host-run development workflow below; the browser only needs `web`. Startup is ordered by health checks: `postgres` and `ai` → `api` → `web`. Ports are configurable with `WEB_PORT`, `API_PORT`, `AI_PORT` and `POSTGRES_PORT` in `.env`.

`docker compose down` stops the stack; `docker compose down -v` also deletes the database.

## Development

Requirements: Docker, Go 1.27+, Node.js 24+ (npm 11), [uv](https://docs.astral.sh/uv/) (installs Python 3.14), and an LLM server.

Run the services on the host with hot reload, with only PostgreSQL in Docker:

```bash
cp .env.example .env          # set ADMIN_TOKEN; set LLM_BASE_URL=http://localhost:11434/v1 for a host-run AI service
make up                       # PostgreSQL in Docker
make ai                       # AI service on :50051 (gRPC)
make api                      # Go API on :8080 (applies migrations on start)
cd frontend && npm install && npm run dev   # UI on http://localhost:5173 (Vite proxies /api → :8080)
```

Mixing is fine: `docker compose up -d postgres ai` runs the AI service in Docker while `make api` and the Vite dev server run on the host. `make help` lists all targets.

### Configuration

All settings are environment variables, documented in [`.env.example`](.env.example) and SPEC.md §13.1. Each service validates its configuration at startup and exits with a clear message when it is invalid. Which model each workflow step uses is not configuration: it is runtime data set on `/admin`.

### API contract

[`api/openapi.yaml`](api/openapi.yaml) is the single source of truth for the REST API. From it, `make openapi` generates:

- **Go** (`backend/internal/api`): request/response types, the chi routing interface handlers must implement, and an embedded copy of the spec that server-side validation reads its limits, enums and patterns from.
- **TypeScript** (`frontend/src/lib/api/gen`): types and zod schemas used by the API client and forms.

The gRPC contract in [`proto/`](proto/) is generated the same way with `make proto`. Generated code is committed; `make openapi-check` and `make proto-check` fail if it is out of date, and a Go contract test validates every handler response against the spec. [AGENTS.md](AGENTS.md) lists the rules for changing the code.

### Tests

```bash
make test           # all of the below
make test-go        # Go unit, contract and store integration tests (creates the leadintel_test database)
make test-ai        # Python tests: fake LLM, website fixtures, in-process gRPC (no network)
make test-web       # svelte-check + vitest
make lint           # generated-code freshness, go vet, ruff, svelte-check
```

`make test-go` needs PostgreSQL (`make up`). Store integration tests run against `TEST_DATABASE_URL` and are skipped when it is not set.

## Website fetch

When a lead has a website, the AI service fetches **that one page** and uses its text to ground the company profile (SPEC.md §9.7).

What it does:

- One `GET` per analysis, triggered by a user action. It honors `robots.txt` and identifies itself as `LeadIntelBot`.
- Accepts `http`/`https` on ports 80 and 443 only, follows at most 3 redirects, stops after 10 s or 1 MB, and accepts HTML only.
- Refuses any address that resolves to loopback, private, link-local or other non-public ranges, on the first request and on every redirect. This keeps lead URLs from reaching the compose network, the host, or cloud metadata endpoints.

What it does not do:

- No crawling, no JavaScript, no cookies or authentication. Client-rendered sites with almost no server-side text are reported as "website could not be read", and the analysis continues from the lead fields alone.
- No lead data is sent to the website, and fetched content is never stored or logged. The site's operator can see that this machine requested the page.

Set `WEB_FETCH_ENABLED=false` to turn it off, for example when working offline.

### Recording real sites for tests

The repository contains only synthetic pages for fictional companies (`ai/tests/fixtures/websites/`). To also test against real sites, record some locally. Recordings go to the git-ignored `ai/tests/fixtures/websites/recorded/` and are picked up by the `recorded` tests automatically:

```bash
make record-websites                                                  # default list
cd ai && uv run python scripts/record_websites.py https://example.com/   # specific URLs
```

## API walkthrough

```bash
API=http://localhost:5173/api          # through nginx; http://localhost:8080/api reaches Go directly

# Create
ID=$(curl -s -X POST $API/leads -H 'Content-Type: application/json' \
  -d '{"company_name":"Acme Corp","website":"https://acme.example","contact_name":"Ann Lee","industry":"SaaS","company_size":"51-200"}' \
  | jq -r .id)

# List (filters: status, q, sort=created_desc|score_desc, limit, offset)
curl -s "$API/leads?q=acme&status=new" | jq

# Get (lead + latest analysis)
curl -s $API/leads/$ID | jq

# Partial update: absent = unchanged, null = clear
curl -s -X PATCH $API/leads/$ID -H 'Content-Type: application/json' \
  -d '{"status":"qualified","industry":null}' | jq

# Validation error → 400 with per-field messages
curl -s -X POST $API/leads -H 'Content-Type: application/json' -d '{"company_name":"","status":"archived"}' | jq

# Analyze (202 new job, 200 if one is already active, 409 until a default model is set)
JOB=$(curl -s -X POST $API/leads/$ID/analyses | jq -r .id)
curl -sN $API/analyses/$JOB/events              # live progress (Server-Sent Events) until completed/failed
curl -s $API/analyses/$JOB | jq '.result | {score, website: .meta.website, recommendations}'
curl -s "$API/leads/$ID/analyses?limit=5" | jq '.items[] | {id, status}'
curl -s -X POST $API/analyses/$JOB/cancel | jq '.status'   # cancel a queued or running job

# Delete → 204
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE $API/leads/$ID
```

Model settings can also be managed without the UI:

```bash
source .env
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" $API/admin/llm-models | jq     # models on the LLM server
curl -s -X PUT $API/admin/llm-settings \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' -d '{
    "default": {"model": "<model id>", "temperature": 0.2},
    "nodes": {
      "extract_company": {"model": null, "temperature": null},
      "analyze_fit": {"model": null, "temperature": null},
      "analyze_intent": {"model": null, "temperature": null},
      "generate_recommendations": {"model": null, "temperature": null}
    }}' | jq
```

Errors use one envelope: `{"error": {"code": "...", "message": "...", "fields": {...}}}` (SPEC.md §7.1).

## Troubleshooting

**The admin page reports "LLM server unreachable", or analyses fail with "AI provider failed".** Inside a container, `localhost` is the container itself. Use `LLM_BASE_URL=http://host.docker.internal:11434/v1` for a server on this machine (Compose maps that name to the host on Linux too). The server must listen on an address Docker can reach: Ollama does by default on macOS and Windows; on Linux, start it with `OLLAMA_HOST=0.0.0.0`. The `ai` service checks the server at startup and logs a warning when it cannot reach it: `docker compose logs ai`.

**Analyses keep failing with "AI provider failed" although the server is reachable.** Not every server or model supports JSON-schema structured output; the `ai` logs then show rejected requests or unparseable responses. Set `LLM_STRUCTURED_OUTPUT_METHOD=function_calling`, or `json_mode` as a last resort, then `docker compose up -d ai`. A larger model also helps.

**Analyses time out.** Local models can be slow on CPU. Raise `ANALYSIS_TIMEOUT` (per job, default `300s`) and `LLM_TIMEOUT_SECONDS` (per call, default `120`), or lower `ANALYSIS_WORKERS` so fewer jobs compete for the model.

**Insights say "website could not be read".** The page was blocked (by `robots.txt` or the address guard), timed out, was not HTML, or had too little server-rendered text. The analysis still runs from the lead fields. The `ai` logs show the outcome per fetch (host and status only).

**Working offline.** Set `WEB_FETCH_ENABLED=false`. Insights then report the fetch as disabled; everything else works against the local LLM.

**Live progress arrives all at once.** Something between the browser and Go is buffering the event stream. The bundled nginx config disables buffering for `/api/`; a proxy placed in front of it needs the same (`proxy_buffering off`, `proxy_read_timeout 1h`).

**A port is already in use.** Set `WEB_PORT`, `API_PORT`, `AI_PORT` or `POSTGRES_PORT` in `.env`.

**`api` exits at startup.** It validates its configuration and logs every problem at once: `docker compose logs api`. The most common cause is a missing or short `ADMIN_TOKEN` (at least 24 characters).

## Coding agents (CodeGraph)

The repository ships configuration for [CodeGraph](https://github.com/colbymchenry/codegraph), a local code index that lets coding agents look up symbols, callers and call paths instead of grepping:

- `.mcp.json` registers the CodeGraph MCP server.
- `.claude/settings.json` allows its tools and runs `codegraph prompt-hook` on every prompt in Claude Code.
- `codegraph.json` ranks generated code and shadcn components below hand-written code.
- `AGENTS.md` ("Navigating the code") tells agents when to use it.

One-time setup per machine:

```bash
npm i -g @colbymchenry/codegraph   # CLI on PATH (or the installer from the CodeGraph README)
codegraph telemetry off            # optional: disable anonymous usage statistics
codegraph init                     # build the local index in .codegraph/ (gitignored)
```

Then restart Claude Code and approve the `codegraph` server when asked. The index updates itself on file changes; nothing needs re-running.

Notes:

- **Do not run `codegraph install`.** The agent configuration is already committed; the installer would rewrite it.
- **Without the CLI**, Claude Code reports a failing prompt hook on every prompt. Install the CLI, or opt out locally by adding `{ "disableAllHooks": true }` to `.claude/settings.local.json` (ignored by git) and declining the MCP server.
- If `codegraph upgrade` adds a CodeGraph block to `.claude/CLAUDE.md`, delete it; the guidance lives in `AGENTS.md`.

## Repository layout

```
backend/    Go API (cmd/api, internal/{api,config,db,httpx,apperr,lead,analysis,llmsettings,aiclient}, migrations, gen)
ai/         Python AI service (gRPC server, LangGraph workflow, website fetch, tests)
frontend/   SvelteKit SPA, Dockerfile and nginx.conf
api/        OpenAPI contract (source of truth for generated Go/TS code)
proto/      gRPC contract (source of truth for generated Go/Python code)
SPEC.md     Technical specification
AGENTS.md   Coding rules
```
