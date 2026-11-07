# Lead Intelligence Platform — Technical Specification (MVP)

| | |
|---|---|
| **Status** | Draft for approval |
| **Date** | 2026-11-06 |
| **Scope** | MVP: a focused, production-minded vertical slice |
| **Stack** | Svelte 5 + SvelteKit (SPA mode) + TS + Vite + TanStack Query + Tailwind v4 / shadcn-svelte + GSAP · Go + Chi + pgx · Python + LangGraph · gRPC/Protobuf · PostgreSQL · Docker Compose |
| **LLM** | Local OpenAI-compatible server (e.g. Ollama, LM Studio, vLLM, llama.cpp), endpoint configured via `LLM_BASE_URL`. **Per-node models are configured at runtime on the superadmin page** (§7.5, §10). |

---

## 1. Product Overview and Goals

**Lead Intelligence Platform** is a lead management dashboard. Users manage sales leads and can ask an AI to analyze a lead. The analysis returns company insights, a quality score, and recommended next actions. Progress shows up live while the analysis runs.

### Goals

1. **Lead management:** create, view, edit, and delete leads, and track each lead's pipeline status.
2. **AI analysis on demand:** one click starts a structured analysis. The result covers company profile, ICP fit, buying intent, a 0–100 score with a tier, and recommendations.
3. **Live feedback:** the user watches the analysis move through its steps in real time, without refreshing.
4. **Reliability that fits an MVP:** no duplicate runs, clear failure states, safe retries, and recovery after restarts.
5. **Explainable architecture:** every significant technology choice has a documented justification and trade-off (see §15).

### Success criteria (MVP)

- A new lead can be created and fully analyzed end-to-end within `ANALYSIS_TIMEOUT` (target: under 90s with a 7–8B local model on a modern laptop).
- Clicking "Analyze" twice never produces two concurrent analyses for the same lead.
- Killing the Python service during an analysis gives a clear, retryable failure in the UI, never a spinner that runs forever.
- The whole system starts with `docker compose up`.

---

## 2. MVP Scope and Explicit Non-Goals

### In scope

- CRUD for leads, with status tracking (`new → contacted → qualified → proposal → won | lost`).
- Lead list with filtering by status, text search, and sorting by created date or AI score.
- Lead detail page with the latest analysis and a short analysis history.
- Async AI analysis jobs. Jobs are persisted, deduplicated, cancellable, and time-limited.
- Real-time progress over SSE.
- **Superadmin page (`/admin`)** to configure which LLM model (and temperature) each LLM workflow step uses: a global default plus optional per-step overrides. Models are picked from the list the LLM server reports. Changes take effect for the next analysis without a restart. Access is protected by an admin token.
- A LangGraph workflow with validation, a website fetch, extraction, parallel analysis, deterministic scoring, recommendations, aggregation, conditional routing, and retries.
- **Website grounding:** when a lead has a `website`, the AI service fetches that one page (the homepage), extracts its readable text, and feeds it to the company extraction step (§9.7). Results state whether they are grounded in the website or inferred from lead data only.
- Docker Compose setup with env-based configuration.

### Non-goals (explicitly out of scope)

| Non-goal | Rationale |
|---|---|
| User accounts, roles, multi-tenancy | Single-user local tool for the MVP. The superadmin area is protected by a single `ADMIN_TOKEN` (§12), not by user accounts. See §12 for the upgrade path. |
| Settings history/audit, per-step LLM endpoints or providers | One endpoint (`LLM_BASE_URL`). Only the model and temperature vary per step. |
| Third-party enrichment (Clearbit, search APIs) and crawling beyond the lead's homepage | Adds keys, rate limits, and legal concerns. The single-page fetch of the lead's own website (§9.7) covers the main grounding need; everything else is inferred from lead data plus model knowledge and labeled "AI-inferred". |
| JavaScript rendering (headless browser) for the website fetch | A browser engine in the AI container is a large dependency and attack surface. Pages that render only client-side yield little text, and the analysis falls back to lead data. |
| Message broker (Kafka, RabbitMQ, Redis) | A PostgreSQL-backed job table is enough at this scale (§4.2). |
| Multiple Go API replicas | The design allows it later (§15.7). The MVP runs one instance. |
| Metrics/tracing stack (Prometheus, OTel) | Structured logs with correlation IDs are enough for now. |
| Bulk import, CSV export, email sending, CRM sync | Product features for later. |
| Multi-agent or tool-using agents | One deterministic graph covers the use case. |
| Automatic re-runs of failed analyses | The user retries explicitly. This avoids silent LLM spend. |
| Mobile-first layout | Desktop-first, but the layout must stay usable down to 768px. |

---

## 3. System Architecture

```mermaid
flowchart LR
    subgraph Browser
        UI["Svelte 5 SPA<br/>(TypeScript, GSAP)"]
    end

    subgraph web["web container (nginx)"]
        STATIC["Static assets"]
        PROXY["/api reverse proxy<br/>(buffering off for SSE)"]
    end

    subgraph api["api container (Go)"]
        HTTP["Chi REST handlers"]
        SSE["SSE handler"]
        BROKER["In-memory event broker"]
        WORKERS["Job workers (N)"]
        GRPCC["gRPC client"]
    end

    subgraph ai["ai container (Python)"]
        GRPCS["gRPC server (grpc.aio)"]
        GRAPH["LangGraph workflow"]
    end

    DB[(PostgreSQL)]
    LLM["OpenAI-compatible LLM API"]
    SITE["Lead's website<br/>(public internet)"]

    UI -- "REST (JSON)" --> PROXY
    UI -- "SSE (EventSource)" --> PROXY
    PROXY --> HTTP
    PROXY --> SSE
    HTTP -- pgx --> DB
    WORKERS -- "claim / update jobs" --> DB
    WORKERS --> GRPCC
    GRPCC -- "AnalyzeLead (server-streaming)" --> GRPCS
    GRPCS --> GRAPH
    GRAPH -- HTTPS --> LLM
    GRAPH -- "HTTP(S) GET, 1 page" --> SITE
    WORKERS -- publish --> BROKER
    BROKER -- subscribe --> SSE
```

### Analysis request lifecycle

```mermaid
sequenceDiagram
    autonumber
    participant UI as Svelte UI
    participant API as Go API (HTTP)
    participant DB as PostgreSQL
    participant W as Go Worker
    participant AI as Python AI (gRPC)
    participant LLM as LLM Provider
    participant WEB as Lead website

    UI->>API: POST /api/leads/{id}/analyses
    API->>DB: INSERT analysis_jobs (status=queued)<br/>(partial unique index: 1 active job per lead)
    API-->>UI: 202 Accepted {job} (or 200 with existing active job)
    API->>W: wake signal (non-blocking)
    UI->>API: GET /api/analyses/{jobId}/events (SSE)
    API-->>UI: event: snapshot (current job state from DB)
    W->>DB: claim job: queued → running (FOR UPDATE SKIP LOCKED)
    W->>AI: AnalyzeLead(job_id, lead) with deadline
    opt lead has a website
        AI->>WEB: GET homepage (guarded, size- and time-limited)
        AI-->>W: AnalysisEvent{progress: fetch_website}
    end
    loop each LLM workflow step
        AI->>LLM: chat completion (structured output)
        AI-->>W: AnalysisEvent{progress}
        W->>DB: UPDATE steps / progress
        W->>API: broker.Publish(jobId, event)
        API-->>UI: event: progress
    end
    AI-->>W: AnalysisEvent{completed, result}
    W->>DB: TX: job=succeeded + result, lead.ai_score/ai_tier
    W->>API: broker.Publish(completed)
    API-->>UI: event: completed → stream closes
```

### Architectural style

- **Three application services plus one database.** Each one exists for a reason: the UI, the system of record and business logic (Go), and AI orchestration (Python). There are no other services.
- **Go is the only writer to PostgreSQL.** Python is stateless and never touches the database.
- **Communication is one-way.** Browser → Go → Python. Python never calls Go. Results come back on the gRPC response stream that Go opened (§8.2).

---

## 4. Service Responsibilities

### 4.1 Frontend (`frontend/`, SvelteKit SPA + Svelte 5)

- Renders the dashboard. Holds no business rules beyond form validation that mirrors server rules.
- Calls Go over REST through TanStack Query (caching, invalidation, mutations) and subscribes to job progress over SSE (`EventSource`). SSE events are written into the same query cache.
- Owns the visual and motion system (GSAP), honoring `prefers-reduced-motion`.

### 4.2 Go API (`backend/`)

The **system of record** and **job orchestrator**.

- REST API for leads and analyses. Handles validation, persistence, and error mapping.
- **Job lifecycle:** creates jobs, deduplicates them, claims them, dispatches them to Python, persists progress and results, and handles cancellation, timeouts, and startup recovery.
- **Job queue = the `analysis_jobs` table.** `ANALYSIS_WORKERS` goroutines (default 2, sized for a local LLM) claim queued jobs with `SELECT … FOR UPDATE SKIP LOCKED`. When a job is created, a non-blocking signal on a `chan struct{}` (capacity 1) wakes them. A 5s ticker is the fallback poll. One mechanism covers concurrency limits, crash recovery, and future horizontal scaling.
- **Event broker:** an in-memory pub/sub keyed by job ID that fans job events out to SSE subscribers. Each subscriber has a buffered channel of 16. A slow subscriber is dropped, and the client reconnects and gets a fresh snapshot.
- **LLM settings owner:** stores the default model and per-node model settings (`llm_node_settings`) and serves the token-protected admin API.
  - At dispatch, it **resolves** the effective model and temperature for every LLM node (override, else default) and sends them in the gRPC request.
  - The resolved config is saved on the job for reproducibility.
  - Model discovery is delegated to Python (`ListModels` RPC), so Go never talks to the LLM server.
- Health endpoints: liveness, plus readiness that pings the DB and checks gRPC health of the AI service.

### 4.3 Python AI service (`ai/`)

A **stateless workflow executor**.

- Implements the `AnalysisService` gRPC server (`grpc.aio`).
- Compiles the LangGraph workflow once at startup and runs it per request, streaming progress back as typed events.
- Uses the **per-node model config carried in each request**. It has no model defaults of its own and stores no settings.
- Lists the models available on the LLM server (`ListModels` RPC) for the admin page.
- Owns prompts, LLM client construction (`LLM_BASE_URL`), structured output schemas, LLM-call retries, and the scoring formula.
- Owns the **website fetch** (§9.7): the only service that makes outbound requests to lead websites. Fetched content is used within the run and never stored; only the fetch outcome is returned in `result.meta.website`.
- Keeps only one piece of in-memory state, a set of in-flight `job_id`s, which it uses to reject duplicate execution (`ALREADY_EXISTS`).
- Exposes the standard `grpc.health.v1` health service.

### 4.4 PostgreSQL

- Stores leads, analysis jobs (with results as JSONB), and LLM node settings.
- Enforces invariants: enums via `CHECK`, at most one active job per lead via a partial unique index, cascading deletes.

---

## 5. Repository and Directory Structure

Monorepo with one top-level directory per service. Packages are organized by feature, not by layer.

```
lead-intelligence-platform/
├── SPEC.md
├── README.md
├── AGENTS.md                     # critical coding rules (.claude/CLAUDE.md imports it)
├── Makefile                      # proto, dev, test, lint, up/down shortcuts
├── docker-compose.yml
├── .env.example
├── buf.yaml                      # buf module config (lint + breaking)
├── buf.gen.yaml                  # Go codegen → backend/gen (Python: grpcio-tools → ai/src/lead_ai/gen, see Makefile)
├── api/
│   └── openapi.yaml              # REST contract: source of truth for Go + TS types and validation rules
├── proto/
│   └── leadintel/analysis/v1/analysis.proto
│
├── backend/                      # Go API
│   ├── go.mod
│   ├── Dockerfile
│   ├── cmd/api/main.go           # wiring: config, pool, migrations, router, workers, shutdown
│   ├── migrations/               # goose SQL migrations (embedded)
│   │   ├── 0001_leads.sql
│   │   ├── 0002_analysis_jobs.sql
│   │   └── 0003_llm_node_settings.sql
│   ├── gen/analysis/v1/          # generated protobuf + gRPC (committed)
│   └── internal/
│       ├── config/config.go      # env parsing + validation
│       ├── api/                  # generated from api/openapi.yaml: types, chi ServerInterface, embedded spec (committed)
│       ├── httpx/                # JSON encode/decode, error envelope, middleware (incl. RequireAdmin)
│       ├── lead/                 # model.go, store.go, handler.go, validate.go, rules.go (limits from spec), *_test.go
│       ├── analysis/
│       │   ├── model.go          # Job, JobStatus, Step, Event types
│       │   ├── store.go          # create/claim/progress/complete/fail/cancel/recover
│       │   ├── handler.go        # REST + SSE handlers
│       │   ├── broker.go         # in-memory pub/sub
│       │   ├── worker.go         # worker pool, dispatch, retry, timeout, cancel registry
│       │   └── *_test.go
│       ├── llmsettings/
│       │   ├── model.go          # Node, NodeSetting, Settings, Resolved
│       │   ├── store.go          # Get / Replace (single TX)
│       │   ├── resolve.go        # Resolve(settings) → per-node effective config (pure)
│       │   ├── handler.go        # admin REST: settings, models; public: config status
│       │   └── *_test.go
│       └── aiclient/client.go    # thin gRPC wrapper; proto ↔ domain mapping (AnalyzeLead, ListModels)
│
├── ai/                           # Python AI service
│   ├── pyproject.toml            # uv-managed
│   ├── Dockerfile
│   ├── src/lead_ai/
│   │   ├── main.py               # server bootstrap, signal handling
│   │   ├── config.py             # pydantic-settings
│   │   ├── server.py             # AnalysisService servicer (stream mapping, dedupe, deadlines)
│   │   ├── llm.py                # per-node ChatOpenAI resolution (cached by model+temperature) + retry wrapper; list_models()
│   │   ├── errors.py             # error taxonomy → proto ErrorCode
│   │   ├── web.py                # website fetch: URL/IP guard, robots.txt, limits, HTML → text (§9.7)
│   │   ├── graph/
│   │   │   ├── state.py          # AnalysisState TypedDict + reducers
│   │   │   ├── schemas.py        # Pydantic structured-output models
│   │   │   ├── prompts.py        # prompt templates
│   │   │   ├── nodes.py          # node functions
│   │   │   ├── scoring.py        # deterministic scoring (pure)
│   │   │   └── builder.py        # build_graph() — StateGraph wiring + routers; models come via RunnableConfig (testable)
│   │   └── gen/                  # generated protobuf + gRPC (committed)
│   ├── scripts/
│   │   └── record_websites.py    # one-off: fetch real sites once → tests/fixtures/websites/recorded/ (never run in tests/CI)
│   └── tests/
│       ├── fixtures/websites/    # synthetic pages (committed): <name>/{response.json,body.html}; recorded/ is git-ignored
│       ├── test_scoring.py
│       ├── test_web.py           # fetcher against fixtures via httpx.MockTransport: guard, limits, extraction
│       ├── test_graph.py         # graph runs with GenericFakeChatModel (test fixture only), incl. failure paths
│       └── test_server.py        # servicer with in-process gRPC
│
└── frontend/                     # SvelteKit (SPA mode, ssr=false) + Svelte 5
    ├── package.json
    ├── svelte.config.js          # adapter-static with fallback: 'index.html'
    ├── vite.config.ts            # tailwindcss() + sveltekit(); dev proxy /api → localhost:8080
    ├── tsconfig.json
    ├── components.json           # shadcn-svelte config
    ├── Dockerfile                # build → nginx
    ├── nginx.conf                # SPA fallback + /api proxy (SSE-safe)
    └── src/
        ├── app.html
        ├── app.css               # Tailwind v4 @import + @theme tokens (light/dark)
        ├── routes/
        │   ├── +layout.ts        # export const ssr = false
        │   ├── +layout.svelte    # QueryClientProvider, AppShell, Toaster
        │   ├── +page.svelte      # Leads list
        │   ├── +error.svelte     # Not found / error page
        │   ├── leads/[id]/+page.svelte   # Lead detail + analysis
        │   └── admin/
        │       ├── +layout.svelte    # AdminTokenGate (token prompt, sessionStorage)
        │       └── +page.svelte      # LLM model settings
        └── lib/
            ├── api/
            │   ├── gen/          # generated from api/openapi.yaml: types.gen.ts, zod.gen.ts (committed)
            │   ├── types.ts      # re-exports generated types + frontend-only types
            │   ├── client.ts     # typed fetch wrapper + ApiError
            │   ├── queries.ts    # TanStack Query keys, query + mutation factories
            │   └── live-analysis.ts  # SSE subscription → writes into the query cache
            ├── schemas/{lead,llm-settings}.ts   # form adapters over the generated zod schemas
            ├── admin-token.svelte.ts  # reactive admin session: token in sessionStorage (try/catch), lock/unlock
            ├── motion/
            │   ├── gsap.ts       # gsap setup, defaults, reduced-motion
            │   └── attachments.ts    # {@attach} helpers: fadeIn, stagger, countUp, drawRing
            └── components/
                ├── ui/        # shadcn-svelte generated: button, input, select, textarea, sheet, dialog, alert-dialog, badge, card, table, skeleton, sonner, tooltip
                ├── layout/    AppShell, TopBar
                ├── leads/     KpiStrip, LeadTable, LeadFilters, LeadForm, LeadStatusBadge, LeadStatusSelect
                ├── analysis/  AnalysisPanel, AnalysisProgress, ScoreRing, CompanyInsights, ScoreBreakdown, RecommendationList, AnalysisHistory
                └── admin/     AdminTokenGate, ModelSettingsForm, NodeModelRow, ModelSelect
```

