# Crucible IAP — Infrastructure Automation Platform

<img src="assets/crucible-logo-transparent.png" alt="Crucible IAP" width="160" />

A self-hosted, privacy-first alternative to Spacelift. Push code → Crucible plans it → review → apply. State, policy, and audit trail stay on your own infrastructure.

| | |
| :---: | :---: |
| ![Dashboard](assets/screenshots/dashboard.png) | ![Plan approval](assets/screenshots/run-plan-approval.png) |
| **Dashboard** — active runs, approvals, recent activity. | **Plan → confirm → apply** — review the plan diff, then click Confirm. |
| ![Stack detail with tags](assets/screenshots/stack-detail-tags.png) | ![Self-service blueprints](assets/screenshots/blueprint-list.png) |
| **Stack detail** — tags, pinning, dependency graph, drift config. | **Blueprints** — parameterized templates app teams deploy via a form. |
| ![Blueprint detail](assets/screenshots/blueprint-details.png) | ![Policy test playground](assets/screenshots/policy-test-playground.png) |
| **Blueprint detail** — param table, types, defaults, publish controls. | **Policy playground** — test OPA/Rego rules against synthetic input. |
| ![Policy git sources](assets/screenshots/policy-git-sources.png) | |
| **Policy GitOps** — sync `.rego` files from a git repo on push. | |

