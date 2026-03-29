# Crucible IAP

**Infrastructure Automation Platform** — a self-hosted, privacy-first alternative to Spacelift.

> Status: Early development. Not production-ready.

## What it is

Crucible IAP is an open-source infrastructure automation platform that orchestrates OpenTofu, Terraform, Ansible, and Pulumi runs with policy enforcement, state management, drift detection, and a full audit trail — all running in your own infrastructure.

## Key features (planned)

- **GitOps-driven** — push to git, runs trigger automatically
- **Policy-as-code** — OPA/Rego for plan validation, approval gates, trigger chains
- **Built-in state backend** — Terraform HTTP backend compatible; no S3 setup required
- **Ephemeral job runners** — each run in an isolated, read-only Docker container
- **SSO via OIDC** — Authentik, Okta, GitHub, Keycloak, or any OIDC provider
- **Drift detection** — scheduled proposed runs with auto-remediation
- **Full audit log** — every action recorded, webhook delivery to SIEM
- **Docker Compose native** — single `docker compose up` to get started

## Quick start

```bash
cp .env.example .env
# Edit .env with your configuration
docker compose -f deploy/docker-compose.yml up -d
```

Crucible will be available at `https://localhost` (Caddy handles TLS automatically).

## Architecture

```
Caddy (TLS) → Crucible API (Go) + Crucible UI (SvelteKit)
                     ↓
              PostgreSQL + MinIO
                     ↓
         Worker Dispatcher → ephemeral Docker containers
                              (tofu / terraform / ansible / pulumi)
```

See [docs/architecture.md](docs/architecture.md) for the full design.

## Development

Requirements: Go 1.23+, Node.js 20+, pnpm, Docker

```bash
# Start dependencies
docker compose -f deploy/docker-compose.dev.yml up -d

# Run API
cd api && go run ./cmd/crucible

# Run UI
cd ui && pnpm dev
```

## License

[AGPL-3.0-or-later](LICENSE) — free to self-host. Commercial licenses available for proprietary use. See [COMMERCIAL.md](COMMERCIAL.md) for details.
