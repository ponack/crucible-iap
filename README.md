# Crucible IAP

![Crucible IAP](ui/static/logo-new.png)

**Crucible IAP - Infrastructure Automation Platform** — a self-hosted, privacy-first alternative to Spacelift.

[![CI](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml/badge.svg)](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml)
![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)
![Status: Early Development](https://img.shields.io/badge/status-early%20development-orange)

---

Crucible IAP orchestrates OpenTofu, Terraform, Ansible, and Pulumi runs with policy enforcement, built-in state storage, drift detection, and a full audit trail — all running in your own infrastructure with no SaaS dependency.

## Features

- **GitOps-driven** — push to a branch or open a PR and runs trigger automatically via GitHub or GitLab webhooks
- **Policy-as-code** — OPA/Rego for plan validation, approval gates, and trigger chains
- **Built-in state backend** — Terraform/OpenTofu HTTP backend compatible; no S3 required
- **Ephemeral job runners** — each run in an isolated, read-only Docker container
- **SSO via OIDC** — Authentik, Okta, GitHub, Keycloak, or any OIDC-compatible provider
- **Drift detection** — scheduled proposed runs with optional auto-remediation
- **Full audit log** — every action recorded; append-only, tamper-resistant at the database level
- **Flexible deployment** — bundled Caddy for zero-config TLS, or bring your own reverse proxy

## Quick start

```bash
cp .env.example .env
# Edit .env — set CRUCIBLE_BASE_URL, CRUCIBLE_SECRET_KEY, POSTGRES_PASSWORD, etc.
docker compose -f deploy/docker-compose.yml up -d
```

Crucible IAP will be available at `https://localhost`. Caddy provisions a TLS certificate automatically (set `CADDY_ACME_EMAIL` for Let's Encrypt).

## Deployment options

### Bundled Caddy (default)

Zero-config TLS via Let's Encrypt or self-signed. Everything in one `docker compose up`.

```bash
docker compose -f deploy/docker-compose.yml up -d
```

### External reverse proxy

Use your existing nginx, Traefik, or Caddy instance instead.

```bash
docker compose -f deploy/docker-compose.yml --profile external-proxy up -d
```

The API binds to `127.0.0.1:8080` and the UI to `127.0.0.1:3000` by default. Point your proxy at those addresses. Ready-to-use config examples are in [`deploy/proxy-examples/`](deploy/proxy-examples/):

| File | Proxy |
| ---- | ----- |
| [`nginx.conf`](deploy/proxy-examples/nginx.conf) | nginx |
| [`traefik.yml`](deploy/proxy-examples/traefik.yml) | Traefik v3 |
| [`caddy-standalone.Caddyfile`](deploy/proxy-examples/caddy-standalone.Caddyfile) | Caddy (external) |

### Bundled Authentik IdP (optional)

Add `--profile authentik` to include a self-hosted Authentik instance. Skip this if you already have an IdP.

```bash
# Default Caddy + Authentik
docker compose -f deploy/docker-compose.yml --profile authentik up -d

# External proxy + Authentik
docker compose -f deploy/docker-compose.yml --profile external-proxy --profile authentik up -d
```

## Architecture

```text
GitHub / GitLab webhook
    │
    ▼
Browser / CI
    │
    ▼
Reverse proxy (Caddy bundled, or nginx / Traefik / your own)
    │
    ├── /auth, /api, /health  →  Crucible API (Go + Echo)
    │                                │
    │                     ┌──────────┼──────────────┐
    │                     ▼          ▼              ▼
    │               PostgreSQL     MinIO       OPA engine
    │               (DB + queue    (state,     (embedded,
    │                + audit log)   plans,      Rego)
    │                               logs)
    │                     │
    │              River job queue
    │                     │
    │           Worker dispatcher (Go)
    │                     │
    │           Docker SDK → ephemeral runner container
    │                        (tofu / terraform / ansible / pulumi)
    │
    └── /*  →  Crucible UI (SvelteKit SSR)
```

See [docs/architecture.md](docs/architecture.md) for the full design including security model, state backend protocol, and policy evaluation hooks.

## Connecting a Git repository

Every stack has a unique webhook URL and secret. Find them on the stack detail page in the UI, or via the API:

```http
GET /api/v1/stacks/:id
→ { "webhook_url": "https://crucible.example.com/api/v1/webhooks/<stack-id>",
    "webhook_secret": "..." }
```

### GitHub

1. Go to your repository → **Settings** → **Webhooks** → **Add webhook**
2. **Payload URL** — paste the `webhook_url` from above
3. **Content type** — `application/json`
4. **Secret** — paste the `webhook_secret`
5. **Which events?** — choose **Let me select individual events**, then tick **Pushes** and **Pull requests**
6. Click **Add webhook**

Crucible will now create a **tracked** run (plan → confirm → apply) on every push to the stack's configured branch, and a **proposed** run (plan only, no apply) on every pull request.

### GitLab

1. Go to your project → **Settings** → **Webhooks** → **Add new webhook**
2. **URL** — paste the `webhook_url`
3. **Secret token** — paste the `webhook_secret`
4. Tick **Push events** and **Merge request events**
5. Click **Add webhook**

### Rotating the secret

If the secret is ever exposed, rotate it without downtime:

```bash
curl -X POST https://crucible.example.com/api/v1/stacks/<id>/webhook/rotate \
  -H "Authorization: Bearer <access-token>"
# → { "webhook_secret": "<new-secret>" }
```

Update the secret in your repository's webhook settings immediately after.

## Run types

| Trigger | Run type | What happens |
| --- | --- | --- |
| Push to tracked branch | `tracked` | Plan → wait for human confirmation → apply |
| Push to tracked branch (`auto_apply=true`) | `tracked` | Plan → auto-apply if policy passes |
| Pull request / Merge request | `proposed` | Plan only — result posted, no apply |
| Manual (from UI or API) | `tracked` / `proposed` / `destroy` | As configured |
| Drift detection | `proposed` | Plan only — alerts on diff |

## State backend configuration

Point any OpenTofu or Terraform stack at Crucible's built-in state backend:

```hcl
terraform {
  backend "http" {
    address        = "https://crucible.example.com/api/v1/state/<stack-id>"
    lock_address   = "https://crucible.example.com/api/v1/state/<stack-id>"
    unlock_address = "https://crucible.example.com/api/v1/state/<stack-id>"
    username       = "<stack-id>"
    password       = "<stack-token-secret>"
  }
}
```

Stack tokens are managed in the UI (Settings → Tokens) or via the API. State is stored in MinIO with full version history.

## Policy-as-code

Attach OPA/Rego policies to stacks to enforce guardrails before runs are allowed to apply:

```rego
package crucible

# Deny any plan that would destroy a resource
plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "delete"
  msg := "destroy operations require an explicit destroy run"
}

warn_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "update"
  msg := sprintf("resource %s will be updated", [input.resource_changes[_].address])
}
```

Policy types: `post_plan` (most common), `pre_apply`, `pre_plan`, `login`.

## Development

Requirements: Go 1.25+, Node.js 22+, pnpm, Docker

```bash
# Start dependencies (PostgreSQL + MinIO only)
docker compose -f deploy/docker-compose.dev.yml up -d

# Run migrations and start API
cd api && go run ./cmd/crucible-iap migrate && go run ./cmd/crucible-iap

# Run UI
cd ui && pnpm install && pnpm dev
```

The UI dev server proxies `/api` and `/auth` to the API at `localhost:8080` automatically.

### Running tests

```bash
# Unit tests (no DB needed)
cd api && go test ./internal/policy/...

# Integration tests (requires PostgreSQL)
export TEST_DATABASE_URL=postgres://crucible:crucible@localhost:5432/crucible_test?sslmode=disable
cd api && go test -race ./...
```

## Roadmap

- [x] OIDC authentication with personal org auto-provisioning
- [x] Stack management (CRUD, tokens, policies)
- [x] Run lifecycle state machine (queued → planning → unconfirmed → applying → finished)
- [x] Terraform/OpenTofu HTTP state backend
- [x] Ephemeral Docker runner with MinIO plan artifact storage
- [x] OPA/Rego policy evaluation engine
- [x] Append-only audit log (tamper-resistant at DB level)
- [x] GitHub and GitLab webhook ingestion (push + PR/MR events)
- [ ] Audit log partition auto-creation + list pagination
- [ ] RBAC enforcement + org invite flow
- [ ] Prometheus metrics + structured health endpoint
- [ ] Policy management UI + drift detection scheduling
- [ ] Operator documentation + security hardening guide

## License

[AGPL-3.0-or-later](LICENSE) — free to self-host forever. Commercial licenses available for proprietary or SaaS use.