**Generated code is committed.** Docker builds then don't need `buf`, and reviewers can see contract changes in diffs. `make proto` regenerates it.

### Dependency budget

| Service | Runtime dependencies |
|---|---|
| Go | `go-chi/chi/v5`, `jackc/pgx/v5`, `google.golang.org/grpc`, `google.golang.org/protobuf`, `pressly/goose/v3`, `google/uuid`, `oapi-codegen/runtime`, `oapi-codegen/nullable`, `getkin/kin-openapi` (+ stdlib `log/slog`; tool: `oapi-codegen`) |
| Python | `grpcio`, `grpcio-health-checking`, `protobuf`, `langgraph`, `langchain-openai`, `langchain-core`, `pydantic`, `pydantic-settings`, `httpx` (already a transitive dependency of `openai`; listed because `web.py` uses it directly). HTML → text uses stdlib `html.parser` and robots rules use stdlib `urllib.robotparser`: no BeautifulSoup or browser engine. |
| Frontend | `svelte`, `@sveltejs/kit`, `@sveltejs/adapter-static`, `@tanstack/svelte-query`, `tailwindcss` + `@tailwindcss/vite`, shadcn-svelte (copied components on `bits-ui`, `tailwind-variants`, `clsx`, `tailwind-merge`), `@lucide/svelte`, `svelte-sonner`, `zod`, `gsap` (dev: `vite`, `typescript`, `svelte-check`, `vitest`, `prettier-plugin-tailwindcss`, `@hey-api/openapi-ts`) |

Each frontend library has a specific job:

- **SvelteKit** handles routing, typed route params, and URL state.
- **TanStack Query** handles server-state caching, mutations, and invalidation.
- **Tailwind with shadcn-svelte** provides accessible primitives (focus traps, keyboard handling) and a consistent visual language without hand-rolled CSS.

The backend has no ORM. The Python service uses LangChain only as a thin layer.

---

## 6. Database Schema and Relationships

```mermaid
erDiagram
    leads ||--o{ analysis_jobs : "has many"
    leads {
        uuid id PK
        text company_name
        text website
        text contact_name
        text contact_email
        text contact_title
        text industry
        text company_size
        text notes
        text status
        smallint ai_score "denormalized from latest success"
        text ai_tier
        timestamptz last_analyzed_at
        timestamptz created_at
        timestamptz updated_at
    }
    analysis_jobs {
        uuid id PK
        uuid lead_id FK
        text status
        jsonb steps
        smallint progress
        jsonb input_snapshot
        jsonb result
        text error_code
        text error_message
        boolean retryable
        int dispatch_attempts
        jsonb model_config "resolved per-node models at dispatch"
        timestamptz created_at
        timestamptz started_at
        timestamptz finished_at
        timestamptz updated_at
    }
    llm_node_settings {
        text node PK "default | extract_company | analyze_fit | analyze_intent | generate_recommendations"
        text model "NULL = inherit default"
        numeric temperature "NULL = inherit default"
        timestamptz updated_at
    }
```

`llm_node_settings` is a standalone configuration table with no foreign keys. Jobs don't reference it; each job keeps its own **snapshot** in `model_config`, so later settings changes never alter history.

### 6.1 `leads`

| Column | Type | Constraints / Notes |
|---|---|---|
| `id` | `uuid` | PK, `DEFAULT gen_random_uuid()` |
| `company_name` | `text` | `NOT NULL`, 1–200 chars |
| `website` | `text` | nullable, `http(s)` URL, ≤ 300 |
| `contact_name` | `text` | nullable, ≤ 200 |
| `contact_email` | `text` | nullable, basic email format, ≤ 320 |
| `contact_title` | `text` | nullable, ≤ 200 |
| `industry` | `text` | nullable, ≤ 100 |
| `company_size` | `text` | nullable, `CHECK IN ('1-10','11-50','51-200','201-1000','1000+')` |
| `notes` | `text` | nullable, ≤ 5000 chars (free-form context fed to the AI) |
| `status` | `text` | `NOT NULL DEFAULT 'new'`, `CHECK IN ('new','contacted','qualified','proposal','won','lost')` |
| `ai_score` | `smallint` | nullable, 0–100, set on successful analysis |
| `ai_tier` | `text` | nullable, `CHECK IN ('hot','warm','cold')` |
| `last_analyzed_at` | `timestamptz` | nullable |
| `created_at` / `updated_at` | `timestamptz` | `NOT NULL DEFAULT now()` (`updated_at` is set by the application) |

Indexes: `(status)`, `(created_at DESC)`, `(ai_score DESC NULLS LAST)`.

Statuses use `text` + `CHECK` rather than PostgreSQL `ENUM`. Changing a CHECK constraint later is a one-line migration; changing an ENUM is more painful.

### 6.2 `analysis_jobs`

| Column | Type | Constraints / Notes |
|---|---|---|
| `id` | `uuid` | PK. Also the **idempotency key** sent to Python |
| `lead_id` | `uuid` | `NOT NULL REFERENCES leads(id) ON DELETE CASCADE` |
| `status` | `text` | `CHECK IN ('queued','running','succeeded','failed','cancelled')` |
| `steps` | `jsonb` | `NOT NULL DEFAULT '[]'`: `[{ "name": "...", "state": "pending|running|done|skipped|failed" }]` |
| `progress` | `smallint` | `NOT NULL DEFAULT 0`, 0–100 |
| `input_snapshot` | `jsonb` | `NOT NULL`, the lead fields at trigger time (reproducibility) |
| `result` | `jsonb` | nullable, `AnalysisResult` (protojson, snake_case) |
| `error_code` | `text` | nullable (see §11.1) |
| `error_message` | `text` | nullable, user-safe message |
| `retryable` | `boolean` | nullable, tells the UI whether to offer "Retry" |
| `dispatch_attempts` | `int` | `NOT NULL DEFAULT 0` |
| `model_config` | `jsonb` | nullable until claimed. The resolved per-node config sent to Python: `{ "extract_company": { "model": "...", "temperature": 0.2 }, … }` |
| `created_at`, `updated_at` | `timestamptz` | `NOT NULL DEFAULT now()` |
| `started_at`, `finished_at` | `timestamptz` | nullable |

Indexes:

```sql
-- At most ONE active analysis per lead (core dedupe invariant).
CREATE UNIQUE INDEX analysis_jobs_one_active_per_lead
    ON analysis_jobs (lead_id) WHERE status IN ('queued', 'running');

-- Worker claim scan.
CREATE INDEX analysis_jobs_queued ON analysis_jobs (created_at) WHERE status = 'queued';

-- History per lead.
CREATE INDEX analysis_jobs_lead_created ON analysis_jobs (lead_id, created_at DESC);
```

### 6.3 `llm_node_settings`

| Column | Type | Constraints / Notes |
|---|---|---|
| `node` | `text` | PK, `CHECK IN ('default','extract_company','analyze_fit','analyze_intent','generate_recommendations')` |
| `model` | `text` | nullable, ≤ 200. For `default`: `NULL` means **AI not configured yet**. For other nodes: `NULL` means inherit the default. |
| `temperature` | `numeric(3,2)` | nullable, `CHECK (temperature BETWEEN 0 AND 2)`. `NULL` on `default` means 0.2. On other nodes it means inherit. |
| `updated_at` | `timestamptz` | `NOT NULL DEFAULT now()` |

- **Seeding:** the migration inserts all five rows with `NULL` model and temperature. The table always has exactly five rows, so the API only does a full replace and never inserts or deletes.
- **Resolution:** `llmsettings.Resolve` is a pure function, unit-tested. For each of the four LLM nodes, it computes `effective = node override ?? default` for both model and temperature.
- **No model defaults anywhere else.** The model is not set by env vars or in Python code. Until an admin picks a default model, analyses cannot be triggered (§7.3).
- **Concurrency:** settings are read fresh from the DB when a job is claimed. That is one small query per job, with no cache to invalidate. A save is one transaction updating five rows, so a job never sees a half-applied change.

### 6.4 Job state machine

```mermaid
stateDiagram-v2
    [*] --> queued: POST /analyses
    queued --> running: worker claims (SKIP LOCKED)
    queued --> cancelled: user cancels
    running --> succeeded: completed event
    running --> failed: failed event / timeout / AI unavailable / stream broken / restart
    running --> cancelled: user cancels
    succeeded --> [*]
    failed --> [*]
    cancelled --> [*]
```

**Every transition is a conditional UPDATE** (`… WHERE id = $1 AND status = '<expected>'`), and the first writer wins. This settles races such as cancel vs. complete, or timeout vs. complete, without locks held in the application.

### 6.5 Migrations

- `goose` with SQL files embedded via `embed.FS`, applied automatically on API startup (`MIGRATE_ON_START=true` by default).
- `make migrate-new name=...` creates a new file.

---

## 7. REST API

Base path `/api`. JSON over UTF-8. Timestamps are RFC 3339 UTC. IDs are UUID strings. Fields are `snake_case`.

**Contract source of truth:** [`api/openapi.yaml`](api/openapi.yaml). Go types and the chi routing interface, TypeScript types and zod schemas are generated from it (`make openapi`). Go reads field limits, enums and patterns from the embedded spec at startup, and a contract test validates every handler response against it. The types below summarize the contract; when they differ, the spec wins.

### 7.1 Conventions

