# Lead Intelligence Platform

A lead management dashboard with on-demand AI analysis. See [SPEC.md](SPEC.md) for the full technical specification and roadmap.

**Implemented:** lead management (create, list, filter, search, edit, change status, delete); AI analysis jobs over the REST API, run by a LangGraph workflow that grounds company insights in the lead's website (SPEC.md §9.7); per-step LLM model settings through the token-protected admin API. Live progress, the analysis UI and the admin page follow the roadmap in SPEC.md §14.

> Lead endpoints have no user authentication. The application is designed as a local, single-user tool (SPEC.md §12). Admin endpoints require `ADMIN_TOKEN`.

## Stack

| Layer | Technology |
|---|---|
| Frontend | SvelteKit (SPA mode) · Svelte 5 · TypeScript · TanStack Query · Tailwind CSS v4 · shadcn-svelte |
| API | Go · chi · pgx · goose |
| AI service | Python · LangGraph · LangChain (OpenAI-compatible client) · httpx |
| Go ↔ Python | gRPC / Protobuf (server-streaming) |
| Database | PostgreSQL 17 |
| LLM | Any OpenAI-compatible server (Ollama, LM Studio, vLLM, llama.cpp), configured by URL |

## Getting started

Requirements: Docker, Go 1.27+, Node.js 22+, [uv](https://docs.astral.sh/uv/) (installs Python 3.14), and an OpenAI-compatible LLM server.

```bash
cp .env.example .env          # set ADMIN_TOKEN (openssl rand -hex 24) and LLM_BASE_URL
make up                       # PostgreSQL in Docker
make ai                       # AI service on :50051 (gRPC)
make api                      # Go API on :8080 (applies migrations on start)
cd frontend && npm install && npm run dev   # UI on http://localhost:5173 (proxies /api → :8080)
```

Analyses are blocked until a default model is chosen. Until the admin page exists (M6), set it through the admin API:

```bash
source .env
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" localhost:8080/api/admin/llm-models | jq   # models on your LLM server
curl -s -X PUT localhost:8080/api/admin/llm-settings \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' -d '{
    "default": {"model": "<model id>", "temperature": 0.2},
    "nodes": {
      "extract_company": {"model": null, "temperature": null},
      "analyze_fit": {"model": null, "temperature": null},
      "analyze_intent": {"model": null, "temperature": null},
      "generate_recommendations": {"model": null, "temperature": null}
    }}' | jq
```

`null` inherits the default; a model or temperature on a step overrides it. Saved models must be listed by the LLM server.

**Website fetch.** When a lead has a website, the AI service fetches that one page (no crawling, no JavaScript) and uses its text for the company profile. Requests are limited to public addresses on ports 80/443, at most 3 redirects, 10 s and 1 MB; fetched content is not stored. Set `WEB_FETCH_ENABLED=false` to work offline (SPEC.md §9.7, §12).

`make help` lists all targets.

### Coding agents (CodeGraph)

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

## API contract

[`api/openapi.yaml`](api/openapi.yaml) is the single source of truth for the REST API. From it, `make openapi` generates:

- **Go** (`backend/internal/api`): request/response types, the chi routing interface handlers must implement, and an embedded copy of the spec that server-side validation reads its limits, enums and patterns from.
- **TypeScript** (`frontend/src/lib/api/gen`): types and zod schemas used by the API client and forms.

Generated code is committed. `make openapi-check` fails if it is out of date, and a Go contract test validates every handler response against the spec.

## Tests

```bash
make test-go     # Go unit tests + store integration tests (creates the leadintel_test database)
make test-ai     # Python tests: fake LLM, website fixtures, in-process gRPC (no network)
make test-web    # svelte-check
make openapi-check  # generated API code matches api/openapi.yaml
make proto-check    # generated gRPC code matches proto/
```

Store integration tests run against `TEST_DATABASE_URL` and are skipped when it is not set.

Website tests replay fixtures through `httpx.MockTransport`. The repository contains only synthetic pages for fictional companies (`ai/tests/fixtures/websites/`). To also test against real sites, record some locally; they are saved to a git-ignored directory and picked up automatically:

```bash
make record-websites                                             # default list
cd ai && uv run python scripts/record_websites.py https://example.com/   # specific URLs
```

## API walkthrough

```bash
API=http://localhost:8080/api

# Create
ID=$(curl -s -X POST $API/leads -H 'Content-Type: application/json' \
  -d '{"company_name":"Acme Corp","website":"https://acme.io","contact_name":"Ann Lee","industry":"SaaS","company_size":"51-200"}' \
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
curl -s $API/analyses/$JOB | jq '{status, progress, steps}'   # poll until succeeded/failed
curl -s $API/analyses/$JOB | jq '.result | {score, website: .meta.website, recommendations}'
curl -s "$API/leads/$ID/analyses?limit=5" | jq '.items[] | {id, status}'

# Delete → 204
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE $API/leads/$ID
```

Errors use one envelope: `{"error": {"code": "...", "message": "...", "fields": {...}}}` (SPEC.md §7.1).

## Repository layout

```
backend/    Go API (cmd/api, internal/{api,config,db,httpx,apperr,lead,analysis,llmsettings,aiclient}, migrations, gen)
ai/         Python AI service (gRPC server, LangGraph workflow, website fetch, tests)
frontend/   SvelteKit SPA
api/        OpenAPI contract (source of truth for generated Go/TS code)
proto/      gRPC contract (source of truth for generated Go/Python code)
SPEC.md     Technical specification
AGENTS.md   Coding rules
```
