# Crucible IAP

![Crucible IAP](assets/CrucibleIAP_master_TRANSPARENT.png)

**Crucible IAP - Infrastructure Automation Platform** — a self-hosted, privacy-first alternative to Spacelift.

[![CI](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml/badge.svg)](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/ponack/crucible-iap)](https://github.com/ponack/crucible-iap/releases/latest)
![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)
![Status: Beta](https://img.shields.io/badge/status-beta-blue)

---

Crucible IAP orchestrates OpenTofu, Terraform, Ansible, and Pulumi runs with policy enforcement, built-in state storage, drift detection, and a full audit trail — all running in your own infrastructure with no SaaS dependency.

## Features

### GitOps & runs

- **GitOps-driven** — push to a branch triggers a tracked run (plan → confirm → apply); open a PR and Crucible plans it, posts a comment, and sets a commit status check
- **GitHub, GitLab, Gitea, and Gogs** — HMAC-verified webhooks (modern X-Hub-Signature-256 and legacy per-forge fallbacks), PR/MR comments, and commit status checks; custom instance base URLs supported for self-hosted deployments
- **Run types** — tracked, proposed (plan-only), destroy, and drift — manual or webhook-triggered
- **Auto-apply** — skip the confirmation gate for low-risk stacks
- **Drift detection** — scheduled proposed runs alert when live state diverges from code

### Policy and compliance

- **Policy-as-code** — OPA/Rego policies evaluate in microseconds; attach multiple policies per stack
- **Policy hooks** — `pre_plan`, `post_plan`, `pre_apply`, `trigger` (downstream stacks), `login`
- **Deny and warn** — blocking denials halt the run; non-blocking warnings surface to the operator
- **Full audit log** — every state-mutating action recorded; append-only and tamper-resistant at the database level

### Infrastructure

- **Multi-tool** — OpenTofu, Terraform, Ansible, and Pulumi in the same platform
- **Flexible state storage** — built-in Terraform/OpenTofu HTTP backend backed by MinIO (zero config); or override per-stack with Amazon S3 / S3-compatible (built-in Sig v4), Google Cloud Storage (RSA-SHA256 JWT), or Azure Blob Storage (SharedKeyLite) — credentials encrypted at rest
- **Ephemeral job runners** — each run in a fresh Docker container: read-only rootfs, `--cap-drop ALL`, tmpfs workspace, per-job scoped JWT — container is gone when the job ends; runner image digest-pinned and cosign-signed on every release; manual runs support per-run variable overrides (highest env precedence, not persisted to stack config)
- **Stack env vars** — AES-256-GCM encrypted at rest with per-stack HKDF-derived keys salted with a deployment-unique secret; injected into runners at job time, never logged or returned by the API
- **Variable sets** — named collections of env vars defined once and attached to multiple stacks; values are write-only, encrypted with the same AES-256-GCM vault; injection order: external secrets → variable sets → stack env vars (stack wins on collision)
- **External secret stores** — pull secrets from AWS Secrets Manager (built-in Sig v4, no SDK), HashiCorp Vault KV v2 (token or AppRole), Bitwarden Secrets Manager (AES-256-CBC E2E decryption), or Vaultwarden / self-hosted Bitwarden (PBKDF2-SHA256 / Argon2id master key derivation + AES-CBC vault crypto); merged with built-in env vars, built-in takes precedence on collision

### Auth and access

- **SSO via OIDC** — Authentik (bundled optional), Okta, GitHub, Keycloak, or any OIDC provider; PKCE always enforced
- **Local auth** — single operator account for deployments without an IdP
- **RBAC** — viewer / member / admin roles enforced at the API layer; org invite flow with single-use tokens
- **Service account API tokens** — long-lived `ciap_…` tokens for CI pipelines and automation scripts; hashed at rest, shown once at creation, role-scoped, last-used tracked
- **Security hardening** — CSP headers, HSTS, failed login auditing, weak credential detection on startup

### Observability and operations

- **Monitoring page** — Grafana panels embedded directly in the Crucible UI at `/monitoring`; all five dashboard panels (HTTP request rate, error rate, latency p50/p95/p99, run completions, queue depth) rendered inline with a 30 s auto-refresh; link to the full Grafana dashboard for admin use
- **Prometheus + Grafana** — built-in dashboards for HTTP latency, run throughput, and queue depth; Grafana served at `{BASE_URL}/grafana`
- **Push notifications** — per-stack Slack webhooks, Gotify, ntfy, and email (SMTP) event subscriptions: plan complete, run succeeded, run failed; configurable defaults in Settings → Notifications; per-stack overrides on the stack detail page
- **Webhook delivery log** — every inbound webhook request is recorded (forge, event type, outcome, skip reason, linked run) for debugging missed or skipped events
- **Structured health endpoint** — `/health` reports DB status, version, and uptime
- **Automatic migrations** — schema migrations run on startup; `migrate` subcommand available for manual control

### Deployment

- **Stack templates** — save a stack configuration as a reusable template (tool, repo, branch, project root, policies, auto-apply, drift settings); new stacks can be pre-filled from a template in one click
- **Single `docker compose up`** — Caddy, API, Worker, UI, PostgreSQL, MinIO, Prometheus, and Grafana in one command
- **Separated API and Worker** — the HTTP API server and the Docker job runner run as distinct containers; the API has no Docker socket, the worker has no public ports
- **Zero-config TLS** — Let's Encrypt via Caddy; bring your own cert or reverse proxy with the `external-proxy` profile
- **Bundled Authentik** — optional `--profile authentik` for a fully self-hosted IdP

## Quick start

```bash
cp .env.example .env
# Edit .env — set CRUCIBLE_BASE_URL, CRUCIBLE_SECRET_KEY, POSTGRES_PASSWORD, etc.

# Create the runner network once (used by ephemeral job containers)
docker network create crucible-runner

docker compose up -d
```

Crucible IAP will be available at `https://localhost`. Caddy provisions a TLS certificate automatically (set `CADDY_ACME_EMAIL` for Let's Encrypt).

## Deployment options

### Bundled Caddy (default)

Zero-config TLS via Let's Encrypt or self-signed. Everything in one `docker compose up`.

```bash
docker compose up -d
```

### External reverse proxy

Use your existing nginx, Traefik, or Caddy instance instead.

```bash
docker network create crucible-runner   # if not already created
docker compose --profile external-proxy up -d
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
docker compose --profile authentik up -d

# External proxy + Authentik
docker compose --profile external-proxy --profile authentik up -d
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
    │              River job queue (PostgreSQL)
    │                     │
    │           Crucible Worker (separate container)
    │           (no public ports, has Docker socket)
    │                     │
    │           Docker SDK → ephemeral runner container
    │                        (tofu / terraform / ansible / pulumi)
    │
    └── /*  →  Crucible UI (SvelteKit SSR)
```

See [docs/architecture.md](docs/architecture.md) for the full design including security model, state backend protocol, and policy evaluation hooks.

## Documentation

| Document | Description |
| -------- | ----------- |
| **[docs/quickstart.md](docs/quickstart.md)** | **Your first stack in 15 minutes — start here** |
| [docs/architecture.md](docs/architecture.md) | Component diagram, request flow, security model, DB schema |
| [docs/operator-guide.md](docs/operator-guide.md) | Deployment, configuration reference, backup, monitoring, troubleshooting |
| [docs/security.md](docs/security.md) | Threat model, hardening checklist, vulnerability reporting |
| [docs/policies.md](docs/policies.md) | Rego policy authoring guide with examples |
| [docs/roadmap.md](docs/roadmap.md) | Expanded roadmap with implementation notes |
| [docs/guides/proxmox.md](docs/guides/proxmox.md) | End-to-end guide: managing Proxmox VMs with GitOps and policy enforcement |
| [docs/guides/ansible.md](docs/guides/ansible.md) | End-to-end guide: running Ansible playbooks with check → confirm → apply and policy enforcement |
| [docs/guides/pulumi.md](docs/guides/pulumi.md) | End-to-end guide: running Pulumi programs with preview → confirm → up and built-in MinIO state backend |

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
plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs contains msg if {
  input.resource_changes[_].change.actions[_] == "delete"
  msg := "destroy operations require an explicit destroy run"
}

warn_msgs contains msg if {
  input.resource_changes[_].change.actions[_] == "update"
  msg := sprintf("resource %s will be updated", [input.resource_changes[_].address])
}
```

Policy types: `post_plan` (most common), `pre_plan`, `pre_apply`, `trigger` (downstream stacks), `login`.

## Development

Requirements: Go 1.25+, Node.js 22+, pnpm, Docker

```bash
# Start dependencies (PostgreSQL + MinIO only)
docker compose -f deploy/docker-compose.dev.yml up -d

# Start API (migrations run automatically on startup)
cd api && go run ./cmd/crucible-iap

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
- [x] List pagination on all collection endpoints
- [x] RBAC enforcement (viewer / member / admin) + org invite flow
- [x] Settings UI — member management, role changes, invite links
- [x] Automatic migrations on startup
- [x] Prometheus metrics + Grafana dashboards (built-in, served at `/grafana`)
- [x] Monitoring page — Grafana panels embedded in the Crucible UI; no separate Grafana tab needed for day-to-day observability
- [x] Org-level Gotify and ntfy defaults — configure default push notification endpoints in Settings; new stacks inherit them
- [x] Structured `/health` endpoint (DB status, version, uptime)
- [x] Policy management UI + drift detection scheduling
- [x] Operator documentation + security hardening guide
- [x] Stack-level environment variables — AES-256-GCM encrypted at rest, injected into runner containers
- [x] PR/MR feedback — plan summary comments and commit status checks on GitHub and GitLab
- [x] Slack notifications — configurable per-stack event subscriptions
- [x] Gotify notifications — per-stack Gotify server URL + encrypted app token; fires on plan complete, run succeeded/failed
- [x] ntfy notifications — per-stack ntfy topic URL + optional Bearer token; fires on plan complete, run succeeded/failed
- [x] External secret store integrations — AWS Secrets Manager (Sig v4, no SDK), HashiCorp Vault KV v2 (token + AppRole), Bitwarden Secrets Manager (E2E decryption), Vaultwarden (self-hosted; PBKDF2/Argon2id + AES-CBC vault crypto)
- [x] Multi-cloud state backend options — S3 / S3-compatible (Sig v4), GCS (JWT + OAuth2), Azure Blob Storage (SharedKeyLite)
- [x] Gitea and Gogs webhook support — modern X-Hub-Signature-256 compat + legacy X-Gitea-Signature fallback
- [x] Per-stack VCS provider config (github/gitlab/gitea) with custom instance base URL for self-hosted deployments
- [x] Remote state sharing — cross-stack `terraform_remote_state` with per-relationship tokens minted on the source stack and injected as env vars at run time
- [x] Auto-remediate drift — automatically queue a tracked apply run after a drift detection run reports changes
- [x] Artifact retention policy — configurable retention period for plan files and run logs; deleted on a daily background sweep
- [x] Org-level notification defaults — pre-fill Slack webhook and VCS provider config for new stacks
- [x] Intuitive dashboard — landing page showing org-wide health at a glance: active/failed runs, stacks with drift, recent audit events, and inline approve/discard/cancel actions without navigating into individual stacks
- [ ] External worker agents — additional runner nodes that connect to the primary instance, allowing job execution capacity to be scaled out independently
- [ ] Stack dependency graph — first-class upstream/downstream relationships with automatic downstream triggers after a successful apply
- [x] Variable sets — define a shared group of env vars once and attach to multiple stacks; eliminates repetition across similar stacks
- [x] Stack templates / blueprints — create new stacks pre-filled from a saved template (tool, repo, branch, project root, auto-apply, drift settings)
- [x] Manual run with variable overrides — trigger a one-off run with temporary env var overrides without changing stack config
- [x] Service account API tokens — machine-readable tokens not tied to a user session, for CI pipelines and automation
- [x] CI linting — gofmt, go vet, gocyclo, ineffassign, misspell, staticcheck run on every PR; `make lint` target for local use
- [x] Email notifications — SMTP (STARTTLS/SMTPS/plaintext) per-stack email address; fires on plan complete, run succeeded/failed; configured in Settings → Notifications
- [x] Webhook delivery log — record of incoming webhook payloads and whether they triggered a run, to debug missed or skipped events
- [ ] Terraform provider caching — vendor provider plugins into MinIO so repeated runs skip registry downloads
- [ ] Terraform module registry — private module registry backed by MinIO for internal module distribution without an external registry dependency
- [x] Resource explorer — browse Terraform state resources in the UI with filtering by type and address
- [ ] Policy-as-code GitOps — manage Rego policies via a dedicated repository with the same PR review + merge flow as infrastructure code
- [ ] Cost estimation — integrate with Infracost or similar to surface per-run cost delta alongside the plan summary
- [ ] Fine-grained RBAC — resource-level permissions (per-stack viewer/approver roles) rather than a single org-wide role
- [ ] Exportable config — export full instance configuration (stacks, policies, variable sets, env var names) as a compressed, importable archive for backup, migration, or cloning between environments

## License

[AGPL-3.0-or-later](LICENSE) — free to self-host forever. Commercial licenses available for proprietary or SaaS use.
