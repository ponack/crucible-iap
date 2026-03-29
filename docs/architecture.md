# Crucible IAP — Architecture

## Overview

Crucible IAP is a self-hosted infrastructure automation platform. It orchestrates OpenTofu, Terraform, Ansible, and Pulumi runs with policy enforcement, built-in state storage, drift detection, and a full audit trail — all in a single Docker Compose stack.

## Component diagram

```
Browser / CI
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│  Caddy (TLS termination, security headers, routing)         │
└────────────────────┬────────────────────┬───────────────────┘
                     │                    │
                     ▼                    ▼
          ┌──────────────────┐  ┌──────────────────┐
          │  Crucible API    │  │  Crucible UI     │
          │  (Go / Echo)     │  │  (SvelteKit SSR) │
          └────────┬─────────┘  └──────────────────┘
                   │
       ┌───────────┼───────────────┬──────────────┐
       ▼           ▼               ▼              ▼
  ┌─────────┐ ┌─────────┐  ┌──────────┐  ┌──────────────┐
  │Postgres │ │  MinIO  │  │OPA engine│  │ River queue  │
  │(primary │ │(state + │  │(embedded │  │ (Postgres-   │
  │ DB +    │ │ plans + │  │ in API)  │  │  backed)     │
  │ audit)  │ │  logs)  │  └──────────┘  └──────┬───────┘
  └─────────┘ └─────────┘                        │
                                                  ▼
                                      ┌───────────────────────┐
                                      │  Worker Dispatcher    │
                                      │  (Go goroutine pool)  │
                                      └───────────┬───────────┘
                                                  │ docker SDK
                                                  ▼
                                      ┌───────────────────────┐
                                      │  Runner Container     │
                                      │  (ephemeral, per run) │
                                      │  --read-only          │
                                      │  --no-new-privileges  │
                                      │  --cap-drop ALL       │
                                      │  tmpfs /workspace     │
                                      │                       │
                                      │  tofu / terraform /   │
                                      │  ansible / pulumi     │
                                      └───────────────────────┘
```

## Request flow — triggering a run

1. User clicks "New Run" in the UI or a git webhook fires
2. `POST /api/v1/stacks/:id/runs` inserts a `runs` row (status: `queued`)
3. A River job is enqueued in PostgreSQL (transactional with the insert)
4. The Worker Dispatcher pulls the job and spawns an ephemeral Docker container
5. The container clones the repo, runs `tofu plan`, streams logs back via SSE
6. For `tracked` runs, status transitions to `unconfirmed` — user approves or discards
7. On confirm, a second River job runs `tofu apply`; status → `finished` or `failed`
8. Logs and plan artifacts are written to MinIO; audit event appended to PostgreSQL

## State management

Crucible implements the Terraform HTTP backend protocol natively. No external state backend (S3, GCS) is required.

Configure your OpenTofu/Terraform stack with:

```hcl
terraform {
  backend "http" {
    address        = "https://crucible-iap.example.com/api/v1/state/<stack-id>"
    lock_address   = "https://crucible-iap.example.com/api/v1/state/<stack-id>"
    unlock_address = "https://crucible-iap.example.com/api/v1/state/<stack-id>"
    username       = "<stack-token-id>"
    password       = "<stack-token-secret>"
  }
}
```

State files are stored versioned in MinIO. Locking uses PostgreSQL `state_locks` table — `INSERT` succeeds atomically or returns 423 if already locked.

## Security model

### Job container isolation

Every run executes in a fresh ephemeral container:

| Control | Value |
|---------|-------|
| `--read-only` | Root filesystem is read-only |
| `--no-new-privileges` | Prevents privilege escalation |
| `--cap-drop ALL` | No Linux capabilities |
| `--memory 2g` | Memory limit (configurable) |
| `--cpus 1.0` | CPU limit (configurable) |
| tmpfs `/workspace` | Workspace in RAM, gone on exit |
| Per-job JWT | Short-lived token scoped to one run |
| Ephemeral | Container removed automatically on exit |

### Authentication

- OIDC Authorization Code + PKCE — no client secrets stored in browser
- Authentik (bundled) or any OIDC provider (Okta, GitHub, Keycloak)
- Crucible issues its own short-lived JWTs (15 min) + refresh tokens (7 days)
- State backend uses HTTP Basic auth with per-stack token pairs

### Audit log

All state-mutating operations are recorded in `audit_events` before returning.
The table uses PostgreSQL rules to make it INSERT-only — UPDATE and DELETE are silently rejected at the database level, making the log tamper-resistant without a separate SIEM.

## Database schema (key tables)

```
users              — authenticated user accounts
organizations      — top-level tenants
organization_members — user ↔ org membership + role
stacks             — infrastructure stacks (repo + tool + config)
runs               — run lifecycle records (status machine)
state_locks        — distributed state locking for TF backend
policies           — OPA/Rego policy source + metadata
stack_policies     — many-to-many stack ↔ policy attachment
audit_events       — append-only partitioned audit log
```

## Policy evaluation hooks

| Hook | When | Blocks? |
|------|------|---------|
| `pre_plan` | Before plan starts | Yes |
| `post_plan` | After plan, before user confirmation | Yes (deny) |
| `pre_apply` | Before apply, after confirmation | Yes |
| `trigger` | After run completes | No (drives downstream stacks) |
| `login` | On SSO callback | Yes |

Policies are written in Rego and stored in PostgreSQL. They are compiled once (OPA `PrepareForEval`) and evaluated in microseconds per request with no network hop.

## Directory structure

```
crucible-iap/
├── api/                    # Go backend
│   ├── cmd/crucible-iap/   # Main entrypoint
│   ├── internal/
│   │   ├── auth/           # OIDC PKCE, JWT middleware
│   │   ├── audit/          # Append-only audit log
│   │   ├── config/         # Viper-based configuration
│   │   ├── db/             # PostgreSQL pool + migrations
│   │   ├── policy/         # OPA/Rego evaluation engine
│   │   ├── queue/          # River job queue client
│   │   ├── runner/         # Docker ephemeral container spawner
│   │   ├── runs/           # Run lifecycle handlers + SSE logs
│   │   ├── server/         # Echo HTTP server + route registration
│   │   ├── stacks/         # Stack CRUD handlers
│   │   ├── state/          # Terraform HTTP backend
│   │   ├── storage/        # MinIO client (state, plans, logs)
│   │   └── worker/         # River worker + log broker
│   └── migrations/         # SQL migration files
├── ui/                     # SvelteKit frontend
│   └── src/
│       ├── lib/
│       │   ├── api/        # Typed API client
│       │   └── stores/     # Svelte 5 Rune stores
│       └── routes/         # File-based routing
├── runner/                 # Runner container image
│   ├── Dockerfile
│   └── entrypoint.sh       # Tool dispatcher (tofu/tf/ansible/pulumi)
└── deploy/                 # Docker Compose deployment
    ├── docker-compose.yml  # Production stack
    ├── docker-compose.dev.yml
    ├── Dockerfile.api
    ├── Dockerfile.ui
    └── caddy/Caddyfile
```

## Images for logos and icons

Place assets in `ui/static/`:

```
ui/static/
├── favicon.svg             # Browser tab icon (SVG preferred)
├── logo.svg                # Full logo (wordmark + icon)
├── logo-mark.svg           # Icon only (used in sidebar, small contexts)
└── logo-dark.svg           # Dark background variant (optional)
```

Reference them in SvelteKit with `/logo.svg` — the `static/` directory is served at the root.
