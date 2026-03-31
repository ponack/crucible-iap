# Contributing to Crucible IAP

Thank you for your interest in contributing. This document covers everything you need to get started.

## Before you open a PR

- For significant features or design changes, open an issue first to discuss the approach.
- For bug fixes and small improvements, a PR directly is fine.
- Check existing issues and PRs to avoid duplicate work.

## Development setup

Requirements: Go 1.23+, Node.js 22+, pnpm, Docker Engine 24+

```bash
# Clone
git clone https://github.com/ponack/crucible-iap.git
cd crucible-iap

# Start dependencies (PostgreSQL + MinIO)
docker compose -f deploy/docker-compose.dev.yml up -d

# Start the API (auto-migrates on startup)
cd api && go run ./cmd/crucible-iap

# In a second terminal, start the UI
cd ui && pnpm install && pnpm dev
```

The UI dev server at `http://localhost:5173` proxies `/api` and `/auth` to `localhost:8080` automatically.

## Running tests

```bash
# API unit tests (no DB required)
cd api && go test ./internal/policy/... ./internal/config/...

# API integration tests (requires PostgreSQL)
export TEST_DATABASE_URL=postgres://crucible:crucible@localhost:5432/crucible_test?sslmode=disable
cd api && go test -race ./...

# UI type-check + lint
cd ui && pnpm check && pnpm lint
```

## Code conventions

### Backend (Go)

- All HTTP handlers return JSON. Errors use `echo.NewHTTPError` — the Echo middleware formats them as `{"message": "..."}`.
- Audit every state-mutating operation before returning the response (`audit.Record`).
- No direct writes to `runs.status` — go through the state machine helpers.
- Keep handlers thin: business logic in `internal/<domain>/`, HTTP wiring in `server/`.
- Use `slog` for structured logging, not `fmt.Println` or `log.Printf`.

### Frontend (SvelteKit + Svelte 5 Runes)

- Use `$derived` / `$state` / `$effect` — no legacy Svelte 4 stores in new code.
- Route params (`$page.params.foo`) are `string | undefined` — always assert non-null with `!` if the route guarantees the param.
- Every `<label>` must have a `for` attribute matching the input's `id` (a11y).
- Run `pnpm check` (svelte-check) before pushing; CI will fail on type errors.

### SQL migrations

- Add files to `api/migrations/` following the `NNN_description.up.sql` / `.down.sql` naming.
- Migrations run automatically on API startup (`migrate up`).
- Never modify an already-merged migration — add a new one.

## Pull request checklist

- [ ] `go test ./...` passes (or integration tests noted as skipped + why)
- [ ] `pnpm check` passes with no errors
- [ ] New API routes are covered by an audit event
- [ ] Security-sensitive changes are noted in the PR description
- [ ] Migration files include both `.up.sql` and `.down.sql`

## Commit style

Use conventional-ish commit messages:

```
feat: add stack-level env var injection
fix: prevent drift scheduler double-fire on restart
docs: update operator guide for v1.1
chore: bump go-oidc to v3.11
```

## Reporting security vulnerabilities

Please do **not** open public issues for security vulnerabilities. See [docs/security.md](docs/security.md) or use GitHub's private **Security → Report a vulnerability** feature.

## License

By contributing, you agree that your contributions will be licensed under the project's [AGPL-3.0-or-later](LICENSE) license.
