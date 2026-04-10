# Crucible IAP — Architecture

## Overview

Crucible IAP is a self-hosted infrastructure automation platform. It orchestrates OpenTofu, Terraform, Ansible, and Pulumi runs with policy enforcement, built-in state storage, drift detection, and a full audit trail — all in a single Docker Compose stack.

## Component diagram

```text
Browser / CI
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│  Caddy (TLS termination, security headers, routing)         │
└─────┬──────────────────────┬───────────────────┬───────────┘
      │                      │                   │
      ▼                      ▼                   ▼
┌──────────────┐  ┌──────────────────┐  ┌──────────────────┐
│ Crucible UI  │  │  Crucible API    │  │     Grafana      │
│ (SvelteKit)  │  │  (Go / Echo)     │  │  (at /grafana)   │
└──────────────┘  └────────┬─────────┘  └──────────────────┘
                            │                      ▲
          ┌─────────────────┼──────────────┐       │ scrapes
          ▼                 ▼              ▼       │
     ┌─────────┐      ┌─────────┐  ┌──────────┐  ┌────────────┐
     │Postgres │      │  MinIO  │  │OPA engine│  │ Prometheus │
     │(DB +    │      │(state + │  │(embedded)│  │            │
     │ audit + │      │ plans + │  └──────────┘  └────────────┘
     │ River   │      │  logs)  │
     │ queue)  │      └─────────┘
     └────┬────┘
          │  River jobs
          ▼
  ┌───────────────────┐
  │  Worker / Drift   │
  │  Dispatcher (Go)  │
  └────────┬──────────┘
           │ docker SDK
           ▼
  ┌───────────────────┐
  │  Runner Container │
  │  (ephemeral)      │
  │  --read-only      │
  │  --cap-drop ALL   │
  │  tmpfs /workspace │
  │                   │
  │  tofu / tf /      │
  │  ansible / pulumi │
  └───────────────────┘
```

## Request flow — triggering a run

1. User clicks a run trigger in the UI (or the Dashboard approve button), or a git webhook fires
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
| --- | --- |
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

```text
users              — authenticated user accounts
organizations      — top-level tenants
organization_members — user ↔ org membership + role
stacks             — infrastructure stacks (repo + tool + config)
runs               — run lifecycle records (status machine)
state_locks        — distributed state locking for TF backend
policies           — OPA/Rego policy source + metadata
stack_policies     — many-to-many stack ↔ policy attachment
run_policy_results — per-policy evaluation outcome per run (deny/warn/pass)
org_policy_defaults — policies automatically applied to all stacks in an org
audit_events       — append-only partitioned audit log
```

## Policy evaluation hooks

| Hook | When | Blocks? |
| --- | --- | --- |
| `pre_plan` | Before plan starts | Yes |
| `post_plan` | After plan, before user confirmation | Yes (deny) |
| `pre_apply` | Before apply, after confirmation | Yes |
| `trigger` | After run completes | No (drives downstream stacks) |
| `login` | On SSO callback | Yes |

Policies are written in Rego and stored in PostgreSQL. They are compiled once (OPA `PrepareForEval`) and evaluated in microseconds per request with no network hop.

## Observability

Crucible ships Prometheus and Grafana as first-class components — not optional add-ons.

| Metric | Description |
| ------ | ----------- |
| `crucible_http_requests_total` | Request count by method, route, status |
| `crucible_http_request_duration_seconds` | Latency histogram |
| `crucible_runs_total` | Run completions by status and trigger type |
| `crucible_queue_depth` | Pending River jobs |
| `crucible_build_info` | Version and commit metadata |

The `/metrics` endpoint is exposed on the API's internal port and scraped by Prometheus from the Docker `backend` network. It is **not** proxied through Caddy — unreachable from the public internet by default.

Grafana is served at `/grafana` via Caddy. The bundled dashboard covers HTTP request rates, error rates, p50/p95/p99 latency, run completion rates, and queue depth.

## Drift detection

