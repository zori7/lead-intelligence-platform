# AGENTS.md

Critical rules for anyone changing this repository. `SPEC.md` is the full specification;
this file is the short list that must not be broken. When a rule here conflicts with a
habit, the rule wins.

## Architecture invariants

- **Browser → Go → Python, one way.** Python never calls Go; results return on the gRPC
  stream Go opened.
- **Go is the only writer to PostgreSQL.** The Python service is stateless.
- **No ORM.** Hand-written, parameterized SQL through pgx. Never interpolate user input
  into SQL; dynamic parts (sort order, column names) come from fixed allowlists in code.
- **State transitions are conditional `UPDATE … WHERE status = '<expected>'`.** The first
  writer wins; no application-level locks.

## The API contract is generated: never hand-edit it

`api/openapi.yaml` is the single source of truth for the browser ↔ Go HTTP contract.

| Generated | From | Command |
|---|---|---|
| `backend/internal/api/openapi.gen.go`: Go types, chi server interface, embedded spec | `api/openapi.yaml` | `make openapi` |
| `frontend/src/lib/api/gen/*`: TypeScript types and zod schemas | `api/openapi.yaml` | `make openapi` |
| `backend/gen/`, `ai/src/lead_ai/gen/`: gRPC code | `proto/**/*.proto` | `make proto` |

- **Never edit generated files.** Change the spec, run the generator, commit both.
  `make openapi-check` and `make proto-check` fail when generated code is stale.
- **To add or change an endpoint or field:** edit `api/openapi.yaml` → `make openapi` →
  implement the new `api.ServerInterface` method (the build fails until you do) → update
  the frontend using the regenerated types.
- **Validation limits live in the spec only.** Go reads lengths, enums, patterns and
  nullability from the embedded spec at startup (`internal/lead/rules.go`); the frontend
  uses the generated zod schemas. Do not hard-code a limit in Go or TypeScript.
- **Database `CHECK` constraints mirror the spec.** Changing a limit or enum means editing
  the spec **and** adding a new migration.
- **Response shapes are tested against the spec** (`internal/lead/contract_test.go`). A new
  status code or field must be documented in the spec, or the test fails.

## Go (`backend/`)

- Errors are values. Wrap with `fmt.Errorf("context: %w", err)`. Domain errors are
  sentinels wrapping `apperr` kinds (e.g. `lead.ErrNotFound`).
- **Errors become HTTP responses in exactly one place: `httpx.WriteError`.** Handlers
  never write status codes for errors themselves. 5xx details go to logs, never to clients.
- Request bodies go through `httpx.DecodeJSON` (64 KB limit, unknown fields rejected).
- Pass `context.Context` as the first argument through every layer that does I/O.
- Depend on small interfaces at the consumer (`lead.Repository`), not concrete types.
- Tests are table-driven. Store tests use `TEST_DATABASE_URL` and skip without it.
- `gofmt` and `go vet` must be clean.

## Python (`ai/`)

- The service is stateless: no database, no settings of its own. Models arrive with each
  request; tests inject fakes through `configurable` (`llm_factory`, `http_client`,
  `resolver`). There is no runtime fake mode.
- Website fetching goes through `lead_ai.web.Fetcher` only, so the address guard applies to
  every request and redirect hop. Never fetch lead URLs with another client.
- Tests never touch the network. Website tests replay fixtures through `httpx.MockTransport`;
  commit only synthetic fixtures (fictional companies, `*.example`). Real-site recordings stay
  in the git-ignored `tests/fixtures/websites/recorded/` (SPEC.md D12).
- `uv run ruff check .` and `uv run ruff format --check .` must be clean.

## Database

- **Never edit a migration that has been applied.** Add a new numbered file under
  `backend/migrations/` (goose, embedded, applied on startup).
- Enums are `text` + `CHECK`, not PostgreSQL `ENUM`.
- `updated_at` is set by the application on every update.

## Frontend (`frontend/`)

- **Svelte 5 runes only:** `$state`, `$derived`, `$effect`, `$props`, `$bindable`. No
  `export let`, `$:`, stores or `createEventDispatcher`. Callback props, snippets.
- **Server state goes through TanStack Query** (`lib/api/queries.ts`). Components never
  call `fetch`. **Never destructure a query or mutation result**: read `query.data`;
  destructuring loses reactivity.
- No `$effect` for derived values. Use `untrack` when an effect must read a value without
  depending on it.
- URL state (filters, sort, page) lives in the query string, not in component state.
- **`{@html}` is forbidden.** All AI and user content renders as text.
- Imports use the `#lib/...` alias with explicit specifiers: `#lib/api/types.js`,
  `#lib/components/ui/button/index.js` (Node subpath imports need exact paths).
- `lib/components/ui/*` is shadcn-svelte source owned by this repo and may be edited.
  Reuse the theme tokens and the `glass`, `glass-popover`, `glow-edge` and `btn-glow`
  classes in `routes/layout.css` instead of one-off colours.
- `npm run check` must report zero errors and zero warnings; Prettier must be clean.

## Security and data handling

- Never log secrets, the admin token, contact emails or full notes.
- `contact_email` is never sent to the AI service or the LLM.
- No secrets in the repository: `.env` is ignored; `.env.example` holds placeholders only.

## Navigating the code

When a `.codegraph/` index exists at the repo root (built locally with `codegraph init`;
not committed), use CodeGraph **before** grep/find or reading files to locate or
understand code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one
  call: the relevant symbols' source plus the call paths between them, including hops grep
  cannot follow (e.g. Svelte component → TanStack Query mutation → API client). Name a
  file or symbol in the query to read its current source.
- **Shell** (always works): `codegraph explore "<symbol names or question>"`.

Without a `.codegraph/` directory, skip CodeGraph; indexing is the developer's choice.
`codegraph.json` ranks generated code and shadcn components below hand-written code.

## Before calling a change done

```bash
make test-go        # Go unit, contract and store integration tests (needs `make up`)
make test-ai        # Python unit tests (offline: fake LLM, website fixtures)
make test-web       # svelte-check
make openapi-check  # generated API code is current
make proto-check    # generated gRPC code is current
```

For UI changes, also exercise the change in a browser: `make up`, `make api`, `make web`.

## Writing style

Documentation, comments and commit messages use a neutral, professional tone and justify
decisions on engineering merit.
