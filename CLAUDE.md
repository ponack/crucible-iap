# Crucible IAP — Claude Code Instructions

## Project overview

Crucible IAP is a self-hosted infrastructure automation platform (Spacelift alternative).

- **Backend:** Go 1.23+ — `api/` directory, module `github.com/ponack/crucible-iap`
- **Frontend:** SvelteKit 2 + Svelte 5 (Runes) — `ui/` directory
- **Deployment:** Docker Compose — `deploy/` directory
- **License:** AGPL-3.0-or-later (dual-license planned)

## Architecture

- `api/cmd/crucible-iap/` — main entrypoint; supports subcommands (`serve`, `migrate`)
- `api/internal/server/` — Echo HTTP server, route registration
- `api/internal/auth/` — OIDC/OAuth2 PKCE authentication
- `api/internal/stacks/` — stack CRUD and git integration
- `api/internal/runs/` — run lifecycle state machine
- `api/internal/state/` — Terraform HTTP backend state API
- `api/internal/runner/` — Docker-based ephemeral job runner
- `api/internal/policy/` — OPA/Rego policy evaluation (embedded)
- `api/internal/audit/` — append-only audit log
- `api/internal/db/` — PostgreSQL connection pool (pgx/v5)
- `api/migrations/` — SQL migration files (golang-migrate format)
- `ui/src/routes/` — SvelteKit file-based routing
- `ui/src/lib/` — shared components, stores, API client

## Key conventions

- All HTTP handlers return JSON; errors use `{"error": "message"}` shape
- JWT auth on all routes except `/auth/*` and `/health`
- Audit every state-mutating operation before returning the response
- Job containers: always ephemeral, `--read-only`, `--no-new-privileges`, tmpfs workspace
- State files stored in MinIO with versioning; never in PostgreSQL
- Run status transitions enforced as a state machine — no direct status field writes

## Development setup

```bash
make dev-deps     # start postgres + minio
make dev-api      # go run ./cmd/crucible-iap
make dev-ui       # pnpm dev
```

## Testing

- `make test-api` — Go tests, aim for coverage on state machine and policy eval
- `make test-ui` — Svelte component tests via vitest
- Integration tests use a real PostgreSQL instance (no mocking the DB)
