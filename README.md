# Lead Intelligence Platform

A lead management dashboard with on-demand AI analysis. See [SPEC.md](SPEC.md) for the full technical specification and roadmap.

**Implemented:** lead management (create, list, filter, search, edit, change status, delete). AI analysis, live progress and the admin page follow the roadmap in SPEC.md §14.

> Lead endpoints have no user authentication. The application is designed as a local, single-user tool (SPEC.md §12).

## Stack

| Layer | Technology |
|---|---|
| Frontend | SvelteKit (SPA mode) · Svelte 5 · TypeScript · TanStack Query · Tailwind CSS v4 · shadcn-svelte |
| API | Go · chi · pgx · goose |
| Database | PostgreSQL 17 |

## Getting started

Requirements: Docker, Go 1.27+, Node.js 22+.

```bash
cp .env.example .env
make up                       # PostgreSQL in Docker
make api                      # Go API on :8080 (applies migrations on start)
cd frontend && npm install && npm run dev   # UI on http://localhost:5173 (proxies /api → :8080)
```

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
make test-web    # svelte-check
make openapi-check  # generated API code matches api/openapi.yaml
```

Store integration tests run against `TEST_DATABASE_URL` and are skipped when it is not set.

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

# Delete → 204
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE $API/leads/$ID
```

Errors use one envelope: `{"error": {"code": "...", "message": "...", "fields": {...}}}` (SPEC.md §7.1).

## Repository layout

```
backend/    Go API (cmd/api, internal/{api,config,db,httpx,apperr,lead}, migrations)
frontend/   SvelteKit SPA
api/        OpenAPI contract (source of truth for generated Go/TS code)
SPEC.md     Technical specification
AGENTS.md   Coding rules
```