> **Not a technical user?** Visit the [Crucible IAP product page](https://www.forgedinfeatherstechnology.com/crucible-iap) for screenshots, feature highlights, and an overview of what Crucible can do for your team.

[![CI](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml/badge.svg)](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/ponack/crucible-iap)](https://github.com/ponack/crucible-iap/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/ponack/crucible-iap/api)](https://goreportcard.com/report/github.com/ponack/crucible-iap/api)
[![zizmor](https://img.shields.io/badge/zizmor-passing-brightgreen)](https://github.com/ponack/crucible-iap/actions/workflows/ci.yml)
![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)
![Status: Release Candidate](https://img.shields.io/badge/status-release%20candidate-green)

---

Crucible IAP orchestrates OpenTofu, Terraform, Ansible, and Pulumi runs with policy enforcement, built-in state storage, drift detection, and a full audit trail — all running in your own infrastructure with no SaaS dependency.

## Features

| Area | What you get |
| ---- | ------------ |
| **GitOps & runs** | Push or PR triggers a tracked run (plan → confirm → apply) with PR comments and commit status checks. Works with GitHub, GitLab, Gitea, and Gogs (HMAC-verified webhooks). Tracked / proposed / destroy / drift run types, auto-apply, and scheduled drift detection. |
| **Policy-as-code** | OPA/Rego policies at `pre_plan`, `post_plan`, `pre_apply`, `trigger`, `login`, and `approval` hooks. Blocking denies + non-blocking warnings. Approval gating on blast radius. GitOps sync — store `.rego` files in a git repo and Crucible syncs them on push (GitHub / GitLab, HMAC-verified). Standalone `/policies/test` playground with OPA evaluation trace. Full append-only audit log. |
| **State & runners** | OpenTofu, Terraform, Ansible, and Pulumi. Built-in Terraform HTTP backend on MinIO (zero config) or per-stack S3 / GCS / Azure Blob overrides. Each run in a fresh, read-only, capability-dropped Docker container — cosign-signed, digest-pinned runner image. |
| **Secrets & identity** | Per-stack OIDC workload identity federation with AWS, GCP, Azure, Vault, Authentik, or any OIDC IdP — no static cloud credentials. Encrypted stack env vars + reusable variable sets. External secret stores: AWS Secrets Manager, Vault KV v2, Bitwarden, Vaultwarden. See [`docs/security.md`](docs/security.md) for crypto details. |
| **Auth & access** | SSO via OIDC (Authentik, Okta, GitHub, Keycloak, anything OIDC) with PKCE, or single-operator local auth. Org-level RBAC (viewer / member / admin) with automatic IdP group → role mapping on login. Per-stack `viewer` / `approver` membership and service-account API tokens for CI. Rate-limited, hardened login. |
| **Observability** | Embedded Grafana on `/monitoring` (8 panels, 30 s refresh). Prometheus + Grafana shipped in-box. Per-stack Slack, Discord, Teams, Gotify, ntfy, and email notifications. Webhook delivery log with full payload inspection. Outgoing webhooks (HMAC-signed HTTP POST) for PagerDuty, ServiceNow, or custom tooling. Infracost monthly cost delta surfaced in every run. |
| **Deployment** | Single `docker compose up` — Caddy, API, Worker, UI, PostgreSQL, MinIO, Prometheus, Grafana. Let's Encrypt TLS via Caddy or the `external-proxy` profile for nginx / Traefik / your own Caddy. Optional bundled Authentik IdP. Built-in Terraform module registry, private provider registry, and stack templates. External agents: drop `docker-compose.agent.yml` + `.env.agent` on any remote host and run `docker compose -f docker-compose.agent.yml up -d` — no other files needed. |

Full feature list with security/crypto specifics: [`docs/operator-guide.md`](docs/operator-guide.md) and [`docs/security.md`](docs/security.md).

## Quick start

> **Just trying it out?** Follow [`docs/quickstart.md`](docs/quickstart.md) for a 15-minute local walkthrough (bundled Caddy on `https://localhost` with local auth) that takes you from `docker compose up` to a plan → confirm → apply run.

**Prerequisites:** `docker` (with compose v2), `openssl`, and a free port 443 (or 80/443 if using Let's Encrypt).

```bash
cp .env.example .env
# Edit .env — set CRUCIBLE_BASE_URL, CRUCIBLE_SECRET_KEY, POSTGRES_PASSWORD, etc.

# Create the runner network once (used by ephemeral job containers)
docker network create crucible-runner

docker compose up -d
```

Crucible IAP will be available at `https://localhost` (self-signed cert — accept the browser warning on first visit). Caddy provisions a real TLS certificate automatically when `CRUCIBLE_BASE_URL` is a public hostname and `CADDY_ACME_EMAIL` is set.

After login, the dashboard greets you with a one-click path to your first stack. Click **Create your first stack** to connect a Git repo — or [**use the `crucible-quickstart` template**](https://github.com/ponack/crucible-quickstart/generate) for a ready-to-run OpenTofu stack you can point Crucible at in under a minute.

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
    │  — or —
    │
    │           crucible-agent (external host, any cloud / on-prem)
    │           polls /api/v1/agent/claim → runs same Docker images
    │           streams logs back → reports outcome
    │
    └── /*  →  Crucible UI (SvelteKit SSR)
```

See [docs/architecture.md](docs/architecture.md) for the full design including security model, state backend protocol, and policy evaluation hooks.

## Documentation

| Document | Description |
| -------- | ----------- |
| **[docs/quickstart.md](docs/quickstart.md)** | **Your first stack in 15 minutes — start here** |
| [docs/architecture.md](docs/architecture.md) | Component diagram, request flow, security model, DB schema |
| [docs/operator-guide.md](docs/operator-guide.md) | Deployment, configuration reference, external worker agents, backup, monitoring, troubleshooting |
| [docs/security.md](docs/security.md) | Threat model, hardening checklist, vulnerability reporting. GitHub Actions workflows audited with [zizmor](https://github.com/zizmorcore/zizmor) on every PR. |
| [docs/policies.md](docs/policies.md) | Rego policy authoring guide — all policy types, input/output shapes, examples |
| [docs/guides/policy-gitops.md](docs/guides/policy-gitops.md) | Policy-as-code GitOps — sync `.rego` files from a git repo, webhook setup, mirror mode |
| [docs/policies/README.md](docs/policies/README.md) | Ready-to-use policy templates (no-destroy, blast radius, tags, EC2 allowlist, public access, approval gates, and more) |
| [docs/roadmap.md](docs/roadmap.md) | Expanded roadmap with implementation notes |
| [docs/guides/team-setup.md](docs/guides/team-setup.md) | Org roles, per-stack RBAC, approval policies, recommended starter policy set |
| [docs/guides/stack-templates.md](docs/guides/stack-templates.md) | Reusable stack configurations — create templates, deploy from them, good design practices |
| [docs/guides/blueprints.md](docs/guides/blueprints.md) | Self-service stack deployment — platform teams publish blueprints, app teams fill in a form and deploy |
| [docs/guides/provider-registry.md](docs/guides/provider-registry.md) | Private Terraform provider registry — publish binaries, configure Terraform, GPG signing |
| [docs/guides/deploy-aws-ecs.md](docs/guides/deploy-aws-ecs.md) | Production deployment on AWS — ECS Fargate (API), EC2 (worker), RDS, S3, ALB |
| [docs/guides/deploy-gcp-cloudrun.md](docs/guides/deploy-gcp-cloudrun.md) | Production deployment on GCP — Cloud Run (API), Compute Engine (worker), Cloud SQL, GCS |
| [docs/guides/deploy-azure-aca.md](docs/guides/deploy-azure-aca.md) | Production deployment on Azure — Container Apps (API), Azure VM (worker), PostgreSQL Flexible Server, Blob Storage |
| [docs/guides/aws.md](docs/guides/aws.md) | AWS credentials, S3 remote state backend, minimal IAM role, recommended AWS policies |
| [docs/guides/gcp.md](docs/guides/gcp.md) | GCP credentials, GCS remote state backend, Workload Identity Federation, minimal IAM roles |
| [docs/guides/azure.md](docs/guides/azure.md) | Azure credentials, Blob Storage remote state, federated identity, minimal role assignments |
| [docs/guides/aws-nuke.md](docs/guides/aws-nuke.md) | Automated AWS sandbox account cleanup with aws-nuke — three-stack setup, dry-run verification, and a self-resetting demo loop |
| [docs/guides/proxmox.md](docs/guides/proxmox.md) | End-to-end guide: managing Proxmox VMs with GitOps and policy enforcement |
| [docs/guides/ansible.md](docs/guides/ansible.md) | End-to-end guide: running Ansible playbooks with check → confirm → apply and policy enforcement |
| [docs/guides/pulumi.md](docs/guides/pulumi.md) | End-to-end guide: running Pulumi programs with preview → confirm → up and built-in MinIO state backend |
| [docs/guides/cloudflare.md](docs/guides/cloudflare.md) | Managing Cloudflare infrastructure as code — bootstrap with cf-terraforming, Crucible stack setup, OPA policies |
| [docs/guides/spacelift-migration.md](docs/guides/spacelift-migration.md) | Migrating from Spacelift to Crucible — concept mapping, state migration paths, and a working Cloudflare example |

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

## Cloud OIDC workload identity federation

Crucible acts as its own OIDC identity provider. Every run mints a short-lived signed JWT that cloud providers exchange for temporary credentials — no static cloud secrets are stored in Crucible. AWS, GCP, Azure, HashiCorp Vault, Authentik, Keycloak, Zitadel, Dex, and any OIDC-compatible IdP are supported; configure per-stack or set an org-level default in **Settings → General → Cloud OIDC Default**.

Per-provider setup (IAM trust policies, GCP workload identity pools, Entra federated credentials, Vault JWT roles, IdP exchange endpoints) and the JWT claims reference are in [`docs/operator-guide.md#cloud-oidc-workload-identity-federation`](docs/operator-guide.md#cloud-oidc-workload-identity-federation).

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
- [x] Monitoring page — Grafana panels embedded in the Crucible UI; eight panels: HTTP request rate, error rate, latency, run completions, queue depth, active runs, stack count, and run success rate; no separate Grafana tab needed for day-to-day observability
- [x] Org-level Gotify and ntfy defaults — configure default push notification endpoints in Settings; new stacks inherit them
- [x] Structured `/health` endpoint (DB status, version, uptime)
- [x] Policy management UI + drift detection scheduling
- [x] Operator documentation + security hardening guide
- [x] Stack-level environment variables — AES-256-GCM encrypted at rest, injected into runner containers; plain (non-secret) values are shown inline in the UI with per-row Edit/Replace actions; secret values remain write-only
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
- [x] External worker agents — deploy `crucible-agent` on any host with Docker access; agents poll the Crucible API for queued runs, execute them locally, and stream logs back; multiple agents per pool with `FOR UPDATE SKIP LOCKED` claim safety; stacks assign to a pool via Settings → Runner; separate optional binary, not bundled with the main image
- [x] Stack dependency graph — first-class upstream/downstream relationships with automatic downstream triggers after a successful apply; cycle detection via recursive CTE
- [x] Variable sets — define a shared group of env vars once and attach to multiple stacks; eliminates repetition across similar stacks
- [x] Stack templates / blueprints — create new stacks pre-filled from a saved template (tool, repo, branch, project root, auto-apply, drift settings)
- [x] Manual run with variable overrides — trigger a one-off run with temporary env var overrides without changing stack config
- [x] Service account API tokens — machine-readable tokens not tied to a user session, for CI pipelines and automation
- [x] CI linting — gofmt, go vet, gocyclo, ineffassign, misspell, staticcheck run on every PR; `make lint` target for local use
- [x] Ansible support — check → confirm → apply lifecycle with PLAY RECAP parsing, inventory auto-detection, and destroy playbook support
- [x] Pulumi support — preview → confirm → up lifecycle with built-in MinIO DIY S3 backend, TypeScript/JavaScript/Python runners, and changeSummary parsing for PR comments
- [x] Email notifications — SMTP (STARTTLS/SMTPS/plaintext) per-stack email address; fires on plan complete, run succeeded/failed; configured in Settings → Notifications
- [x] Webhook delivery log — record of incoming webhook payloads and whether they triggered a run, to debug missed or skipped events
- [x] Webhook re-delivery — re-trigger a run from any past delivery directly in the UI; replays the stored payload without requiring a new push or manual re-configuration
- [x] Environment TTL / auto-destroy — set a scheduled destroy time on any stack; a background scheduler fires a destroy run at the deadline and clears the TTL so it only fires once; prevents dev/feature environment sprawl
- [x] Terraform provider caching — provider binaries cached in MinIO after first download; subsequent runs restore from cache before `terraform init` so registry downloads are skipped; platform-filtered (linux_amd64 / linux_arm64); cache miss is non-fatal (falls back to registry automatically)
- [x] Terraform module registry — private module registry backed by MinIO; implements the Terraform Module Registry Protocol v1 (`/.well-known/terraform.json` discovery, versions, download, archive, search); publish via UI upload or git-tag auto-publish; README auto-extracted from archive and rendered as markdown; download count tracked; yank individual versions; service account tokens authenticate the Terraform CLI via `~/.terraformrc`
- [x] Resource explorer — browse Terraform state resources in the UI with filtering by type and address
- [x] Policy-as-code GitOps — sync `.rego` files from a git repository into policies on push; HMAC-verified webhooks, GitHub and GitLab support, mirror mode, policy type inference from directory structure or inline `# crucible:type` comments
- [x] Cost estimation — integrate with Infracost or similar to surface per-run cost delta alongside the plan summary
- [x] Fine-grained RBAC — per-stack viewer/approver roles in addition to the org-wide admin/member/viewer hierarchy; restricted stacks hidden from non-members
- [x] Exportable config — download a full JSON snapshot of stacks, policies, variable sets, templates, blueprints, and worker pools; non-secret env vars included in plaintext, secret vars as name-only placeholders; import on any instance with conflict-skip semantics (existing resources matched by name are never overwritten); both operations audit-logged; Settings → Export / Import tab
- [x] Custom run hooks — per-stack pre/post-plan and pre/post-apply bash scripts; configured in the stack settings UI, injected as env vars, executed inside the runner container; a non-zero exit fails the run
- [x] Context-aware approval policies — OPA `approval` hook evaluates plan context (run type, trigger, add/change/destroy counts, stack name) and returns `require_approval: true` to gate runs behind explicit sign-off; `deny` fails the run immediately
- [x] Startup config validation — `RUNNER_MEMORY_LIMIT` and `RUNNER_CPU_LIMIT` validated at boot; server refuses to start on invalid values rather than silently running containers unbounded
- [x] OIDC workload identity federation — Crucible acts as its own OIDC identity provider; each run receives a short-lived signed JWT; configure per-stack or set an org-level default in Settings → General to exchange it for temporary credentials with AWS, GCP, Azure, HashiCorp Vault, Authentik, or any generic OIDC-compatible IdP — no static secrets in Crucible
- [x] Notification test buttons — one-click test delivery for org-level Slack, Gotify, and ntfy endpoints directly from Settings → Notifications; confirms credentials are wired correctly without waiting for a run
- [x] Per-stack RBAC on remote state links — configuring a cross-stack `terraform_remote_state` link now requires at least approver role on the source stack; prevents org members from granting access to state they cannot manage
- [x] Auth endpoint rate hardening — per-IP rate limits tightened on `/auth/callback` (OAuth code exchange) and `/auth/refresh` (token renewal); service account tokens lock out after 20 failures per 5-minute window per IP
- [x] argon2id token hashing — service account and stack tokens upgraded from unsalted SHA-256 to argon2id (32 MB / 2 iterations / 1 thread); existing tokens lazily upgraded on first use with no forced rotation; new SA token format embeds UUID for O(1) point-lookup
- [x] httpOnly session cookies — refresh token moved from localStorage to a server-set `crucible_refresh` httpOnly `SameSite=Strict` cookie; access token kept in JS memory only; page-reload session restored transparently via silent cookie exchange
- [x] Stack dependency flow diagram — upstream/downstream relationships visualised as an SVG flow diagram on the stack detail page; bezier-curve arrows, indigo-highlighted current stack, clickable dep nodes; zero new dependencies
- [x] Org context switching after invite acceptance — accepting an org invite now immediately switches the active session to the invited org; users land on that org's stacks without needing to log out and back in
- [x] Org name editing — admins can rename their organisation from Settings → Organisation; slug remains stable for URL routing
- [x] Scheduled runs — cron-based plan, apply, or destroy runs per stack independent of code pushes; standard 5-field cron expressions (`0 2 * * *` = 2 am daily); next run time shown inline; worker polls every minute and enqueues the appropriate run type automatically
- [x] Stack locking / maintenance mode — per-stack flag that prevents new runs from being queued; operators set it before manual cloud console changes and release it when done; prevents race conditions during incident response; lock reason shown as an amber banner on the stack page
- [x] Run annotations — free-text operator note on any run ("deployed for hotfix", "reverting per oncall"); closes the audit gap between who triggered a run and why; inline click-to-edit on the run detail page
- [x] Generic outgoing webhooks — fire arbitrary HTTP POST on run state changes to PagerDuty, ServiceNow, Jira, or custom tooling; HMAC-signed, configurable per event type, delivery log with retry
- [x] SSO group → role mapping — automatically assign org roles from IdP group claims on every login; eliminates manual invite management for large teams on Authentik, Okta, Keycloak, or GitHub
- [x] Cost estimation — integrate Infracost (self-hosted server supported) to surface per-run monthly cost delta alongside the plan summary
- [x] IaC security scanning — built-in Checkov / Trivy scan post-plan; findings surfaced as structured results in the run detail alongside OPA policy output; configurable severity threshold to block apply
- [x] Private provider registry — full Terraform Provider Registry Protocol v1; upload binaries per OS/arch, SHA-256 checksums served dynamically, GPG public key management per namespace for `terraform providers lock`; air-gapped deployments reference providers via `source = "host/namespace/type"`
- [x] Per-stack run concurrency cap — set `max_concurrent_runs` on any stack; worker enforces the cap at job start and fails the run immediately if the limit is reached; 0 / unset = unlimited
- [x] Self-service infrastructure blueprints — platform teams define blueprints (repo, tool, params) and publish them; app teams deploy new stacks by filling in a form without touching IaC config; params are injected as encrypted `TF_VAR_*` env vars; string, number, bool, and select param types; per-param env prefix; deploy runs atomically in a single transaction
- [x] OPA policy test playground — standalone `/policies/test` page; pick any saved policy, paste synthetic JSON, run it and see allow/deny/warn/trigger results with optional OPA evaluation trace; genuine differentiator — neither Spacelift nor TF Cloud has this built in
- [x] PR preview environments — auto-create a stack from a template when a PR opens, auto-destroy when it closes; branch name drives workspace isolation; pairs with stack dependencies for full per-PR environment chains
- [x] AI run troubleshooting — one-click "Explain failure" on failed runs; sends log context to the Claude API and returns a structured root-cause explanation and suggested fix; API key set in Settings → AI troubleshooting (no container restart needed) or via `ANTHROPIC_API_KEY` env var
- [x] Multi-provider AI troubleshooting — AI provider is now configurable from Settings; choose Anthropic (Claude) or any OpenAI-compatible endpoint (OpenAI, OpenRouter, OpenWebUI, Ollama, etc.); custom base URL and model fields added; existing `ANTHROPIC_API_KEY` deployments carry over automatically via migration 059
- [x] Stack clone — one-click Clone button on any stack detail page; copies tool/repo config, runner image, hooks, worker pool, env vars (re-encrypted under the new stack's key), and tags into a new draft stack; state, runs, tokens, and notification secrets are not copied
- [x] Run re-trigger — ↺ Re-trigger button on any terminal run (failed, canceled, discarded, finished); creates a new run with the same type and variable overrides against the current branch head; respects stack lock and per-stack RBAC
- [x] AWS Cloud OIDC session duration default — saving an AWS Cloud OIDC config without specifying a session duration no longer returns a 500; defaults to 3600 s
- [x] OIDC token injection — runner no longer fails with "container rootfs is marked read-only"; token written from the entrypoint into tmpfs after container start instead of via CopyToContainer before start
- [x] Worker OIDC base URL — `CRUCIBLE_BASE_URL` added to the worker service environment; without it the JWT issuer was empty and no OIDC token was minted, causing "No valid credential sources found" on every OIDC-configured run
- [x] Provider cache restore crash — empty provider cache no longer kills the run; `jq -r '.keys[]'` on an empty array fed an empty key into the restore loop, causing `rm -f` to target the cache directory itself and exit non-zero under `set -e`
- [x] Dark / light mode switcher — system preference detected on first visit; persists to `localStorage`; sun/moon toggle in the sidebar header; native browser elements (scrollbars, inputs) follow the theme via CSS `color-scheme`; no flash of wrong theme on hard reload; smooth 150ms transition when toggling
- [x] Sidebar header polish — logo size increased; version badge and theme toggle relocated from the bottom footer to the top header bar (flush right of the wordmark), keeping the footer to email + sign out only; dashboard page no longer capped at a fixed max-width
- [x] Forge UI — teal-slate design system with OKLCH hue-shifted zinc scale; Heroicons icon sidebar with grouped nav sections (Core / Config / Ops) and teal active-state left-border; RunLifecycle 5-step rail on every run detail page; terminal-style log viewer; full indigo→teal color migration across all 33 UI pages
- [x] Toast notifications — all 48 browser-native `alert()` popups replaced with a teal-accented toast store; error / success / info variants auto-dismiss after 4.5 s and stack in the bottom-right corner; `aria-live` polite for screen readers
- [x] Consistent empty states — shared `EmptyState` component with a teal icon badge, heading, and subtext on all 10 list pages (stacks, runs, policies, worker pools, variable sets, blueprints, templates, module registry, provider registry, audit log)
- [x] Cold / Hot / Neutral Forge theme switcher — three named forge themes selectable from color-swatch dot buttons in the sidebar; Cold Forge (teal-slate, hue 185 OKLCH, `#2DD4BF`), Hot Forge (copper-amber, hue 42 OKLCH, `#D4883C`), Neutral Forge (standard zinc, hue 286, `#818cf8` indigo accent — pre-redesign look); each combines independently with dark / light mode for six total palette combinations; persists to `localStorage` with anti-FOUC protection
- [x] Dashboard redesign — priority-zone layout (Action Required → Live → History); stat cards with Heroicons icons and alert-colour tinting when values are non-zero; "+ New stack" CTA in the header; update-available notice demoted to a subtle link; version display removed from the main content area
- [x] Ember / Frost / Gold Forge themes — three additional forge palette options: Ember Forge (hue 15, `#E05252` deep red), Frost Forge (hue 220, `#60A5FA` arctic blue), Gold Forge (hue 78, `#F5C542` warm gold); sidebar now shows six dot swatches; each has a paired light-mode variant and anti-FOUC protection
- [x] Stack tags — org-scoped, color-coded labels managed in Settings → Tags; attach multiple tags to any stack; tag pills visible in the stack list and stack detail header
- [x] Tag filtering — filter the stacks list and runs list by one or more tags via a dropdown with color swatches and active filter pills
- [x] Stack pinning — pin any stack to float it to the top of the stacks list; toggle via the pin icon on each row
- [x] Bulk approve — "Approve all (N)" button on the dashboard Action Required zone; confirms all pending runs in one click
- [x] Starter policies — [`ponack/crucible-policies`](https://github.com/ponack/crucible-policies) public repo with 8 ready-made OPA policies (destroy protection, cost gates, business-hours controls, and more); one-click quick-connect from the Policy Git Sources page
- [x] Policy sync fix — `org_id` ambiguity in the policy git sync worker caused all sync jobs to fail silently; fixed column qualification in the JOIN query
- [x] Policy input shape — cost fields (`cost_add`, `cost_change`, `cost_remove`) added to OPA policy input; `approval` type added to UI type labels, badges, templates, and input schema
- [x] Blueprint export/import — export any blueprint as a portable JSON file (`schema_version` + all config + params, no ids or timestamps); import via file upload or paste on the blueprints list page; imported blueprints start as drafts; audit-logged
- [x] Run navigation correctness — navigating away from a confirmed plan+apply run no longer shows the previous run's data; stale `pollFinal` callbacks from the confirm flow are now cancelled on navigation; the stack detail page refreshes its run list every 10 s so newly created runs appear without a page reload
- [x] Blueprint params fix — blueprints with no parameters no longer cause the detail page to hang on "Loading…"; Go's `json:"omitempty"` was silently dropping the `params` field from API responses when empty; the detail and deploy pages now guard against a missing field defensively
- [x] Blueprint detail hang (all blueprints) — `tool_version`, `repo_url`, `runner_image`, and `drift_schedule` were also tagged `omitempty`; any blueprint with empty values for those fields caused the same Svelte 5 re-render hang; all four fields now always present in API responses
- [x] Blueprint detail and deploy page hang (final fix) — `Array.sort()` mutates in place; calling it on a Svelte 5 reactive proxy throws `state_unsafe_mutation`, which Svelte catches and rolls back — leaving the page permanently on "Loading..."; fixed by spreading a copy before sorting: `[...params].sort(...)`
- [x] Stack clone — duplicate any existing stack into a new one pre-filled with all config (tool, repo, branch, env vars, policies, notifications, tags, runner pool, hooks, drift settings); clone action available from the stack list and stack detail page; cloned stack starts with no runs and no state
- [x] Run re-trigger — queue a new run directly from any historical run without navigating back to the stack; retrigger button on the run detail page creates a fresh run for the same stack and immediately navigates to it
- [x] Discord notifications — per-stack Discord incoming webhook; fires on plan complete, run succeeded/failed; org-level default in Settings → Notifications inherited by new stacks; one-click test delivery
- [x] Microsoft Teams notifications — per-stack Teams incoming webhook or Power Automate HTTP trigger; same event coverage as Discord; org-level default; one-click test delivery
- [x] Audit log JSON export — audit log export now supports `?format=json` in addition to CSV; returns a JSON array of all matching events respecting active filters; available from the audit log page alongside the existing CSV export
- [x] Audit log action filter — free-text action input replaced with a category dropdown (run.*, stack.*, policy.*, org.*, blueprint.*, varset.*, sa.*, tag.*) for cleaner filtering without knowing exact action names
- [x] Approval timeout auto-discard — configurable global timeout (in hours) in Settings → Notifications; a background sweep every 5 minutes discards runs stuck in `unconfirmed` or `pending_approval` beyond the deadline and records a `run.approval_expired` audit event; set to 0 to disable
- [x] Approval expiry on startup — the approval expiry sweep now runs immediately when the server starts, not only on the first 5-minute tick; runs that expired during a server restart or downtime are caught and discarded within seconds of boot
- [x] Audit JSON export streaming — large audit exports no longer buffer all rows into memory before responding; rows are marshalled and flushed individually so memory usage is O(1) regardless of result set size
- [x] OIDC provider init failure — if the OIDC provider URL is unreachable at startup the server now logs a structured error with the issuer URL and exits cleanly rather than panicking with a Go stack trace
- [ ] CLI tool — `crucible` command-line client for triggering runs, reading stack status, and approving/discarding pending runs from CI pipelines or terminals without opening the UI
- [ ] OpenTofu / Terraform version pinning — per-stack IaC binary version selector; runner pulls the pinned version rather than always using the image default; supports semantic version ranges; versions cached in MinIO
- [ ] State version diff — side-by-side view of Terraform state between any two runs; highlights added, changed, and removed resources; accessible from the run detail and resource explorer pages
- [x] GitHub App integration — first-class GitHub App support; register an App in Settings → GitHub App (encrypted credentials, auto-generated webhook + setup URLs); install flow with HMAC-signed state round-trip; single global webhook per App replacing per-stack webhook URLs; per-stack installation picker; push and PR events dispatched via App auth; installation access tokens used for PR comments and commit status checks; backwards-compatible with existing PAT-based stacks
- [x] GitHub App setup guidance — Settings → GitHub App now shows a 6-step setup guide with required permissions (Contents/Metadata/Pull requests/Commit statuses), webhook events (Push/Pull request/Create), and inline "where to find this" hints for every registration form field; post-registration URL section labels each URL with the exact GitHub field name to paste into; stack detail page shows an "App token active" / "Using PAT" status badge so the current auth mode is visible at a glance
- [x] GitHub App discoverability — Settings → GitHub App shows a "Ready — now connect to stacks" callout once an installation exists; stack Integrations section shows a cross-reference hint linking directly to the GitHub App authentication section when an App is available but the stack is not yet connected; Callback URL vs Setup URL distinction clarified in both the setup guide and the URL wiring section
- [ ] Stack health scoring — composite score per stack based on recent run success rate, drift frequency, cost trend, and policy failures; displayed as a colour-coded badge in the stacks list; sortable and filterable
- [ ] Multi-org support — single Crucible instance hosting multiple isolated organizations; targets MSPs and consultancies managing multiple client environments from one deployment
- [ ] RustFS object storage — replace the bundled MinIO with RustFS for a fully Rust-native, S3-compatible object store; same API surface, lower resource footprint
- [ ] Projects / Spaces — hierarchical org → project → stack layout; per-project RBAC, integration defaults, and notification routing; stacks belong to exactly one project; replaces the current flat-tag-only organisation for large deployments
- [ ] Bitbucket and Azure DevOps VCS — first-class webhook ingestion, PR comments, and commit status checks for Bitbucket Server / Cloud and Azure DevOps Repos; same auth and integration model as the existing GitHub / GitLab / Gitea support
- [ ] ChatOps approvals — approve, discard, and re-trigger runs directly from Slack and Microsoft Teams notifications; signed action buttons embedded in the existing per-stack webhook payloads; no extra app installation per channel
- [ ] Terragrunt support — first-class wrapper around Terraform / OpenTofu with `run-all` orchestration, dependency resolution, and `remote_state` config inheritance; treated as its own `tool` value alongside opentofu / terraform / ansible / pulumi
- [ ] Customer-managed encryption keys (BYOK) — bring-your-own-key for stack secrets via AWS KMS, HashiCorp Vault Transit, or Azure Key Vault; per-stack data-encryption keys are wrapped by the customer-controlled KMS instead of the bundled `secret_key`
- [ ] Audit log SIEM streaming — push audit events live to Splunk HEC, Datadog Logs, or generic syslog / HTTP endpoints; replaces or supplements the CSV / JSON export for compliance-bound deployments
- [ ] Compliance policy packs — installable OPA bundles for SOC 2, CIS AWS Foundations, HIPAA, and PCI-DSS; one-click attach to any stack; updates pulled from the [`ponack/crucible-policies`](https://github.com/ponack/crucible-policies) repo
- [ ] Plan diff between runs — side-by-side comparison of resource changes between any two plans on the same stack; surfaces drift introduced by config edits between runs
- [ ] Budget alerts — per-stack monthly budget threshold with Slack / email alerts when projected (Infracost) cost crosses 80% / 100%; cost trend chart on the stack detail page showing the last 30 runs
- [ ] In-app run analytics — charts for run success rate, average plan time, drift frequency, and policy violation trends rendered from the Prometheus metrics already collected; eliminates the need to leave Crucible for Grafana for day-to-day observability
- [ ] Continuous validation — periodic policy re-evaluation against current stack state independent of run lifecycle; surfaces drifted compliance posture (e.g., a stack that was compliant at apply time but has since failed a tighter policy); configurable interval per stack

## License

[AGPL-3.0-or-later](LICENSE) — free to self-host forever. Commercial licenses available for proprietary or SaaS use.

---

Built by [Forged in Feathers Technology](https://www.forgedinfeatherstechnology.com) · [Crucible IAP product page](https://www.forgedinfeatherstechnology.com/crucible-iap)
