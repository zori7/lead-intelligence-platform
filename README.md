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

## Tests

```bash
make test-go     # Go unit tests + store integration tests (creates the leadintel_test database)
make test-web    # svelte-check
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
backend/    Go API (cmd/api, internal/{config,db,httpx,apperr,lead}, migrations)
frontend/   SvelteKit SPA
SPEC.md     Technical specification
```