A background goroutine (`worker.StartDriftScheduler`) wakes every minute and checks for stacks that are overdue for a drift check. Each stack carries a `drift_schedule` (minutes) and `drift_last_run_at`. When `drift_last_run_at + schedule_interval ≤ now()`, a `proposed` run is created with `trigger=drift_detection` and `is_drift=true`. The plan output is surfaced in the UI as a drift alert.

Operators can also trigger a one-off drift check via the UI or `POST /api/v1/stacks/:id/drift`.

If `auto_remediate_drift=true` on the stack, the worker automatically queues a `tracked` run with `AutoApply=true` after a drift run finishes with a non-empty plan — no human confirmation required.

## Remote state sharing

Cross-stack `terraform_remote_state` is supported without sharing long-lived credentials. When a remote state source is added (stack A reads from stack B):

1. A dedicated stack token is minted on the source stack (B)
2. The token secret is encrypted using B's HKDF-derived vault key and stored in `stack_remote_state_sources`
3. At run time, the worker decrypts the secret and injects `CRUCIBLE_REMOTE_STATE_<SLUG>_{ADDRESS,USERNAME,PASSWORD}` env vars into stack A's runner container

Revoking the relationship deletes the token from stack B immediately.

## Artifact retention

A background goroutine (`worker.StartRetentionScheduler`) runs 5 minutes after startup then every 24 hours. It queries runs whose `finished_at` is older than the `artifact_retention_days` system setting and calls `storage.DeleteArtifacts` for each. Terraform state files are never deleted by this sweep — only plan binaries and run logs. Set `artifact_retention_days = 0` (the default) to retain indefinitely.

## Directory structure

```text
crucible-iap/
├── api/                    # Go backend
│   ├── cmd/crucible-iap/   # Main entrypoint (serve + migrate subcommands)
│   ├── internal/
│   │   ├── auth/           # OIDC PKCE, JWT middleware, Basic auth
│   │   ├── audit/          # Append-only audit log
│   │   ├── config/         # Configuration + startup validation
│   │   ├── db/             # PostgreSQL pool + auto-migrations
│   │   ├── metrics/        # Prometheus instrumentation + middleware
│   │   ├── policy/         # OPA/Rego evaluation engine
│   │   ├── policies/       # Policy CRUD handlers + stack assignment
│   │   ├── queue/          # River job queue client
│   │   ├── runner/         # Docker ephemeral container spawner
│   │   ├── runs/           # Run lifecycle handlers + SSE logs
│   │   ├── server/         # Echo HTTP server + route registration
│   │   ├── stacks/         # Stack CRUD handlers
│   │   ├── state/          # Terraform HTTP backend
│   │   ├── storage/        # MinIO client (state, plans, logs)
│   │   └── worker/         # River worker, log broker, drift scheduler
│   └── migrations/         # SQL migration files (golang-migrate)
├── ui/                     # SvelteKit frontend
│   └── src/
│       ├── lib/
│       │   ├── api/        # Typed API client
│       │   └── stores/     # Svelte 5 Rune stores
│       └── routes/         # File-based routing
├── runner/                 # Runner container image
│   ├── Dockerfile
│   └── entrypoint.sh       # Tool dispatcher (tofu/tf/ansible/pulumi)
├── deploy/                 # Docker Compose deployment
│   ├── docker-compose.yml  # Production stack
│   ├── docker-compose.dev.yml
│   ├── Dockerfile.api
│   ├── Dockerfile.ui
│   ├── caddy/Caddyfile
│   ├── prometheus/         # Prometheus scrape config
│   └── grafana/            # Grafana provisioning + dashboards
└── docs/                   # Operator and developer documentation
    ├── architecture.md     # This file
    ├── operator-guide.md   # Deployment, backup, monitoring
    ├── security.md         # Threat model, hardening checklist
    └── policies.md         # Rego policy authoring guide
```

## Images for logos and icons

Place assets in `ui/static/`:

```text
ui/static/
├── favicon.svg             # Browser tab icon (SVG preferred)
├── logo.svg                # Full logo (wordmark + icon)
├── logo-mark.svg           # Icon only (used in sidebar, small contexts)
└── logo-dark.svg           # Dark background variant (optional)
```

Reference them in SvelteKit with `/logo.svg` — the `static/` directory is served at the root.