**Error envelope** (every 4xx/5xx):

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Request validation failed.",
    "fields": { "contact_email": "must be a valid email address" }
  }
}
```

| HTTP | `error.code` | When |
|---|---|---|
| 400 | `invalid_json`, `validation_failed` | Malformed body, field rule violations |
| 401 | `unauthorized` | Missing or wrong admin token on `/api/admin/*` |
| 404 | `not_found` | Unknown lead or job |
| 409 | `invalid_state` | Cancelling a finished job |
| 409 | `ai_not_configured` | Triggering an analysis before a default model is set |
| 413 | `payload_too_large` | Body > 64 KB |
| 422 | `unknown_model` | Saving a model the LLM server doesn't list (`fields` names the node) |
| 500 | `internal` | Unexpected error (details only in logs) |
| 503 | `unavailable` | DB unreachable (readiness) |
| 503 | `llm_unreachable` | Model list unavailable (LLM server or AI service down) |

Request limits: `http.MaxBytesReader` set to 64 KB, and unknown JSON fields rejected (`DisallowUnknownFields`).

### 7.2 Types

```ts
type LeadStatus = 'new' | 'contacted' | 'qualified' | 'proposal' | 'won' | 'lost';
type CompanySize = '1-10' | '11-50' | '51-200' | '201-1000' | '1000+';
type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';
type StepName = 'validate_input' | 'fetch_website' | 'extract_company' | 'analyze_fit'
              | 'analyze_intent' | 'score_lead' | 'generate_recommendations' | 'aggregate';
type StepState = 'pending' | 'running' | 'done' | 'skipped' | 'failed';

interface Lead {
  id: string;
  company_name: string;
  website: string | null;
  contact_name: string | null;
  contact_email: string | null;
  contact_title: string | null;
  industry: string | null;
  company_size: CompanySize | null;
  notes: string | null;
  status: LeadStatus;
  ai_score: number | null;
  ai_tier: 'hot' | 'warm' | 'cold' | null;
  last_analyzed_at: string | null;
  created_at: string;
  updated_at: string;
}

interface AnalysisJob {
  id: string;
  lead_id: string;
  status: JobStatus;
  steps: { name: StepName; state: StepState }[];
  progress: number;                 // 0–100
  result: AnalysisResult | null;    // present when succeeded
  error: { code: string; message: string; retryable: boolean } | null;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

interface AnalysisResult {
  summary: string;
  company: {
    name: string;
    industry: string;
    size_estimate: string;
    business_model: string;
    description: string;
    key_needs: string[];
    confidence: number;            // 0–1
  };
  fit: { score: number; rationale: string; matched_criteria: string[]; gaps: string[] } | null;
  intent: { score: number; rationale: string; signals: string[] } | null;
  score: { value: number; tier: 'hot' | 'warm' | 'cold'; confidence: number;
           breakdown: { fit: number | null; intent: number | null } };
  recommendations: { action: string; rationale: string; priority: 'high' | 'medium' | 'low' }[];
  meta: { node_models: Partial<Record<LlmNode, string>>;  // model actually used per LLM node
          website: WebsiteFetch;           // whether insights are grounded in the lead's website
          partial: boolean; warnings: string[]; duration_ms: number;
          input_tokens: number; output_tokens: number };
}

interface WebsiteFetch {
  status: 'fetched'        // page text was given to extract_company
        | 'not_provided'   // lead has no website
        | 'disabled'       // WEB_FETCH_ENABLED=false
        | 'blocked'        // robots.txt disallows, or the address is not public (§9.7)
        | 'failed';        // network error, timeout, non-2xx, non-HTML, or no readable text
  final_url: string | null;        // after redirects; null unless fetched
}

type LlmNode = 'extract_company' | 'analyze_fit' | 'analyze_intent' | 'generate_recommendations';

interface LlmSettings {
  default: { model: string | null; temperature: number | null };      // model null → AI not configured
  nodes: {
    node: LlmNode;
    model: string | null;              // null → inherit default
    temperature: number | null;        // null → inherit default
    effective_model: string | null;    // resolved, read-only
    effective_temperature: number;     // resolved, read-only
  }[];
  configured: boolean;
  updated_at: string;
}
```

`fit` and `intent` are nullable because the workflow can **degrade gracefully** when one parallel branch fails (`meta.partial = true`). A website fetch that does not succeed is not a degradation (`partial` stays `false`); it is reported through `meta.website.status`.

### 7.3 Endpoints

| Method | Path | Description | Success |
|---|---|---|---|
| `GET` | `/api/leads` | List leads. Query: `status`, `q` (ILIKE on company/contact), `sort` (`created_desc` default, `score_desc`), `limit` (≤100, default 50), `offset` | `200 { items: Lead[], total: number }` |
| `POST` | `/api/leads` | Create lead | `201 Lead` |
| `GET` | `/api/leads/stats` | Pipeline totals for the KPI strip, over all leads (ignores list filters and paging) | `200 { total, analyzed, by_tier: { hot, warm, cold } }` |
| `GET` | `/api/leads/{id}` | Get lead with its latest job | `200 { lead: Lead, latest_analysis: AnalysisJob \| null }` |
| `PATCH` | `/api/leads/{id}` | Partial update (any subset of editable fields, including `status`) | `200 Lead` |
| `DELETE` | `/api/leads/{id}` | Delete lead. Cancels any active job first, then cascades | `204` |
| `POST` | `/api/leads/{id}/analyses` | Trigger analysis (idempotent, see below). Rejected if no default model is configured. | `202 AnalysisJob` (new) / `200 AnalysisJob` (existing active) / `409 ai_not_configured` |
| `GET` | `/api/leads/{id}/analyses` | Analysis history, newest first (`limit` ≤ 20) | `200 { items: AnalysisJob[] }` |
| `GET` | `/api/analyses/{id}` | Get job | `200 AnalysisJob` |
| `POST` | `/api/analyses/{id}/cancel` | Cancel a queued or running job | `200 AnalysisJob` / `409 invalid_state` |
| `GET` | `/api/analyses/{id}/events` | SSE progress stream | `200 text/event-stream` |
| `GET` | `/api/config` | Public app status for the UI (e.g. "AI not configured" banner) | `200 { ai_configured: boolean }` |
| `GET` | `/api/admin/llm-settings` | 🔒 Current default and per-node settings with resolved effective values | `200 LlmSettings` |
| `PUT` | `/api/admin/llm-settings` | 🔒 Replace all settings (see §7.5) | `200 LlmSettings` / `422 unknown_model` / `503 llm_unreachable` |
| `GET` | `/api/admin/llm-models` | 🔒 Models available on the LLM server (via Python `ListModels`) | `200 { items: { id: string }[] }` / `503 llm_unreachable` |
| `GET` | `/healthz` | Liveness (process up) | `200 {"status":"ok"}` |
| `GET` | `/readyz` | DB ping + AI gRPC health | `200` / `503` with per-dependency status |

**Trigger idempotency.** `POST /api/leads/{id}/analyses` inserts a `queued` job. If the partial unique index fires (`23505`), the handler returns **`200` with the existing active job** instead. Double-clicks, retries, and two browser tabs all converge on one job. No `Idempotency-Key` header is needed, because "one active analysis per lead" *is* the idempotency rule.

### 7.4 SSE contract — `GET /api/analyses/{id}/events`

Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`.

| Event | `data` payload | Notes |
|---|---|---|
| `snapshot` | `AnalysisJob` | **Always the first event**, read from the DB. Makes reconnects idempotent: no replay log or `Last-Event-ID` is needed. |
| `progress` | `{ step: StepName, state: StepState, progress: number, message?: string }` | One per step transition |
| `completed` | `AnalysisJob` (with `result`) | Terminal. Server closes the stream. |
| `failed` | `AnalysisJob` (with `error`) | Terminal |
| `cancelled` | `AnalysisJob` | Terminal |
| *(comment)* `: ping` | — | Every 15s keep-alive |

If the job is already terminal when the client connects, the server sends `snapshot` followed by the matching terminal event and then closes. To avoid a gap, the handler **subscribes to the broker before reading the snapshot**.

The `WriteTimeout` on `http.Server` would kill long-lived streams. The SSE handler therefore clears its write deadline with `http.NewResponseController(w).SetWriteDeadline(time.Time{})`.

### 7.5 Admin API (superadmin, 🔒)

**Auth.** Every `/api/admin/*` route sits behind the `httpx.RequireAdmin` middleware:

- It expects `Authorization: Bearer <ADMIN_TOKEN>` and compares with `subtle.ConstantTimeCompare`.
- `ADMIN_TOKEN` is required at startup and must be at least 24 characters, or the API refuses to start.
- A wrong or missing token returns `401 unauthorized`, logged with the request ID but never the token.

**`PUT /api/admin/llm-settings` body:**

```json
{
  "default": { "model": "qwen2.5:14b-instruct", "temperature": 0.2 },
  "nodes": {
    "extract_company":          { "model": "qwen2.5:7b-instruct", "temperature": null },
    "analyze_fit":              { "model": null, "temperature": null },
    "analyze_intent":           { "model": null, "temperature": 0.4 },
    "generate_recommendations": { "model": null, "temperature": 0.6 }
  }
}
```

**Validation:**

- `default.model` is required (non-empty) and must stay set. Clearing it once configured is not allowed.
- All four node keys must be present (full replace). Unknown keys are rejected.
- Temperatures must be between 0 and 2, or `null`.
- Every non-null model must appear in the LLM server's model list. Go calls `ListModels` with a 3s timeout:
  - unknown model → `422 unknown_model`, with `fields` naming the offending node, e.g. `{ "nodes.analyze_fit.model": "not available on LLM server" }`;
  - list unavailable → `503 llm_unreachable`, and nothing is saved.

  Saving only models that exist means a typo can't break every later analysis.
- The write is one transaction across the five rows.

**Effect:** the next job to be **claimed** uses the new settings. Running jobs keep the snapshot they were dispatched with.

---

## 8. gRPC Service Contract

### 8.1 Proto (`proto/leadintel/analysis/v1/analysis.proto`)

```proto
syntax = "proto3";

package leadintel.analysis.v1;

option go_package = "leadintel/backend/gen/analysis/v1;analysisv1";

// Executes AI lead analysis. Stateless apart from in-flight dedupe by job_id.
service AnalysisService {
  // Runs the workflow and streams progress. The stream ends with exactly one
  // terminal event (completed | failed). Transport errors surface as gRPC status.
  rpc AnalyzeLead(AnalyzeLeadRequest) returns (stream AnalysisEvent);

  // Lists models the configured LLM server exposes (GET {LLM_BASE_URL}/models).
  // Used by the admin page and to validate settings on save.
  // UNAVAILABLE if the LLM server cannot be reached.
  rpc ListModels(ListModelsRequest) returns (ListModelsResponse);
}
// Health: standard grpc.health.v1.Health (service name "leadintel.analysis.v1.AnalysisService").

message AnalyzeLeadRequest {
  string job_id = 1;          // UUID; idempotency key for in-flight dedupe
  LeadInput lead = 2;
  repeated NodeModelConfig models = 3;  // fully RESOLVED by Go: exactly one entry per LLM step
}

// Per-node model selection, resolved by Go from llm_node_settings.
// Python has no model defaults: a missing/duplicate/non-LLM step → INVALID_INPUT.
message NodeModelConfig {
  Step step = 1;              // one of EXTRACT_COMPANY, ANALYZE_FIT, ANALYZE_INTENT, GENERATE_RECOMMENDATIONS
  string model = 2;           // non-empty
  float temperature = 3;      // 0–2
}

message ListModelsRequest {}
message ListModelsResponse { repeated ModelInfo models = 1; }
message ModelInfo { string id = 1; }

message LeadInput {
  string company_name = 1;
  string website = 2;         // empty = unknown (proto3 scalars)
  string contact_name = 3;
  string contact_title = 4;
  string industry = 5;
  string company_size = 6;
  string notes = 7;
  // contact_email intentionally NOT sent: not needed for analysis (data minimization).
}

message AnalysisEvent {
  string job_id = 1;
  uint32 sequence = 2;        // monotonically increasing per job
  oneof payload {
    StepProgress progress = 10;
    AnalysisCompleted completed = 11;
    AnalysisFailed failed = 12;
  }
}

enum Step {
  STEP_UNSPECIFIED = 0;
  STEP_VALIDATE_INPUT = 1;
  STEP_EXTRACT_COMPANY = 2;
  STEP_ANALYZE_FIT = 3;
  STEP_ANALYZE_INTENT = 4;
  STEP_SCORE_LEAD = 5;
  STEP_GENERATE_RECOMMENDATIONS = 6;
  STEP_AGGREGATE = 7;
  STEP_FETCH_WEBSITE = 8;     // runs between VALIDATE_INPUT and EXTRACT_COMPANY
}

enum StepState {
  STEP_STATE_UNSPECIFIED = 0;
  STEP_STATE_RUNNING = 1;
  STEP_STATE_DONE = 2;
  STEP_STATE_SKIPPED = 3;
  STEP_STATE_FAILED = 4;
}

message StepProgress {
  Step step = 1;
  StepState state = 2;
  uint32 percent = 3;         // overall progress 0–100
  string message = 4;         // short human-readable, no PII
}

message AnalysisCompleted { AnalysisResult result = 1; }

enum ErrorCode {
  ERROR_CODE_UNSPECIFIED = 0;
  ERROR_CODE_INVALID_INPUT = 1;       // contract violation (non-retryable)
  ERROR_CODE_INSUFFICIENT_DATA = 2;   // not enough signal to analyze (non-retryable)
  ERROR_CODE_PROVIDER_ERROR = 3;      // LLM failed after retries (retryable)
  ERROR_CODE_PROVIDER_CONFIG = 4;     // auth rejected or model not found (non-retryable; fix in admin settings)
  ERROR_CODE_INTERNAL = 5;            // bug (retryable)
}

message AnalysisFailed {
  ErrorCode code = 1;
  string message = 2;         // user-safe
  bool retryable = 3;
}

message AnalysisResult {
  string summary = 1;
  CompanyInsights company = 2;
  FitAnalysis fit = 3;                 // unset when branch failed
  IntentAnalysis intent = 4;           // unset when branch failed
  LeadScore score = 5;
  repeated Recommendation recommendations = 6;
  ResultMeta meta = 7;
}

message CompanyInsights {
  string name = 1; string industry = 2; string size_estimate = 3;
  string business_model = 4; string description = 5;
  repeated string key_needs = 6; float confidence = 7;
}
message FitAnalysis { uint32 score = 1; string rationale = 2; repeated string matched_criteria = 3; repeated string gaps = 4; }
message IntentAnalysis { uint32 score = 1; string rationale = 2; repeated string signals = 3; }

enum Tier { TIER_UNSPECIFIED = 0; TIER_HOT = 1; TIER_WARM = 2; TIER_COLD = 3; }
message ScoreBreakdown { optional uint32 fit = 1; optional uint32 intent = 2; }
message LeadScore { uint32 value = 1; Tier tier = 2; float confidence = 3; ScoreBreakdown breakdown = 4; }

enum Priority { PRIORITY_UNSPECIFIED = 0; PRIORITY_HIGH = 1; PRIORITY_MEDIUM = 2; PRIORITY_LOW = 3; }
message Recommendation { string action = 1; string rationale = 2; Priority priority = 3; }

message ResultMeta {
  map<string, string> node_models = 1;  // step name (snake_case) → model actually used
  bool partial = 2; repeated string warnings = 3;
  uint32 duration_ms = 4; uint32 input_tokens = 5; uint32 output_tokens = 6;
  WebsiteFetch website = 7;
}

enum WebsiteFetchStatus {
  WEBSITE_FETCH_STATUS_UNSPECIFIED = 0;
  WEBSITE_FETCH_STATUS_FETCHED = 1;
  WEBSITE_FETCH_STATUS_NOT_PROVIDED = 2;
  WEBSITE_FETCH_STATUS_DISABLED = 3;
  WEBSITE_FETCH_STATUS_BLOCKED = 4;
  WEBSITE_FETCH_STATUS_FAILED = 5;
}
message WebsiteFetch {
  WebsiteFetchStatus status = 1;
  string final_url = 2;       // empty unless FETCHED
}
```

Go maps proto enums to the lowercase string values used in the REST API (`TIER_HOT` → `"hot"`). It stores the mapped JSON in `analysis_jobs.result`, so the API never exposes protobuf naming.

### 8.2 Why server-streaming instead of a Python → Go callback

The requirement is: *"Python reports completion and failure to Go through an appropriate gRPC mechanism."* **A server-streaming RPC is that mechanism.** Go opens `AnalyzeLead` and Python sends progress and exactly one terminal event on the response stream.

| | Server-streaming (chosen) | Unary start + Python calls Go back |
|---|---|---|
| Services that are gRPC servers | 1 (Python) | 2 (Python and Go) |
| Cancellation | Built in: Go cancels its context and Python's handler task is cancelled | Needs a separate `Cancel` RPC |
| Timeout | gRPC deadline propagates automatically | Must be enforced on both sides |
| Detecting Python death | Stream breaks with `UNAVAILABLE` immediately | Needs heartbeats or lease expiry |
| Go restart mid-job | Stream lost, so the job is failed with `INTERRUPTED` (§11) | Python could still deliver the result |
| Coupling | One-directional dependency | Circular dependency |

The one weakness (a Go restart loses in-flight streams) is acceptable for an MVP: the job is marked failed and is retryable. The callback model becomes worthwhile only when jobs run for many minutes or Go deploys very frequently.

### 8.3 Call semantics

| Concern | Rule |
|---|---|
| **Deadline** | Go sets `context.WithTimeout(ANALYSIS_TIMEOUT)` (default 300s) per job. gRPC propagates it. Python wraps the graph run in `asyncio.timeout(context.time_remaining())`. |
| **Cancellation** | Go keeps `map[jobID]context.CancelFunc` (mutex-guarded). Cancelling cancels the stream. In `grpc.aio` the handler task gets `CancelledError`, which unwinds the graph and aborts in-flight HTTP calls to the LLM. |
| **Idempotency** | `job_id` is the key. Python keeps `in_flight: set[str]`; a duplicate gets `ALREADY_EXISTS`. Go never dispatches the same job twice because the claim is an atomic `queued → running` transition. |
| **Model config** | Go resolves settings when it claims the job, stores them in `analysis_jobs.model_config`, and sends them in `models`. Config travels **with the request**: Python never reads settings, there's no cache to invalidate, and every job is reproducible from its row. |
| **ListModels** | Unary, 3s deadline set by Go. Python calls `GET {LLM_BASE_URL}/models` (5s HTTP timeout) and returns the IDs sorted. An unreachable server returns `UNAVAILABLE`, which Go maps to `503 llm_unreachable`. No retries: the admin can just click "Refresh". |
| **Domain failure** | Returned as an `AnalysisFailed` event followed by a normal stream close (status `OK`). |
| **Transport failure** | Returned as a gRPC status (`UNAVAILABLE`, `DEADLINE_EXCEEDED`, `RESOURCE_EXHAUSTED`, `INTERNAL`). |
| **Backpressure** | Python sets `maximum_concurrent_rpcs = MAX_CONCURRENT_ANALYSES` (default 4). Excess calls get `RESOURCE_EXHAUSTED`. Go's worker count (2) normally stays under this limit. The real throughput limit is the local LLM server. |
| **Stream integrity** | If the stream ends with `OK` but no terminal event, Go treats it as `INTERNAL` ("stream ended unexpectedly"). |
| **Metadata** | Go sends `x-job-id` and `x-request-id` metadata for log correlation. |

### 8.4 Retry boundaries

Each layer retries only what it understands, and retries never stack.

| Layer | What is retried | Policy |
|---|---|---|
| **LLM call** (Python) | Rate limit (429), timeout, 5xx, connection errors, structured-output parse failure | `Runnable.with_retry(stop_after_attempt=LLM_MAX_ATTEMPTS=4, wait_exponential_jitter=True)` with waits of 5s, 15s and 45s (plus up to 2s of jitter), so a restarting local LLM server or a model still loading has about a minute to recover. The job deadline bounds the total. The `ChatOpenAI(max_retries=0)` SDK retry is **disabled** so retries don't multiply (3×3). |
| **Dispatch** (Go) | `UNAVAILABLE`, `RESOURCE_EXHAUSTED`, **only before the first event is received** | Up to `AI_DISPATCH_ATTEMPTS=3`, backoff 1s/2s/4s, bounded by the job deadline |
| **Workflow** (user) | Any failed job with `retryable=true` | The user clicks "Retry", which creates a new job. Never automatic. |

After the first event arrives, Go **does not** retry. LLM spend has already happened, and a mid-stream failure means a deeper problem that a human should see.

---

## 9. LangGraph Workflow Design

### 9.1 Graph

```mermaid
flowchart TD
    START([START]) --> V[validate_input<br/><i>deterministic</i>]
    V -- "invalid / insufficient" --> FAIL[fail<br/><i>deterministic</i>]
    V -- ok --> W[fetch_website<br/><i>HTTP, non-fatal</i>]
    W --> E[extract_company<br/><i>LLM</i>]
    E -- "error after retries" --> FAIL
    E -- ok --> FIT[analyze_fit<br/><i>LLM</i>]
    E -- ok --> INT[analyze_intent<br/><i>LLM</i>]
    FIT --> S[score_lead<br/><i>deterministic</i>]
    INT --> S
    S -- "both branches failed" --> FAIL
    S -- "≥1 branch ok" --> R[generate_recommendations<br/><i>LLM, non-fatal</i>]
    R --> A[aggregate<br/><i>deterministic</i>]
    A --> END([END])
    FAIL --> END
```

| Node | Kind | Responsibility | On failure |
|---|---|---|---|
| `validate_input` | Pure | Trims and normalizes fields and the website domain. Requires `company_name` and **at least one** of `website`, `industry`, `notes`, `contact_title` (otherwise there's nothing to analyze). Enforces length limits again as defense in depth. | Sets `failure = INSUFFICIENT_DATA` or `INVALID_INPUT` and routes to `fail` |
| `fetch_website` | I/O | Fetches the lead's homepage under the rules in §9.7 and stores its readable text in `website`. Reports `skipped` when there is no website or fetching is disabled. | Non-fatal: `website.status = blocked / failed`, warning appended, extraction continues from lead data |
| `extract_company` | LLM | Produces `CompanyInsights` (industry, size estimate, business model, description, key needs, confidence) from lead data plus the website text when present | Critical: routes to `fail` with `PROVIDER_ERROR` / `PROVIDER_CONFIG` |
| `analyze_fit` | LLM | Scores fit against the configured ICP (`ICP_DESCRIPTION`): score 0–100, matched criteria, gaps, rationale | Non-fatal: `fit = None`, warning appended |
| `analyze_intent` | LLM | Scores buying intent from notes, contact seniority, and context: score 0–100, signals, rationale | Non-fatal: `intent = None`, warning appended |
| `score_lead` | Pure | Combines sub-scores deterministically (§9.4). If both are missing, sets `failure`. | Routes to `fail` if both branches failed |
| `generate_recommendations` | LLM | 2–5 prioritized next actions with rationale, based on everything above | Non-fatal: empty list, warning appended |
| `aggregate` | Pure | Builds and validates the final `AnalysisResult` (Pydantic), sets `meta` (partial, warnings, tokens, duration) | Raises, giving `INTERNAL` (a bug) |
| `fail` | Pure | Normalizes `failure` into `(code, message, retryable)` | — |

Workflow capabilities:

- **Input validation:** `validate_input`, plus Pydantic on every LLM output.
- **External grounding:** `fetch_website` (§9.7).
- **Extraction:** `extract_company`.
- **Parallelism:** two edges fan out from `extract_company`; `add_edge(["analyze_fit","analyze_intent"], "score_lead")` fans back in and waits for both.
- **Conditional transitions:** three `add_conditional_edges` routers (after validate, extract, and score).
- **Scoring:** `score_lead`.
- **Structured output:** `llm.with_structured_output(PydanticModel)`.
- **Per-node models:** each LLM node resolves its own model from `RunnableConfig` (§9.6).
- **Aggregation:** `aggregate`.
- **Error handling and retries:** §8.4, with graceful degradation for non-critical branches.

### 9.2 State

```python
class AnalysisState(TypedDict, total=False):
    # input
    job_id: str
    lead: LeadInput                       # pydantic model built from the proto

    # intermediate
    website: WebsiteContent               # status, final_url, title, description, text (truncated)
    company: CompanyInsights | None
    fit: FitAnalysis | None
    intent: IntentAnalysis | None
    score: LeadScore | None
    recommendations: list[Recommendation]

    # accumulated by parallel branches → MUST use reducers
    warnings: Annotated[list[str], operator.add]
    usage: Annotated[list[TokenUsage], operator.add]

    # control
    failure: Failure | None               # (code, message, retryable); set → route to fail
    result: AnalysisResult | None         # set by aggregate
```

The parallel branches write **different** keys (`fit`, `intent`), but both may append to `warnings` and `usage`. Those keys need `operator.add` reducers. Without them LangGraph raises `InvalidUpdateError` when two branches update the same key in one superstep.

### 9.3 Error handling inside the graph

- **Retry lives at the LLM-call level** via `with_retry` (§8.4), not in LangGraph's node `RetryPolicy`. A `RetryPolicy` that is exhausted raises and aborts the whole graph. Catching inside the node lets non-critical branches **degrade instead of fail**, and the routing stays visible in the graph.
- Each LLM node catches the final exception and classifies it with `errors.classify(exc)`:
  - `openai.AuthenticationError` / `NotFoundError` (bad model) → `PROVIDER_CONFIG`, non-retryable
  - Exhausted transient errors or parse errors → `PROVIDER_ERROR`, retryable
- Critical node (`extract_company`) sets `failure`. Non-critical nodes append to `warnings`.
- `fetch_website` never raises: every fetch error maps to a `WebsiteFetchStatus` and a user-safe warning (e.g. "Website could not be read; insights are inferred from lead data"). It has no retries: one timeout-bounded attempt, so a slow site cannot consume the job deadline.
- Anything not caught (a bug) propagates to the servicer and becomes `AnalysisFailed{INTERNAL, retryable=true}` with the exception logged.

### 9.4 Deterministic scoring (`scoring.py`)

The LLM supplies the sub-scores. Code computes the final score, so it is explainable, testable, and stable across runs.

```
if fit and intent:   value = round(0.6 * fit + 0.4 * intent); confidence = company.confidence
elif fit only:       value = fit;    confidence = company.confidence * 0.6; warnings += "intent unavailable"
elif intent only:    value = intent; confidence = company.confidence * 0.6; warnings += "fit unavailable"
else:                failure = PROVIDER_ERROR (retryable)

tier = hot if value >= 70 else warm if value >= 40 else cold
```

Weights and thresholds are module constants. Unit tests cover every branch and boundary (39/40, 69/70).

### 9.5 Progress reporting

- A small decorator `@tracked(Step.ANALYZE_FIT)` wraps each node. Using LangGraph's `get_stream_writer()`, it emits `{"step", "state": "running"}` on entry and `done` / `failed` / `skipped` on exit.
- The servicer runs `graph.astream(state, stream_mode=["custom", "values"])`. It maps `custom` chunks to `StepProgress` events and computes `percent` as completed steps / 8. The last `values` chunk is the final state, which becomes `completed` or `failed`.
- Steps not reached because of routing (for example, everything after a validation failure) are reported as `skipped` in the terminal snapshot that Go writes.

### 9.6 LLM configuration

- **Endpoint (env, infrastructure):** `LLM_BASE_URL` and `LLM_API_KEY`.
  - The primary target is a **local OpenAI-compatible server** (Ollama, LM Studio, vLLM, llama.cpp server). Hosted providers work unchanged.
  - `LLM_API_KEY` defaults to a dummy value, because local servers ignore it but the client requires a non-empty string.
- **Model per node (runtime, from the request):** the servicer turns `AnalyzeLeadRequest.models` into `{node: (model, temperature)}`. It validates that all four LLM nodes are present exactly once (otherwise `INVALID_INPUT`) and passes the map as `config={"configurable": {"models": …}}` to `graph.astream`.
  - Each LLM node takes `config: RunnableConfig` and calls `llm.for_node(config, "analyze_fit")`. That returns `ChatOpenAI(base_url, api_key, model, temperature, timeout=LLM_TIMEOUT_SECONDS, max_retries=0)` from a small cache keyed by `(model, temperature)`, so HTTP connection pools are reused across jobs.
  - The graph is **compiled once at startup**. Model choice is runtime input, not graph structure.
  - Using `configurable` (LangGraph's standard per-run config channel) instead of graph state keeps state limited to domain data.
- **Recording:** each node records the model it actually used. `aggregate` writes them to `meta.node_models`, and the UI shows them in the result footer.
- `LLM_STRUCTURED_OUTPUT_METHOD` = `json_schema` (default) | `function_calling` | `json_mode`. Local servers differ in support. If a model returns invalid JSON under `json_schema`, switch to `json_mode`. The Pydantic parse step plus retry catches remaining malformed output.
- **Local-model realities:**
  - Latency is higher, so defaults are `LLM_TIMEOUT_SECONDS=120`, `ANALYSIS_TIMEOUT=300s`, `ANALYSIS_WORKERS=2`. They leave at least a 2× margin over a 27B reasoning model measured during M2 (slowest call 48s, full run 122s); smaller models finish well within them.
  - **Mixing models costs swap time.** Local servers often hold only one model in memory (e.g. Ollama's `OLLAMA_MAX_LOADED_MODELS`). Using different models for `analyze_fit` and `analyze_intent`, which run in parallel, can force a reload per call. The admin page shows a hint when the two parallel nodes resolve to different models.
  - Many local servers process requests one at a time (e.g. Ollama's `OLLAMA_NUM_PARALLEL`). The fit and intent branches are still dispatched in parallel, but may be served one after the other. This is correct, just slower.
  - Keep prompts and schemas compact (short field descriptions, at most 5 list items) so small models stay reliable.
- **Startup check:** the service calls `GET {LLM_BASE_URL}/models` once at startup and logs a clear warning if the server is unreachable. It does not exit, so the LLM server can start later. The same call backs the `ListModels` RPC.
- **There is no runtime fake mode.** `llm.for_node` reads an optional `configurable["llm_factory"]`, so tests inject LangChain's `GenericFakeChatModel` or stubs that raise errors (per node). Fakes exist only in test code.
- **Prompt hygiene:** lead fields and website text are untrusted input. Prompts put them inside delimited blocks (`<lead_data>…</lead_data>`, `<website_content>…</website_content>`) and tell the model to treat them as data only. The model has **no tools**, and output must parse into a schema, so a prompt injection can at worst change the text of that one analysis.

### 9.7 Website fetch (`web.py`)

The `fetch_website` node grounds `extract_company` in what the company says about itself. It fetches **one page**, the URL in the lead's `website` field (normally the homepage). It does not crawl links.

**Request rules (enforced on the initial URL and on every redirect hop):**

| Rule | Value |
|---|---|
| Scheme and port | `http` / `https` only; ports 80 and 443 only |
| Address guard (SSRF) | Resolve the host; reject if **any** resolved address is loopback, private, link-local, multicast, reserved or unspecified (`ipaddress` checks, IPv4 and IPv6). This blocks the compose network (`api`, `postgres`), the host, and cloud metadata endpoints. Result: `blocked`. |
| Redirects | Followed manually, at most 3, each re-checked against the rules above |
| `robots.txt` | Fetched first (same rules, 64 KB cap). If it disallows the path for our user agent: `blocked`. A missing or unreadable `robots.txt` allows the fetch. |
| Timeout | `WEB_FETCH_TIMEOUT_SECONDS` (default 10s) for the whole fetch, including `robots.txt` and redirects |
| Response | 2xx only; `Content-Type` must be `text/html` or `application/xhtml+xml`; body streamed and cut at 1 MB |
| Identity | `User-Agent: LeadIntelBot/<version> (+<repository URL>)`. No cookies, no authentication, no JavaScript. |

**Extraction:** stdlib `html.parser` collects `<title>`, `<meta name="description">` / `og:description`, and visible text, skipping `script`, `style`, `noscript`, `svg`, `template`, `nav`, and `footer`. Whitespace is collapsed and the text is truncated to `WEB_FETCH_MAX_CHARS` (default 6000) so prompts stay small enough for 7–8B local models. Fewer than 200 characters of text counts as `failed` (typical of client-rendered pages).

**Data handling:** the fetched text exists only in graph state for the duration of the run. It is not returned to Go, stored, or logged (logs carry status, final host, byte count, and duration). `meta.website` records the outcome so the UI can label the insights.

**Testability:** the node reads an optional `configurable["http_client"]` (an `httpx.AsyncClient`), mirroring `llm_factory`. Tests pass a client built on `httpx.MockTransport` that serves fixtures, so the real fetch code (guard, redirects, limits, extraction) runs offline. Address resolution is behind a small injectable resolver so tests can map fixture hosts to public or private addresses.

**Fixtures (`ai/tests/fixtures/websites/`):**

Both kinds use the same layout, `<name>/response.json` (requested URL, final URL, redirect chain, status, headers, resolved addresses, `robots.txt` body, recording date) and `<name>/body.html`, and the same loader. Synthetic fixtures also declare the outcome they must produce (`expected`: status, a fragment of the user-safe reason, and URLs that must never be requested) and may set `delay_seconds` (slow server) or `body_repeat` (an oversized body generated at load time instead of committed).

- **Synthetic (committed, run in CI):** hand-written pages for fictional companies on reserved domains (`*.example`). They cover the happy path with realistic markup (navigation, footer, inline scripts and styles, `og:` tags) and the edge paths: redirect to a private address, redirect loop, `robots.txt` disallow, non-HTML content type, oversized body, client-rendered shell with almost no text, and a page containing prompt-injection text. They contain no third-party content, so the repository redistributes nothing.
- **Recorded (local only, git-ignored):** `ai/scripts/record_websites.py` fetches a short list of real company homepages **once** with the production fetcher and writes them to `ai/tests/fixtures/websites/recorded/`. It strips `<script>`, `<style>`, `<svg>` and comments before saving, to keep fixtures small. It is run by hand (`make record-websites`) when fixtures need refreshing, never by tests or CI. Tests marked `recorded` run over every recorded site and **skip** when the directory is empty, so a fresh clone and CI pass without them.
- Locally recorded fixtures also back the lead set used for manual prompt evaluation (M4), so prompt changes can be compared against the same website content.

---

## 10. Frontend Pages and Component Structure

### 10.1 Routing (SvelteKit, SPA mode)

SvelteKit runs with `ssr = false` and `adapter-static` (`fallback: 'index.html'`). The output is a pure static SPA served by nginx. Go remains the only backend, and no SvelteKit server code (`+page.server.ts`, form actions) is used.

| Route | File | Purpose |
|---|---|---|
| `/` | `routes/+page.svelte` | KPI strip (total, by tier, analyzed %), filters, lead table, create/edit sheet |
| `/leads/[id]` | `routes/leads/[id]/+page.svelte` | Lead info, inline status change, edit, delete, **AnalysisPanel**, history |
| `/admin` | `routes/admin/+page.svelte` (gated by `admin/+layout.svelte`) | **Superadmin: LLM model settings** (§10.7) |
| error / 404 | `routes/+error.svelte` | — |

List filters (`status`, `q`, `sort`) live in the URL query string. They are read via `page.url` from `$app/state` and written with `goto(..., { replaceState: true, keepFocus: true })`, so filtered views can be shared and the back button works.

### 10.2 State management

| Kind of state | Owner |
|---|---|
| **Server state** (leads, jobs, history) | **TanStack Query** (`@tanstack/svelte-query`, runes-based API: `createQuery(() => ({ … }))`) |
| **Live job progress** | SSE events written into the TanStack Query cache (§10.3) |
| **URL state** (filters, sort) | SvelteKit `page.url` |
| **Local UI state** (sheet open, form draft) | Component `$state` |

Query keys (`queries.ts`): `['leads', filters]`, `['leads', 'stats']` (under `['leads']`, so every lead mutation and finished analysis refreshes it), `['lead', id]`, `['analyses', leadId]`, `['analysis', jobId]`, `['config']`, `['admin', 'llm-settings']`, `['admin', 'llm-models']`.

| Mutation | Cache behavior |
|---|---|
| `createLead` / `updateLead` / `deleteLead` | Invalidate `['leads']` and `['lead', id]` |
| Status change | **Optimistic** update with rollback in `onError` |
| `triggerAnalysis` | Seeds `['analysis', jobId]` with the returned job and starts the live subscription |
| `cancelAnalysis` | Writes the returned job into the cache |
| `saveLlmSettings` | Writes the returned settings to `['admin','llm-settings']` and invalidates `['config']` |

### 10.3 Svelte 5 conventions

- **Runes only:** `$state`, `$derived`, `$effect`, `$props`, `$bindable`. No legacy `export let`, `$:`, or `writable` stores.
- **Snippets** (`{#snippet}` / `{@render}`) instead of slots. Callback props (`onsave`) instead of `createEventDispatcher`.
- **Side effects:** `$effect` with returned cleanup (for example, closing an `EventSource`). No `$effect` for derived values.
- **DOM behaviors** (animations) use **attachments** (`{@attach fadeIn()}`), so GSAP code stays out of components and is cleaned up automatically.
- **Forms:** a `zod` schema in `schemas/lead.ts` mirrors the server rules. Client errors and server `fields` errors render through the same field-error UI.

#### Live analysis (`live-analysis.ts`)

```ts
// Opens an EventSource for a job and writes every event into the query cache.
// Components just read createQuery(['analysis', jobId]) — one source of truth.
export function subscribeToAnalysis(qc: QueryClient, jobId: string): () => void {
  // 'snapshot' | 'completed' | 'failed' | 'cancelled' → qc.setQueryData(['analysis', jobId], job)
  // 'progress' → patch steps/progress in the cached job
  // terminal → close; invalidate ['lead', leadId], ['analyses', leadId], ['leads']
  // returns cleanup (close EventSource) for use inside $effect
}
```

`AnalysisPanel` calls it inside an `$effect` while the job is active. Reconnection uses `EventSource`'s built-in auto-reconnect. Because the server always sends `snapshot` first, a reconnect can never leave the cache stale.

### 10.4 Key components

| Component | Responsibility |
|---|---|
| `KpiStrip` | Totals by tier and the share of leads analyzed, from `GET /api/leads/stats`. The list endpoint is paginated (≤100 rows), so counting its items would be wrong for larger pipelines. |
| `LeadTable` | shadcn `Table`. Sortable rows: company, contact, status badge, score pill, last analyzed. Click opens detail. Skeleton rows while loading. Empty state with CTA. |
| `LeadFilters` | Status segmented control, debounced search (300ms), sort select. Synced to URL query. |
| `LeadForm` (in shadcn `Sheet`) | Create/edit. zod validation mirrors the server rules. Server `fields` errors map onto inputs. |
| `LeadStatusSelect` | Inline status change on the detail page (optimistic). |
| `AnalysisPanel` | State machine view: *never analyzed* → CTA; *active* → `AnalysisProgress` + Cancel; *succeeded* → results; *failed* → error + Retry (if `retryable`). If `['config'].ai_configured` is false, the Analyze button is disabled with a hint: "AI not configured — ask an admin (/admin)". The result footer lists the models used per step (`meta.node_models`). |
| `AnalysisProgress` | Vertical step list with per-step state icons. `analyze_fit` and `analyze_intent` sit side by side to show the parallelism. Includes a progress bar. |
| `ScoreRing` | SVG ring (0–100) with tier color. GSAP count-up and stroke animation. |
| `CompanyInsights`, `ScoreBreakdown`, `RecommendationList` | Result cards. Source label from `meta.website`: "From website + AI" with the final host when `fetched`, otherwise "AI-inferred" plus a short reason ("no website", "website could not be read", "website disallows automated access"). Confidence indicator. Partial-result warning banner. |
| `AnalysisHistory` | Last 5 jobs with status, score, timestamp. Clicking one shows that result. |
| `AlertDialog`, `Toaster` (svelte-sonner) | Delete confirmation. Non-blocking success and error feedback. |

### 10.5 Visual and motion direction

- **Styling stack:** Tailwind CSS v4 (`@tailwindcss/vite`, CSS-first config via `@theme` in `app.css`). shadcn-svelte components are copied into `lib/components/ui` and restyled through the shared CSS variables. Icons come from `@lucide/svelte`.
- **Look:**
  - Neutral surfaces (zinc scale), one indigo accent, tier colors (hot = rose, warm = amber, cold = slate-blue).
  - Inter via Google Fonts, 8px spacing grid.
  - 12px radius on cards, 1px hairline borders, soft shadows only on raised layers.
  - Light and dark themes via CSS variables (shadcn convention), toggled with the `.dark` class from a TopBar button (`mode-watcher`). **Dark is the default.** `static/theme-init.js` applies the saved mode before the app renders (a file, not an inline script, so a strict CSP stays possible).
  - Components use semantic tokens only (`bg-card`, `text-info`, `ring-tier-hot/25`, …), defined for both themes in `routes/layout.css`.
- **Hierarchy:** page title → KPI strip → content. The score is the most prominent element on the detail page.
- **Motion principles:** restrained and purposeful. Durations 150–450ms. `power2.out` / `expo.out` easing. No bounce, no looping decoration.

| Moment | Motion |
|---|---|
| Lead table load | Rows fade up 8px with a 30ms stagger (first load only, max 12 rows) |
| Sheet open/close | Slide from right + backdrop fade (300ms) |
| Page transition | Content cross-fade + 6px rise (200ms) |
| Step becomes running → done | Icon morph, check scales in, connector line fills |
| Score revealed | Ring stroke draws + number counts up (900ms), then the result cards stagger in |
| Status change | Badge color tween |

- **Reduced motion:** `gsap.matchMedia()` with `(prefers-reduced-motion: reduce)` sets durations to 0. Content must never depend on an animation finishing.
- **GSAP usage:** core `gsap` only (timelines, `gsap.context()` for cleanup inside attachments). No plugins are needed for the MVP.

### 10.6 API client

`client.ts` exposes typed functions (`listLeads`, `createLead`, …, `triggerAnalysis`) over `fetch`. Non-2xx responses throw `ApiError { status, code, message, fields }`. `queries.ts` wraps these functions into TanStack Query options and mutations, so components never call `fetch` directly.

All requests use relative `/api/...` paths: the Vite dev proxy forwards them in development and nginx does in Docker. Everything is same-origin, so no CORS is needed.

Admin calls add `Authorization: Bearer <token>` from `admin-token.svelte.ts`. On a `401`, the client clears the stored token and the gate asks for it again.

### 10.7 Superadmin page (`/admin`)

**Access:**

- There is no link to `/admin` in the main navigation. It is reached by URL.
- `AdminTokenGate` asks for the token and checks it with `GET /api/admin/llm-settings`. On success, it keeps the token in `sessionStorage` (cleared when the tab closes) and renders the page.
- A "Lock" button clears the token.

**Layout:** one card, "LLM models", with a table of five rows.

| Row | Model control | Temperature control | Shows |
|---|---|---|---|
| **Default** | `ModelSelect` (required) | number input 0–2 (empty = 0.2) | — |
| Extract company | `ModelSelect` with a first option **"Inherit default"** | number input (empty = inherit) | effective model and temperature in muted text, e.g. "→ qwen2.5:14b · 0.2" |
| Analyze fit | same | same | same |
| Analyze intent | same | same | same |
| Generate recommendations | same | same | same |

**Behavior:**

- `ModelSelect` is a shadcn `Select` or `Combobox` filled from `['admin','llm-models']`, with a **Refresh** button. If the LLM server is unreachable (`503`), it shows an inline error and the page stays read-only: no save until models can be verified.
- A model that is saved but no longer on the server is still shown, with a warning badge "not available on server".
- Effective values in the table are computed **live on the client** as the form changes, using the same rule as `Resolve`. After saving, the server's response is authoritative.
- A hint appears when `analyze_fit` and `analyze_intent` resolve to different models (model-swap cost on local servers, §9.6).
- **Save** is disabled until something changes. It sends a full-replace `PUT`. `422 unknown_model` field errors map onto the matching row. Success shows a toast: "Saved — applies to the next analysis".
- **First run:** when `configured=false`, a banner explains that a default model must be chosen before leads can be analyzed.

The form uses a zod schema (`schemas/llm-settings.ts`) that mirrors §7.5.

---

## 11. Error Handling and Observability

### 11.1 Error taxonomy (end-to-end)

| Origin | Condition | Job `error_code` | `retryable` | UI |
|---|---|---|---|---|
| Python | Validation fails | `invalid_input` / `insufficient_data` | false | "Add more details (website, industry or notes) and try again." + Edit button |
| Python | LLM transient, exhausted | `provider_error` | true | "AI provider failed. Retry." |
| Python | Auth rejected, or a configured model was removed from the LLM server | `provider_config` | false | "AI model configuration is invalid. An admin must update model settings." (shows the `/admin` link) |
| Python | `models` in the request is missing or duplicates a node | `invalid_input` | false | Generic. This is a Go bug, logged at error level. |
| Python | Website fetch blocked or failed | — (job continues) | — | Insights labeled "AI-inferred" with the reason (§10.4) |
| Python | Unhandled exception | `internal` | true | Generic + Retry |
| Go | Dispatch retries exhausted (`UNAVAILABLE`) | `ai_unavailable` | true | "Analysis service unavailable. Retry." |
| Go | Deadline exceeded | `timeout` | true | "Analysis took too long. Retry." |
| Go | Stream broken mid-run | `ai_unavailable` | true | Same as above |
| Go | API restarted mid-run | `interrupted` | true | "Analysis was interrupted. Retry." |
| User | Cancel | status `cancelled` | — | "Cancelled." + Analyze again |

Rules:

- `error_message` is always user-safe. Raw exceptions and provider responses go to logs only.
- **Go handlers:** errors are wrapped with `%w`. Domain sentinel errors (`lead.ErrNotFound`, `analysis.ErrNotCancellable`, …) map to HTTP codes in **one** place (`httpx.WriteError`).

### 11.2 Failure recovery

| Scenario | Behavior |
|---|---|
| **Go API restarts** | On startup, before workers start: `UPDATE analysis_jobs SET status='failed', error_code='interrupted', retryable=true WHERE status='running'` (the streams died with the process). `queued` jobs stay queued and are picked up by the workers. |
| **Python dies mid-run** | The Go stream returns `UNAVAILABLE` and the job is failed with `ai_unavailable`. SSE pushes `failed` and the user can retry. |
| **Python down before dispatch** | Dispatch retries with backoff (§8.4), then fails the job with `ai_unavailable`. `/readyz` reports `ai: down`. |
| **PostgreSQL down** | API returns `503`/`500`. pgxpool reconnects automatically. Workers log and back off. Compose `depends_on: service_healthy` handles startup ordering. |
| **Lead website slow or down** | One attempt bounded by `WEB_FETCH_TIMEOUT_SECONDS` (10s), then the analysis continues from lead data with `meta.website.status = failed`. |
| **LLM server slow or down** | Per-call timeout (120s) plus overall job deadline (300s). If the local server is down, the job fails with `provider_error` after the per-call retries, and the user can retry. |
| **Browser disconnects from SSE** | No effect on the job. On reconnect, a snapshot restores state. |
| **Graceful shutdown (SIGTERM)** | Go: stop accepting HTTP, stop claiming jobs, cancel in-flight jobs and mark them `interrupted`, drain up to 10s. Python: `server.stop(grace=10)`. |

### 11.3 Observability

- **Structured logs:**
  - Go uses `log/slog` (JSON handler).
  - Python uses stdlib `logging` with a JSON formatter.
  - Every log line about a job carries `job_id` and `lead_id`. HTTP logs carry `request_id` (chi `middleware.RequestID`), which is forwarded to Python as gRPC metadata.
- **Job timeline from data:** `created_at`, `started_at`, `finished_at`, `dispatch_attempts`, `model_config`, and `result.meta` (duration, tokens, models per node) answer most "what happened?" questions with one SQL query.
- **Settings changes:** saving LLM settings logs one `llm_settings_updated` line with the old and new resolved models per node. That is enough to correlate quality changes with config changes. A history table is out of scope.
- **Health:** `/healthz` (liveness) and `/readyz` (DB + AI health). Python implements `grpc.health.v1`.
- **Logged per job (Python):** step durations, LLM attempt counts, token usage. **Never logged:** API keys, contact email, or full notes (only lengths and hashes at debug level).

---

## 12. Security Considerations

| Area | MVP measure | Next step (post-MVP) |
|---|---|---|
| AuthN/AuthZ | No user accounts: lead features are open (a local single-user tool, documented loudly in the README). **Superadmin routes (`/api/admin/*`) require `ADMIN_TOKEN`** (bearer, constant-time compare, at least 24 chars, the API refuses to start without it). The frontend `/admin` gate is UX only; the server enforces the token. | Real users with a `role` (`user` / `superadmin`), session auth or OIDC, `owner_id` on leads |
| Admin token handling | Kept in `sessionStorage` (not `localStorage`), never logged, sent only over the same-origin `/api`. No `{@html}` anywhere limits XSS-based theft. | Short-lived sessions, CSRF protection once cookies are used |
| Settings tampering | Saved models must exist on the LLM server. Temperatures are range-checked. Model names are passed to the LLM API as parameters, never put into prompts or shell commands. | — |
| Secrets | With a local LLM there is usually no real key. If `LLM_API_KEY` is set, it exists **only** in the Python container env. `.env` is git-ignored and `.env.example` has placeholders. | Docker/K8s secrets, key rotation |
| Data residency | Lead data goes only to the configured `LLM_BASE_URL`, which is local by default. The website fetch sends **no lead data**: it is a plain `GET` of the lead's own URL. It does reveal to that site's operator that this machine requested the page. | Document the implications if a hosted provider is configured. Optional egress proxy for the fetch. |
| Server-side request forgery | The website URL is user input fetched by the `ai` container. Scheme/port allowlist, public-address guard on every redirect hop, redirect cap, timeout, size cap, HTML-only (§9.7). Residual risk: DNS rebinding between the check and the connection. | Pin the connection to the checked address, or route fetches through an egress proxy that enforces the same policy |
| Scraping etiquette and legal | One page per analysis, triggered by a user action, honors `robots.txt`, identifies itself with a descriptive `User-Agent`, no crawling, content not stored. `WEB_FETCH_ENABLED=false` turns it off entirely. | Per-domain rate limit if analyses are ever batched |
| Network exposure | Every published port binds to `127.0.0.1`. `web` is the entry point; `api`, `ai:50051` and `postgres` are published on localhost only so host-run processes (`make api`, `make test-go`) can reach them, and are never reachable from other machines. gRPC is plaintext on the compose network. | Drop the dev port mappings in a deployment profile; mTLS between services |
| Input validation | Validated in the Go API (types, lengths, enums, URL and email format), again in Python (`validate_input`), and through DB `CHECK` constraints. 64 KB body limit, unknown fields rejected. | — |
| SQL injection | Parameterized pgx queries only. Sort is an allowlisted enum, never interpolated. | — |
| Prompt injection | Lead fields **and website text** are delimited untrusted input. No tools or function execution, schema-validated output, results rendered as **text** (no `{@html}`). A synthetic fixture with injection text is part of the test suite. | Output moderation, if results get shared |
| XSS | Svelte escapes by default. `{@html}` is forbidden (lint rule). CSP: `default-src 'self'` with a hash for SvelteKit's inline bootstrap script, emitted by the build as a `<meta>` tag in `index.html` (`kit.csp`, mode `hash`); `style-src` allows inline style attributes (Svelte transitions, GSAP). nginx adds the header-only directives (`frame-ancestors 'none'`). Fonts are self-hosted, so no third-party origins are allowed. | Move the policy to a header with nonces if the app gains server rendering |
| Abuse and cost | One active job per lead, bounded worker pool, per-call and per-job timeouts, no automatic workflow retries | Per-user rate limit and token budget |
| Data minimization | `contact_email` is never sent to Python or the LLM | Configurable PII redaction |
| CORS | Not enabled (same-origin through the proxy). If enabled for dev, use an explicit allowlist. | — |
| Dependencies | Minimal set, lockfiles committed (`go.sum`, `uv.lock`, `package-lock.json`) | Dependabot/Renovate |

---

## 13. Local Development and Docker Compose

### 13.1 Configuration (`.env.example`)

| Variable | Service | Default | Purpose |
|---|---|---|---|
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | postgres | `lead` / `lead` / `leadintel` | DB bootstrap |
| `DATABASE_URL` | api | `postgres://lead:lead@postgres:5432/leadintel?sslmode=disable` | pgx pool DSN |
| `API_ADDR` | api | `:8080` | HTTP listen address |
| `AI_GRPC_ADDR` | api | `ai:50051` | Python service address |
| `ANALYSIS_WORKERS` | api | `2` | Max concurrent jobs per API instance (kept low for local LLMs) |
| `ANALYSIS_TIMEOUT` | api | `300s` | Per-job deadline |
| `AI_DISPATCH_ATTEMPTS` | api | `3` | Pre-stream dispatch retries |
| `MIGRATE_ON_START` | api | `true` | Run goose migrations at boot |
| `ADMIN_TOKEN` | api | *(required, ≥ 24 chars)* | Protects `/api/admin/*`. Generate with `openssl rand -hex 24`. |
| `LOG_LEVEL` | api, ai | `info` | — |
| `GRPC_ADDR` | ai | `0.0.0.0:50051` | gRPC listen address |
| `LLM_BASE_URL` | ai | *(required)*, e.g. `http://host.docker.internal:11434/v1` | Your local OpenAI-compatible endpoint. Use `host.docker.internal` to reach a server running on the host from inside Docker. |
| `LLM_API_KEY` | ai | `local` | Dummy by default (local servers ignore it) |
| `LLM_STRUCTURED_OUTPUT_METHOD` | ai | `json_schema` | `json_schema` \| `function_calling` \| `json_mode` |
| `LLM_TIMEOUT_SECONDS` | ai | `120` | Per-call timeout |
| `LLM_MAX_ATTEMPTS` | ai | `4` | Per-call attempts (waits 5s, 15s, 45s between them) |
| `MAX_CONCURRENT_ANALYSES` | ai | `4` | gRPC concurrency cap |
| `ICP_DESCRIPTION` | ai | *B2B SaaS, 50–1000 employees, …* | Ideal customer profile used by `analyze_fit` |
| `WEB_FETCH_ENABLED` | ai | `true` | Fetch the lead's website (§9.7). `false` for offline use; results then report `disabled`. |
| `WEB_FETCH_TIMEOUT_SECONDS` | ai | `10` | Whole-fetch budget, including `robots.txt` and redirects |
| `WEB_FETCH_MAX_CHARS` | ai | `6000` | Website text passed to `extract_company` |

Each service parses and validates its config at startup and **exits non-zero with a clear message** on invalid config.

**Model selection is not configured through env.** Which model each workflow step uses is runtime data, set on the `/admin` page and stored in `llm_node_settings`. Env holds only infrastructure: where the LLM server is, timeouts, concurrency, and secrets.

### 13.2 `docker-compose.yml` (shape)

| Service | Image / build | Ports (host) | Depends on | Healthcheck |
|---|---|---|---|---|
| `postgres` | `postgres:17-alpine`, volume `pgdata` | `127.0.0.1:5432` (dev convenience) | — | `pg_isready` |
| `ai` | `./ai` (python:3.14-slim + uv, non-root), `extra_hosts: ["host.docker.internal:host-gateway"]` (needed on Linux, harmless on macOS). Needs outbound internet access for the website fetch (default Docker networking provides it). | `127.0.0.1:50051` (dev convenience: lets a host-run API reach it; never exposed beyond localhost) | — | `python -m lead_ai.healthcheck` (grpc health) |
| `api` | `./backend` (multi-stage → `distroless/static:nonroot`) | `127.0.0.1:8080` | `postgres: service_healthy`, `ai: service_healthy` | `/api healthcheck`: the binary probes its own `GET /healthz` (the image has no shell or curl) |
| `web` | `./frontend` (`node:24-alpine` build → `nginx-unprivileged`, non-root, listens on 8080) | `127.0.0.1:5173 → 8080` | `api: service_healthy` | — |

Host ports are overridable (`POSTGRES_PORT`, `AI_PORT`, `API_PORT`, `WEB_PORT`) so the stack can run next to host-run dev processes. Container-to-container addresses (`DATABASE_URL`, `AI_GRPC_ADDR`) are set in `docker-compose.yml`, so the host-oriented values in `.env` do not apply inside Docker.

`nginx.conf` essentials: SPA fallback `try_files $uri /index.html` with `Cache-Control: no-cache`; long-lived caching for `/_app/immutable/`. For `location /api/`: `proxy_pass http://api:8080`, `proxy_http_version 1.1`, `proxy_buffering off`, `proxy_read_timeout 1h`, and `proxy_set_header Connection ''` (required for SSE). Security headers: `Content-Security-Policy: frame-ancestors 'none'` (the rest of the policy is the `<meta>` tag, §12), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`.

### 13.3 Workflows

```bash
cp .env.example .env              # set LLM_BASE_URL + ADMIN_TOKEN
docker compose up --build         # full stack → http://localhost:5173
# first run: open http://localhost:5173/admin, enter ADMIN_TOKEN, pick a default model, save

# Fast inner loop (hybrid): infra in Docker, app code on host with hot reload
docker compose up -d postgres ai
cd backend && go run ./cmd/api            # :8080
cd frontend && npm run dev                # :5173, Vite proxies /api → :8080
# Running ai on the host instead? Use LLM_BASE_URL=http://localhost:<port>/v1

make proto      # buf lint + generate → backend/gen; grpcio-tools → ai/src/lead_ai/gen
make openapi    # api/openapi.yaml → backend/internal/api + frontend/src/lib/api/gen
make test       # go test ./... · uv run pytest · npm run check && npm test
make lint       # go vet + staticcheck · ruff · svelte-check · buf lint
```

---

## 14. Milestone-Based Implementation Roadmap

**Strategy:**

1. Get the CRUD slice working first.
2. Build the analysis pipeline end-to-end against the **real local LLM**, happy path only (M2–M3).
3. Harden the workflow: degradation, retries, error classification (M4).
4. Polish comes last.

Every milestone ends in a runnable, demonstrable state.

| # | Milestone | Cumulative result |
|---|---|---|
| M1 | Lead CRUD vertical slice | Manage leads in a basic UI |
| M2 | gRPC contract + happy-path workflow (incl. website fetch) + job lifecycle | Real analyses, grounded in the lead's website, run and persist (curl) |
| M3 | SSE + live analysis UI | **Full E2E slice with real AI** |
| M4 | Workflow hardening | Graceful degradation, retries, error taxonomy, website fetch edge cases |
| M5 | Reliability: cancel, timeout, recovery | Failure-safe |
| M6 | Superadmin model settings page | Per-node models configurable from the UI |
| M7 | Premium UI and GSAP motion | Polished dashboard |
| M8 | Full Docker Compose + docs | One-command startup |

> **Priority:** M1–M4 form the functional core.
> - M6 can follow later, because settings are already editable through the admin API from M2.
> - Within M5, graceful shutdown is the lowest-priority item.
> - Within M7, the score ring and step animations come first.

---

### M1 — Lead CRUD vertical slice

**Goal:** Create, list, view, edit, and delete leads and change their status through a working (unpolished) UI backed by Go and PostgreSQL.

**Files**
- `docker-compose.yml` (postgres only for now), `.env.example`, `Makefile`
- `backend/go.mod`, `backend/cmd/api/main.go`, `backend/internal/config/`, `backend/internal/httpx/`, `backend/internal/lead/*`, `backend/migrations/0001_leads.sql`
- `frontend/` scaffold (`npx sv create`: SvelteKit + TS + Tailwind; `adapter-static`; `npx shadcn-svelte init`), `svelte.config.js`, `src/routes/{+layout.ts,+layout.svelte,+page.svelte,+error.svelte,leads/[id]/+page.svelte}`, `src/lib/api/{types,client,queries}.ts`, `src/lib/schemas/lead.ts`, `components/leads/{LeadTable,LeadFilters,LeadForm,LeadStatusSelect,LeadStatusBadge}.svelte`, shadcn `ui/*` as needed

**Tasks**
1. Compose with `postgres:17-alpine` and a healthcheck.
2. Go: config loading, pgxpool, embedded goose migrations on start, chi router with `RequestID`, `RealIP`, `Recoverer`, a slog request logger, and timeouts.
3. `lead` package: model and validation, store (pgx, hand-written SQL), handlers for the five lead endpoints, error envelope.
4. Frontend:
   - Scaffold in SPA mode (`ssr = false`), typed API client, TanStack Query setup.
   - List page with URL-synced filters and search; create/edit sheet with zod validation.
   - Detail page with optimistic status select and delete (with confirm dialog).
   - Basic shadcn defaults are enough; visual polish comes in M7.
5. Vite dev proxy `/api → :8080`.

**Dependencies:** none.

**Acceptance criteria**
- All lead endpoints in §7.3 behave as specified, including `404`, `400 validation_failed` with `fields`, and `413`.
- `status` rejects values outside the enum (API and DB CHECK).
- UI: create → appears in list; edit → persisted; status change → persisted (optimistic, rolls back on error); delete → confirm → removed.
- Filtering by status and search by company name work.

**Verify**
- `go test ./internal/lead/...`: validation unit tests (table-driven) and store integration tests against compose Postgres (`TEST_DATABASE_URL`).
- Manual curl script in the README (create/list/patch/delete).
- `npm run check` (svelte-check) passes with zero errors. Manual UI walkthrough.

---

### M2 — gRPC contract, happy-path workflow, job lifecycle

**Goal:** `POST /api/leads/{id}/analyses` creates a job. A Go worker resolves the per-node models from `llm_node_settings` and dispatches the job over gRPC to the Python service, which runs the **full graph topology against your local LLM** (happy path only), including the fetch of the lead's website. The result is persisted. Settings are managed through the token-protected admin API with curl. No UI yet.

**Files**
- `buf.yaml`, `buf.gen.yaml`, `proto/leadintel/analysis/v1/analysis.proto`, generated code in `backend/gen/` and `ai/src/lead_ai/gen/`
- `backend/migrations/{0002_analysis_jobs,0003_llm_node_settings}.sql`, `backend/internal/analysis/{model,store,worker,handler}.go`, `backend/internal/llmsettings/*`, `backend/internal/httpx/` (`RequireAdmin`), `backend/internal/aiclient/client.go`
- `ai/pyproject.toml`, `ai/src/lead_ai/{main,config,server,errors}.py`, `ai/src/lead_ai/llm.py`, `ai/src/lead_ai/web.py`, `ai/src/lead_ai/graph/{state,schemas,prompts,builder,nodes,scoring}.py`, `ai/Dockerfile`
- `ai/scripts/record_websites.py`, `ai/tests/fixtures/websites/` (synthetic happy-path fixtures), `Makefile` (`record-websites`), `.gitignore` (`ai/tests/fixtures/websites/recorded/`)
- `docker-compose.yml` (+ `ai` service)

**Tasks**
1. Write the proto (§8.1), run `buf lint`, and generate Go and Python code.
2. Python:
   - `grpc.aio` server with health service, the `AnalyzeLead` servicer, and `ListModels`.
   - `llm.py`: `for_node(config, node)` with a `(model, temperature)` client cache pointed at `LLM_BASE_URL`, `list_models()`, and the startup `/models` check.
   - Validate `models` in the request (4 LLM nodes, exactly once) and pass them via `configurable`.
   - Build the **full graph topology** (§9.1) with `build_graph()`. Use simple prompts and `with_structured_output` in every LLM node, plus deterministic `validate_input`, `score_lead`, `aggregate`. Record `meta.node_models`.
   - `web.py` and the `fetch_website` node (§9.7): scheme/port allowlist, public-address guard on every hop, manual redirects (≤3), timeout, 1 MB cap, HTML-only, text extraction and truncation. `extract_company` receives the text in a `<website_content>` block. Record `meta.website`.
   - Synthetic happy-path fixtures: 2–3 fictional company homepages with realistic markup (committed).
   - `record_websites.py`: record 4–5 real homepages of contrasting companies locally (git-ignored; they also seed the M4 evaluation leads).
   - Emit progress via the stream writer. Add in-flight `job_id` dedupe.
3. Go: jobs migration with the partial unique index. Store methods `Create` (catches `23505` and returns the existing job), `ClaimNext`, `UpdateProgress`, `Complete`, `Fail`, `Get`, `ListByLead`. The `Complete` transaction also updates `leads.ai_score/ai_tier/last_analyzed_at`.
4. Go: worker pool (`ANALYSIS_WORKERS`) with wake channel and ticker. Dispatch through `aiclient` with a per-job deadline. Map events to the store. Dispatch retry on `UNAVAILABLE` before the first event.
5. Go: `analysis.Analyzer` interface (defined in the consumer package) so the worker can be tested with a fake.
6. Go `llmsettings`:
   - Seeded five-row migration; store `Get` / `Replace` (one transaction).
   - Pure `Resolve`.
   - `RequireAdmin` middleware (`ADMIN_TOKEN` required at startup).
   - `GET/PUT /api/admin/llm-settings` with validation against `ListModels`, `GET /api/admin/llm-models`, public `GET /api/config`.
7. Go worker: at claim time, load settings → `Resolve` → save `model_config` on the job → send `models` in the request. The trigger endpoint returns `409 ai_not_configured` while no default model is set.
8. REST: trigger, get job, list history.

**Dependencies:** M1.

**Acceptance criteria**
- Before any settings are saved, triggering an analysis returns `409 ai_not_configured`, and `GET /api/config` returns `ai_configured: false`.
- `/api/admin/*` without the token returns `401`. `GET /api/admin/llm-models` lists the local server's models. A `PUT` with a nonexistent model returns `422 unknown_model` naming the node.
- After `PUT`ting a default model plus one override (e.g. a different model for `extract_company`), `curl -X POST /api/leads/{id}/analyses` returns `202`. Repeating it while active returns `200` with the **same** job ID.
- For a realistic lead with a reachable website, `GET /api/analyses/{id}` shows `succeeded` within `ANALYSIS_TIMEOUT`, with a schema-valid `result`, all steps `done`, and `meta.website.status = fetched`. The lead's `ai_score` is set, and the score matches the deterministic formula applied to the sub-scores.
- The same lead without a website succeeds with `fetch_website` `skipped` and `meta.website.status = not_provided`. A lead whose website is `http://localhost:8080` or `http://api:8080` succeeds with `blocked`, and no request reaches that address.
- The job's `model_config` and `result.meta.node_models` both show the override for `extract_company` and the default for the other three nodes. Changing settings afterwards does not change the stored job.
- A lead with only `company_name` gives `failed / insufficient_data / retryable=false` **without any LLM call or website fetch**, and the later steps show as `skipped`.
- With `ai` stopped, a triggered job ends `failed / ai_unavailable / retryable=true` after ~3s of retries (3 attempts, backoff 1s and 2s).

**Verify**
- `uv run pytest`:
  - `test_scoring.py`: all branches and boundaries.
  - `test_web.py` (offline, fixtures through `httpx.MockTransport`): text extraction for every synthetic happy-path fixture and, when present locally, every recorded site (title, description, non-empty text within `WEB_FETCH_MAX_CHARS`, no script/style content); address guard rejects loopback, private, link-local and IPv6-local addresses, including on a redirect hop.
  - `test_graph.py`: happy path (with a synthetic website fixture; the recording fake asserts the `extract_company` prompt contains the website text) and insufficient data, with `GenericFakeChatModel` injected.
  - `test_graph.py` also checks per-node routing: a recording fake factory sees the configured model for each node.
  - `test_server.py`: event ordering, single terminal event, duplicate job → `ALREADY_EXISTS`, missing or duplicate `models` entry → `INVALID_INPUT`.
- `go test ./internal/analysis/...`: worker with a fake `Analyzer` (success, domain failure, transport failure before and after the first event); store test for the dedupe index.
- `go test ./internal/llmsettings/...`:
  - table-driven `Resolve` tests (inherit model, inherit temperature, all overrides, unconfigured default);
  - handler tests for `401`, `422 unknown_model`, `503 llm_unreachable` (fake model lister), and full-replace validation.
- Manual curl walkthrough against your local LLM.

---

### M3 — SSE and live analysis UI (end-to-end slice)

**Goal:** From the lead detail page, the user clicks **Analyze**, watches the steps progress live, and sees the real AI result rendered.

**Files**
- `backend/internal/analysis/{broker,handler}.go` (SSE), worker publishes to the broker
- `frontend/src/lib/api/live-analysis.ts`, `src/lib/api/queries.ts` (analysis queries and mutations), `components/analysis/{AnalysisPanel,AnalysisProgress,ScoreRing,CompanyInsights,ScoreBreakdown,RecommendationList,AnalysisHistory}.svelte`

**Tasks**
1. Broker: `Subscribe(jobID) (<-chan Event, unsubscribe func())` and `Publish(jobID, Event)` (non-blocking, drops slow subscribers).
2. SSE handler: subscribe → snapshot → stream → 15s ping → close on terminal. Clear the write deadline. Detect client disconnect via `r.Context()`.
3. `subscribeToAnalysis` (SSE → TanStack Query cache) plus the `AnalysisPanel` state machine (never analyzed / active / succeeded / failed).
4. Static result rendering (no animation yet). History list.

**Dependencies:** M2.

**Acceptance criteria**
- Clicking Analyze shows steps transitioning live, with fit and intent running in parallel, then the result. No page refresh needed.
- Refreshing mid-analysis resumes showing correct progress (snapshot).
- Opening the same lead in two tabs and clicking Analyze in both shows a **single** job progressing in both tabs.
- Connecting to the events endpoint of a finished job gives `snapshot` + terminal event + close.
- Failed jobs show the message, and "Retry" appears only when `retryable`.

**Verify**
- `go test`: broker unit tests (fan-out, unsubscribe, slow subscriber dropped). SSE handler test with `httptest` (event order for terminal job).
- `curl -N localhost:8080/api/analyses/{id}/events` shows a well-formed event stream.
- The insights card shows "From website + AI" with the host for a fetched site, and "AI-inferred" with the reason otherwise.
- Manual two-tab test and refresh-mid-run test.

---

### M4 — Workflow hardening

**Goal:** The workflow handles unreliable LLM behavior (slow responses, malformed JSON, server down) as specified in §8.4 and §9.3.

**Files**
- `ai/src/lead_ai/llm.py`, `web.py`, `graph/prompts.py`, `graph/nodes.py`, `errors.py`, `config.py`
- `ai/tests/fixtures/websites/` (synthetic edge-case fixtures)

**Tasks**
1. `llm.py`: per-node `ChatOpenAI` with `max_retries=0`. A `structured(llm, schema)` helper combines `with_structured_output(schema, method=LLM_STRUCTURED_OUTPUT_METHOD, include_raw=True)` with `.with_retry(...)`. Token usage is read from the raw message when the server reports it.
2. Tighten prompts for small local models (compact schemas, explicit JSON-only instructions, delimited untrusted input).
3. Node error classification (§9.3). Graceful degradation for non-critical nodes. Fill `meta` (node models, tokens, duration, warnings).
4. Validate config at startup: `LLM_BASE_URL` is required, `WEB_FETCH_*` values are in range. Run the `/models` reachability check (§9.6).
5. Website fetch hardening (§9.7): `robots.txt` handling, content-type and minimum-text checks, mapping every fetch error to a `WebsiteFetchStatus` and a user-safe warning, and the whole-fetch timeout budget. Add the synthetic edge-case fixtures.
6. Prompt for `extract_company`: prefer website statements over model knowledge when they conflict, raise `confidence` only when the website corroborates the lead data, and ignore instructions inside `<website_content>`.

**Dependencies:** M2 (M3 for the visual check).

**Acceptance criteria**
- Stopping the local LLM server mid-analysis gives `provider_error / retryable=true` after `LLM_MAX_ATTEMPTS` attempts (visible in logs), not a hang.
- A model removed from the LLM server after it was saved in settings (e.g. `ollama rm`) gives `failed / provider_config / retryable=false` without retry loops (one attempt in logs). The UI points to `/admin`.
- When one parallel branch fails (simulated in tests), the job still succeeds with `meta.partial = true` and a warning banner in the UI.
- Results from 3 contrasting leads (strong ICP fit, poor fit, sparse data) are schema-valid, and their scores are ordered sensibly. Run with website fetch on and off, the fetched variant gives a more specific `company.description` and `key_needs`.
- Every synthetic website fixture ends with the expected `meta.website.status`, the job still succeeds, and the fetch never exceeds `WEB_FETCH_TIMEOUT_SECONDS`. The prompt-injection fixture does not change the output schema or leak instructions into recommendations.

**Verify**
- `uv run pytest`: graph tests with `GenericFakeChatModel` and stubs that:
  - raise a transient error and then succeed (retry works);
  - raise `AuthenticationError` / `NotFoundError` (no retry);
  - fail one branch (`partial=true`);
  - fail both branches (job fails).
- `uv run pytest tests/test_web.py`: one table-driven case per synthetic fixture (redirect to private address, redirect loop, `robots.txt` disallow, non-HTML, oversized body, client-rendered shell, slow response against the timeout), each asserting the status and that no disallowed request was made.
- Manual: the 3-lead comparison (website fetch on and off), and stopping and starting the LLM server mid-run.

---

### M5 — Reliability: cancellation, timeouts, recovery

**Goal:** The failure modes in §11.2 behave as specified.

**Files**
- `backend/internal/analysis/{worker,store,handler}.go`, `backend/cmd/api/main.go`, `ai/src/lead_ai/server.py`, `ai/src/lead_ai/main.py`

**Tasks**
1. Cancel endpoint plus cancel registry in the worker. Conditional status updates (first writer wins).
2. Delete lead → cancel active job first.
3. Startup recovery (`running → failed/interrupted`). Graceful shutdown on both services.
4. Python honors deadlines (`asyncio.timeout`) and cancellation, and cleans up its in-flight set in `finally`.
5. `/readyz` with DB ping and gRPC health check (1s timeout each).

**Dependencies:** M3.

**Acceptance criteria**
- Cancelling a running job flips the UI to `cancelled` within ~1s, and Python logs show the run aborted (no further LLM calls).
- With `ANALYSIS_TIMEOUT=5s`, a real analysis ends `failed / timeout`, and Python logs show the run aborted.
- `docker compose restart api` during a run leaves the job `failed / interrupted / retryable=true` after restart, and the UI offers Retry.
- `docker compose stop ai` makes `/readyz` return `503` with `ai: down`.

**Verify**
- `go test`: store tests for the race rules (complete after cancel is a no-op), worker cancel test with a blocking fake `Analyzer`.
- `uv run pytest`: servicer cancellation test (client cancels → handler exits, `job_id` removed from the in-flight set).
- Manual chaos checklist (the four bullets above).

---

### M6 — Superadmin model settings page

**Goal:** A superadmin can choose the default model and per-node overrides (model and temperature) from `/admin`. Leads UI reflects whether AI is configured and which models produced each result.

**Files**
- `frontend/src/routes/admin/{+layout.svelte,+page.svelte}`, `src/lib/admin-token.svelte.ts`, `src/lib/schemas/llm-settings.ts`, `src/lib/api/{client,queries,types}.ts` (admin and config calls), `components/admin/{AdminTokenGate,ModelSettingsForm,NodeModelRow,ModelSelect}.svelte`
- `components/analysis/AnalysisPanel.svelte` (not-configured state, models-used footer)

**Tasks**
1. Token gate: prompt, verify, `sessionStorage`, lock button, auto-lock on `401`.
2. Settings form per §10.7:
   - models select from `llm-models` with "Inherit default" and refresh;
   - live effective-value preview;
   - "not available on server" badge for stale models;
   - parallel-branch model mismatch hint;
   - full-replace save with field-level `422` errors.
3. Leads UI: `['config']` query. Disable Analyze with a hint when not configured. Show `meta.node_models` in the result footer.

**Dependencies:** M2 (API), M3 (AnalysisPanel).

**Acceptance criteria**
- `/admin` without a token shows only the gate. A wrong token shows an error. The right token shows the settings.
- On a fresh DB, the leads page shows "AI not configured" and Analyze is disabled. After picking a default model and saving, Analyze works without a page reload.
- Setting `generate_recommendations` to another model and saving means the **next** analysis reports that model for that step in its footer. A running analysis is unaffected.
- With the LLM server stopped, the page shows "LLM server unreachable" and Save is disabled. Existing settings still display.
- A saved model that disappears from the server is flagged in the table.

**Verify**
- `vitest`: the client-side effective-value function matches the Go `Resolve` cases. Both tests read one table, `api/testdata/llm-resolve-cases.json`.
- Manual walkthrough of the five criteria above.

---

### M7 — Premium UI and GSAP motion

**Goal:** The dashboard looks and feels like a premium SaaS product, with restrained motion.

**Files**
- `frontend/src/app.css` (tokens, themes), `src/lib/motion/{gsap,attachments}.ts`, `components/ui/*`, `components/layout/*`, styling passes over all pages and components

**Tasks**
1. Design tokens (color, spacing, radius, shadow, typography) and light/dark themes.
2. `AppShell` + `TopBar`, KPI strip on the leads page, skeletons, empty states, toasts. Restyle shadcn components through the shared tokens.
3. Motion attachments: `fadeIn`, `staggerChildren`, `countUp`, `drawRing`, all using `gsap.context()` cleanup and the reduced-motion `matchMedia`.
4. Apply the motion table from §10.5.

**Dependencies:** M3 (M4 recommended).

**Acceptance criteria**
- Every interaction in §10.5 animates as described, with no layout shift or jank on a mid-range laptop.
- With OS "reduce motion" on, there are no animations and everything is still visible immediately.
- Usable at 768px width. Keyboard: sheet and dialog trap focus and close on `Esc`. Visible focus rings.
- `svelte-check` clean, no `{@html}`.

**Verify**
- Manual visual pass in light and dark themes and with reduced motion on.
- Lighthouse accessibility ≥ 90 on the leads, lead detail, and admin pages.

---

### M8 — Full Docker Compose and documentation

**Goal:** `docker compose up --build` brings up the entire stack, and a newcomer can run it from the README.

**Files**
- `backend/Dockerfile`, `ai/Dockerfile` (final), `frontend/Dockerfile`, `frontend/nginx.conf`, `docker-compose.yml` (final), `README.md`, `Makefile` (final)

**Tasks**
1. Multi-stage Dockerfiles (non-root users, small images).
2. Compose health-based ordering. Internal-only `ai`. Nginx SSE-safe proxy and CSP.
3. README: quick start (local LLM setup, e.g. Ollama with a recommended model), architecture link to this spec, curl examples, troubleshooting (reaching the host LLM from Docker, structured-output method fallback, running offline with `WEB_FETCH_ENABLED=false`). Document what the website fetch does and does not do (§9.7, §12), and how to record real-site fixtures locally.

**Dependencies:** M1–M7.

**Acceptance criteria**
- On a fresh clone: run `cp .env.example .env`, set `LLM_BASE_URL` and `ADMIN_TOKEN`, run `docker compose up --build`, then pick a default model on `/admin`. The result is a working app at `http://localhost:5173` that analyzes leads with the local LLM.
- SSE works through nginx (progress is not delivered all at once at the end).
- `make test` passes.

**Verify**
- `docker compose down -v && docker compose up --build`, then run the full manual E2E (configure model in `/admin` → create → analyze → watch → result → edit → delete).

---

### MVP Definition of Done

- [ ] All milestone acceptance criteria met
- [ ] `make test` and `make lint` green
- [ ] Works end-to-end against the local OpenAI-compatible LLM server
- [ ] README quick start verified from a clean clone

---

## 15. Architecture Decisions and Trade-offs

### 15.1 Why Go for the main backend?

- Long-lived concurrent work (SSE connections, streaming gRPC calls, workers) maps naturally to goroutines, channels, and `context` cancellation, all in the stdlib.
- Static typing, a single static binary, fast startup, and low memory make it cheap to run and simple to deploy.
- The ecosystem fits the problem: `pgx` is a high-quality Postgres driver, gRPC Go is a first-class implementation, and `chi` is stdlib-compatible `net/http`.
- **Trade-off:** more boilerplate than a Python or Node backend, but explicit code is easy to review, debug, and maintain.

### 15.2 Why Python + LangGraph for AI orchestration?

- The AI ecosystem (provider SDKs, LangChain integrations, evaluation tooling) is Python-first.
- LangGraph models the workflow as an explicit, typed state machine: parallel fan-out and fan-in, conditional edges, reducers for concurrent writes, and streaming. That is easier to reason about and test than ad-hoc async code or an autonomous agent.
- Isolating AI in its own service keeps volatile code (prompts, models, provider quirks) away from the system of record. It can be deployed and scaled separately.
- **Trade-off:** a second language and runtime, and a network hop. This is justified by the ecosystem and by the different scaling profile (§15.7).
- **LangChain usage is deliberately thin:** `ChatOpenAI`, `with_structured_output`, `with_retry`. No chains, agents, or memory abstractions.

### 15.3 Why gRPC (not REST) between Go and Python?

- **Contract first:** one `.proto` generates typed clients and servers for both languages. Breaking changes are caught by `buf breaking`.
- **Server-streaming is native.** Progress and the final result travel on one call, and cancellation and deadlines propagate automatically. With REST this would need SSE or chunked responses plus a separate cancel endpoint.
- **Health checking** comes as a standard protocol.
- **Trade-off:** harder to debug with curl (use `grpcurl`), plus a codegen step. Browsers can't call it directly, which doesn't matter here because only Go calls it.

### 15.4 Why SSE (not WebSockets) for the browser?

- The data flows **one way** (server → client). Client actions are ordinary REST calls.
- SSE is plain HTTP. It works through proxies with one nginx setting, reconnects automatically (`EventSource`), and needs no library on either side.
- The "snapshot first" design makes reconnects idempotent without replay logs.
- **Trade-off:** text-only and one-way. HTTP/1.1 limits a browser to about 6 connections per origin, which is fine here (one stream per open analysis). HTTP/2 removes the limit.

### 15.5 How are concurrent AI jobs handled?

- **Admission:** a partial unique index allows at most one active job per lead (dedupe at the DB, race-free).
- **Execution:** a fixed pool of `ANALYSIS_WORKERS` goroutines claims jobs with `FOR UPDATE SKIP LOCKED`. The DB is the queue, so there's no extra infrastructure, and jobs survive restarts.
- **Downstream protection:** Python caps concurrent RPCs (`RESOURCE_EXHAUSTED` beyond that), and Go retries those with backoff.
- **Within a job:** the fit and intent analyses run in parallel inside the graph (asyncio), which cuts latency by roughly one LLM call.
- **Defense in depth against duplicates:** DB index → atomic claim → Python in-flight set.
- **Config consistency under concurrency:** each job snapshots its resolved model config when it is claimed. Saving settings while jobs run can't produce a job that uses a mix of old and new models.

### 15.6 How does the system recover from failures?

- **State lives only in PostgreSQL.** Python is stateless. Go's in-memory state (broker, cancel registry) can be rebuilt or safely lost.
- Restart recovery fails orphaned `running` jobs as `interrupted` and re-runs `queued` ones.
- Explicit retry boundaries (§8.4) prevent retry storms and duplicate LLM spend.
- Every failure ends up as a **terminal, user-visible state with a retry flag**. No job stays pending forever, because every job has a deadline.

### 15.7 How can services scale independently?

| Service | Bottleneck | Scaling path |
|---|---|---|
| Python AI | LLM latency, provider rate limits (I/O bound) | More replicas behind a gRPC-aware load balancer (client-side round-robin via DNS, or Envoy). It's stateless, so this is trivial. |
| Go API | Connections (SSE) | More replicas. The DB queue is already multi-instance safe (`SKIP LOCKED`). Needed changes: (1) the broker moves to Postgres `LISTEN/NOTIFY` so an SSE client on instance A sees events from a worker on instance B; (2) orphan detection switches from "fail all running at startup" to a lease (`locked_until` heartbeat column). |
| PostgreSQL | Writes for progress | Negligible at this scale (≈10 writes/job). Later: read replica for lists, or move progress to NOTIFY only. |
| Frontend | — | Static files behind a CDN |

### 15.8 Other notable decisions

| Decision | Alternative | Why |
|---|---|---|
| OpenAPI-first REST contract with codegen on both sides | Hand-synced Go structs, TS types and zod; a shared package (impossible across languages) | One file defines shapes, enums, lengths and patterns. Go and TS types are generated, server validation reads the spec, and a contract test checks responses, so the browser and server cannot drift silently. Mirrors the proto-first approach used for Go ↔ Python. |
| Hand-written pgx SQL | `sqlc`, GORM | Three small tables. Plain SQL is readable and has no codegen step. `sqlc` is a good upgrade if queries grow. |
| Deterministic final score | LLM produces the score | Explainable, testable, stable. The LLM only judges sub-dimensions. |
| Results as JSONB | Normalized result tables | Results are read whole and never queried by inner fields. Only the score is denormalized onto `leads` for sorting. |
| SvelteKit in SPA mode (`ssr=false`, adapter-static) | Plain Vite + router library, SvelteKit with SSR | The standard Svelte router with typed params and URL state, and no Node server to run. SSR adds no value behind a dashboard, and Go stays the only backend. |
| TanStack Query for server state | Hand-written runes stores | Caching, invalidation, optimistic mutations, and loading/error states come built in. SSE events feed the same cache, so there is one source of truth. |
| Tailwind v4 + shadcn-svelte | Hand-written CSS, a heavyweight component library | Fast path to a premium look. Accessible primitives (bits-ui) for dialogs, sheets, and selects. Components are copied in, so they are fully customizable with no dependency on the library's styling. |
| Real local LLM in dev, fakes only in unit tests | Runtime fake mode | One code path. A local model costs nothing to run, and LLM-factory injection via `configurable` keeps tests deterministic. |
| Structured-output method set per deployment (`LLM_STRUCTURED_OUTPUT_METHOD`, default `json_schema`) | A fixed method; a method per node | OpenAI-compatible servers differ in JSON support (Ollama, LM Studio, vLLM). One setting covers the server in use; per-node methods add configuration without a current need. |
| Block analyses until a default model is set (`409 ai_not_configured`) | Seed a default from an env var (`LLM_BOOTSTRAP_MODEL`) | An explicit choice in `/admin` avoids silently running on a model nobody selected. An env-var bootstrap can be added for unattended setups. |
| Manual retry of failed analyses (Retry shown when `retryable`) | Automatic one-shot retry | Every retry spends LLM time; the user decides. Transport failures before the first event are already retried by the worker (§8.4). |
| ICP description from the environment (`ICP_DESCRIPTION`) | Editable ICP settings page stored in the DB | One ICP per deployment is enough for the MVP. A settings page follows the same pattern as the LLM settings if it becomes necessary. |
| Generated gRPC and OpenAPI code committed | Generate during the Docker build | Builds need no codegen toolchain, diffs show contract changes in review, and `make proto-check` / `make openapi-check` catch drift. |
| goose migrations, embedded and applied on start | golang-migrate; a custom runner | Plain SQL files, embedded in the binary, no separate migration step to forget. |
| Fetch the lead's homepage in the AI service, one page, no JavaScript | LLM inference from lead data only; third-party enrichment APIs; headless-browser crawling | Grounds company insights in the company's own description at the cost of one bounded HTTP request. The AI service already makes outbound calls and owns all inputs to the prompts, and Go stays free of untrusted outbound traffic. Third-party APIs add keys and cost; a headless browser adds a large dependency and attack surface. |
| Website fixtures replayed through `httpx.MockTransport`: synthetic pages committed, real-site recordings local only | Live requests in tests; committing recorded third-party pages; a VCR-style cassette library | Tests stay offline and deterministic, and the real fetch code runs in CI against realistic markup. The repository redistributes no third-party content. Real-site recordings use the production fetcher and add coverage locally without being required. No extra test dependency. |
| Per-node models as **DB settings, edited in `/admin`**, sent with each request | Env vars per node; Python reads settings itself; Python pulls config from Go | Changes need no restart or redeploy. Go stays the single owner of persisted state. Python stays stateless and needs no cache invalidation. Each job records exactly which models produced it. |
| Validate models against the live `/models` list on save | Accept any string | A typo would otherwise break every later analysis. Failing at save time gives immediate feedback in the form. |
| Static `ADMIN_TOKEN` for superadmin | Full user and role system | Real protection for the only privileged surface at a fraction of the complexity of full auth. The upgrade path is a `role` column plus sessions. |
| No broker (Kafka/Redis) | Queue infrastructure | The DB-backed queue meets all requirements. One less moving part. |

---

## 16. Open Decisions

None at present. Settled decisions, with the alternatives considered, are recorded in §15.
