# Crucible IAP — Roadmap

The canonical checklist lives in [README.md](../README.md#roadmap). This document provides expanded context, rationale, and implementation notes for items that need more than one line.

---

## Completed

### Code Quality & Developer Experience ✓

CI lint job (`gofmt`, `go vet`, `staticcheck`, `gocyclo -over 15`, `ineffassign`, `misspell`) runs on every PR. `make lint` runs the same checks locally. Go Report Card badge added to README.

### Runner Image Hardening ✓

- Pin runner base image to a digest (not `:latest`) to prevent supply-chain drift
- Add `HEALTHCHECK` to runner Dockerfile
- Publish signed runner image via `cosign` on release

### Ansible Support ✓

Ansible runs follow the same check → confirm → apply lifecycle as OpenTofu. `--check --diff` output is captured as the plan artifact; PLAY RECAP is parsed for `changed`/`unreachable` counts for PR comments.

### Pulumi Support ✓

Pulumi runs follow the same preview → confirm → up lifecycle. `pulumi preview --json` parsed for add/change/delete counts. MinIO auto-configured as DIY S3 backend — no Pulumi Cloud account required.

> Stack references for cross-stack outputs are not yet implemented — use remote state sources as a workaround.

### Stack Dependency Graph ✓

After a stack applies successfully, all configured downstream stacks automatically receive a tracked run. DAG cycle detection prevents loops. Respects the downstream stack's `auto_apply` flag.

### Variable Sets ✓

Named groups of env vars defined once and attached to multiple stacks. Encrypted at rest with the same AES-256-GCM vault as stack env vars. Injection order: external secrets → variable sets → stack env vars (stack wins on collision).

### Fine-Grained RBAC ✓

Per-stack viewer/approver roles in addition to the org-wide admin/member/viewer hierarchy. Restricted stacks are hidden from non-members entirely.

### Terraform Provider Caching ✓

Provider binaries cached in MinIO after first download. Subsequent runs restore from cache before `terraform init`. Platform-filtered (linux_amd64 / linux_arm64). Cache miss is non-fatal and falls back to the registry automatically.

### Custom Run Hooks ✓

Per-stack pre/post-plan and pre/post-apply bash scripts. Configured in the stack settings UI, injected as env vars, executed inside the runner container. A non-zero exit fails the run.

### Context-Aware Approval Policies ✓

OPA `approval` hook evaluates plan context (run type, trigger, add/change/destroy counts, stack name) and returns `require_approval: true` to gate runs behind explicit sign-off.

### Environment TTL / Auto-Destroy ✓

Configurable destroy-at timestamp on any stack. A background scheduler fires a destroy run at the deadline and clears the TTL so it only fires once.

### OIDC Workload Identity Federation ✓

Crucible acts as its own OIDC identity provider. Each run receives a short-lived signed JWT. Configure per-stack or set an org-level default in Settings → General to exchange it for temporary AWS, GCP, or Azure credentials.

---

## Near Term

### Scheduled Runs

Beyond drift detection, allow arbitrary runs (plan, apply, or destroy) to be triggered on a cron schedule. Drift always creates a `proposed` run — this feature would allow `tracked` or `proposed` runs on any schedule without a code push.

**Use cases:** nightly applies to keep environments fresh, morning plan-only checks to surface drift before the team starts work, weekend scheduled destroys for dev environments.

**Implementation notes:**

- New `stack_schedules` table: `stack_id`, `cron_expression`, `run_type`, `enabled`
- Extend the drift scheduler goroutine (or separate goroutine) to evaluate cron expressions
- UI: schedule management section on stack detail page, similar to drift detection settings
- Cron expression validated on save; standard 5-field format (`minute hour dom month dow`)
- Disabled stacks skip scheduled triggers

### Stack Locking / Maintenance Mode

A per-stack flag that prevents new runs from being queued. Operators set it before making manual cloud console changes or during incident response; unset it when done.

**Why it matters:** Without locking, a push during a manual console intervention creates a race condition between the runner and the operator's in-progress changes. Spacelift and TF Cloud both expose this as a first-class operation.

**Implementation notes:**

- `is_locked` boolean column on `stacks` with an optional `lock_reason` TEXT
- API: `POST /stacks/:id/lock` and `DELETE /stacks/:id/lock` (admin/operator only)
- Worker checks `is_locked` before dequeuing a run job; returns a clear error message
- UI: prominent lock badge on the stack header; lock/unlock button for admins
- Audit event recorded on lock and unlock

### Run Annotations / Operator Notes

Allow operators to leave a free-text note on any run — "deployed for the Q2 hotfix", "reverting due to oncall alert #1234", "manual apply to fix drift after console change".

**Why it matters:** The audit log records who triggered what, but not *why*. A one-line annotation field closes this gap without requiring an external ticketing integration. Useful for incident retrospectives and compliance reviews.

**Implementation notes:**

- `annotation` TEXT column on `runs`, nullable
- `PATCH /api/v1/runs/:id` to set/update it (operator role required on the stack)
- Shown in the run detail header and the runs list tooltip
- Included in audit events and any outgoing webhook payloads

### Generic Outgoing Webhooks

Fire an arbitrary HTTP POST to a configured URL on run state changes. Currently Crucible supports Slack, Gotify, ntfy, and SMTP — but teams also need PagerDuty, ServiceNow, Jira, custom CMDBs, and internal tooling.

**Implementation notes:**

- New `outgoing_webhooks` table: `stack_id` (nullable for org-level), `url`, `secret` (HMAC), `event_types[]`, `headers` (JSONB for custom auth headers)
- Payload: same shape as existing Slack notifications + full run object
- HMAC-SHA256 signature header (same pattern as inbound webhooks)
- Delivery log with retry (up to 3 attempts, exponential backoff)
- UI: manage in Settings → Notifications alongside existing channels

### SSO Group → Role Mapping

Automatically assign org roles and stack roles based on IdP group membership. Eliminates manual invite management for teams with many stacks and frequently-changing rosters.

**Implementation notes:**

- Map IdP claims (e.g. `groups` array in OIDC token) to Crucible org roles
- Config in Settings → Members: list of `{ claim_value: "platform-team", role: "admin" }` mappings
- Applied on every login — role is re-evaluated from fresh token claims, not cached
- Per-stack group mapping: assign a stack role to all members of an IdP group
- Works with any OIDC provider that includes group claims (Authentik, Okta, Keycloak, GitHub teams via custom claims)

---

## Medium Term

### Exportable Config

Export the full instance configuration as a single compressed archive and import it on another instance. Useful for backup, DR, staging-to-prod promotion, and migration.

**What gets exported:** stacks, policies, variable sets, org settings, integration metadata. Secret values excluded by default; opt-in with `--password` (AES-256-GCM + Argon2id-derived key).

**What is always excluded:** run history, audit log, state files, users/membership.

**Conflict strategy:** import skips existing objects by name/slug by default; `--overwrite` replaces them.

### Cost Estimation

Integrate Infracost to surface estimated monthly cost delta alongside the plan summary (`+$12.40/mo`, `-$3.20/mo`). Infracost has a self-hosted server option that aligns with Crucible's self-hosted philosophy.

**Implementation notes:**

- Run `infracost breakdown --path <plan-json>` in the runner container post-plan
- Parse JSON output and store `cost_add`, `cost_change`, `cost_remove` on the run
- Surface in the run header alongside `+N ~N -N` plan delta badge
- Requires `INFRACOST_API_KEY` (or self-hosted server URL) set on the stack or as an org default

### IaC Security Scanning

Run Checkov (or Trivy) against the plan or workspace before apply. Surface findings as structured policy results — not just log lines — so they appear in the run detail alongside OPA results.

**Implementation notes:**

- Execute `checkov --directory . --output json` as part of the post-plan step
- Parse findings into `run_policy_results` rows (one per finding, policy_type=`security`)
- UI: dedicated security findings section in the run detail, collapsible by severity
- Block apply on CRITICAL findings (configurable threshold per stack)
- Works as an alternative or complement to OPA policies

### Private Provider Registry

Extend the existing module registry (already shipping) to serve custom Terraform providers. Critical for air-gapped deployments that cannot reach registry.terraform.io and for teams distributing internal providers.

**Implementation notes:**

- Implement the Terraform Provider Registry Protocol (`/v1/providers/` endpoints)
- Providers stored in MinIO alongside modules
- Upload via UI or API (same pattern as modules)
- `terraform_provider_mirror` block in `~/.terraformrc` to point at Crucible
- Signing: support GPG key upload per provider namespace for `terraform providers lock`

### Per-Stack Run Concurrency Cap

Allow limiting a specific stack to N concurrent runs (typically 1 for production stacks). Currently only a global cap exists.

**Implementation notes:**

- `max_concurrent_runs` INT column on `stacks`, nullable (null = use global setting)
- Worker checks active run count for the stack before dequeuing
- UI: field in stack settings alongside the runner image override
- Useful for production stacks where concurrent applies would conflict, and for slow stacks that should not consume the global quota

### Self-Service Infrastructure Blueprints

Parameterized stack creation with a visible input form. Like stack templates, but with named user-facing input fields (environment name, region, instance size) that are rendered as form controls — the user fills them in without touching the stack config.

**Why it matters:** Platform engineering teams can publish blueprints; app teams self-serve new environments without writing Terraform or understanding stack configuration. Spacelift calls this "blueprints"; env0 calls it "self-service workflows".

**Implementation notes:**

- New `blueprints` table: name, description, base template ID, `inputs[]` schema (label, key, type, default, validation regex)
- Blueprint inputs are stored as variable overrides on the created stack
- UI: public blueprint catalog page; fill form → creates stack in one click
- Input values rendered as `TF_VAR_*` env vars or injected into the stack's env var set

### OPA Policy Test UI

Write a synthetic run payload and evaluate it against a saved policy inline — see deny/warn/pass output without attaching the policy to a stack and waiting for a real run.

**Why it matters:** Currently policy authors are blind until a real run hits. A test UI dramatically reduces authoring friction and prevents "attach policy → trigger run → see error → fix → repeat" cycles. Neither Spacelift nor TF Cloud has this built in — it would be a genuine differentiator.

**Implementation notes:**

- `POST /api/v1/policies/:id/eval` — accepts a JSON body matching the policy input schema, returns evaluation result
- UI: "Test policy" panel on the policy detail page with a JSON editor pre-filled with a realistic example payload
- Example payload populated from the most recent real run that hit this policy (if any)

---

## Long Term / Speculative

### Multi-node / HA

- PostgreSQL connection pooling (PgBouncer)
- Stateless API — run multiple API instances behind a load balancer
- Remote Docker host support for runner containers (not just local socket)

### External Worker Agents

Lightweight agent binary that connects to the primary instance and executes jobs locally on the agent host. Decouples runner capacity from the API host; no Docker socket on the central server required. Pull-based model (agent polls for work) so no public ingress is needed on the agent.

### PR Preview Environments

Automatically create a stack (from a designated template) when a PR is opened; automatically destroy it when the PR is closed or merged. Branch name drives workspace isolation.

**Why it matters:** Feature branch testing without manual environment management. Teams get a fresh, isolated environment per PR with zero ops overhead. Spacelift and env0 both offer this. Particularly powerful when combined with stack dependencies (networking → compute → app per PR).

**Implementation notes:**

- New webhook event type: `pr_opened`, `pr_closed`, `pr_merged`
- Stack setting: "preview environment template" — points to a blueprint or template
- On `pr_opened`: clone template → create stack named `preview-<pr-number>`, set branch to PR head
- On `pr_closed`/`pr_merged`: queue a destroy run, then delete the stack after destroy completes
- PR comment with preview environment URL posted on stack creation

### AI Run Troubleshooting

When a run fails, offer a one-click "Explain failure" button that sends the run log and plan artifact to the Claude API and returns a structured explanation: root cause, suggested fix, and relevant documentation links.

**Why it matters:** Terraform/OpenTofu error messages are often cryptic. Spacelift Intelligence does this. For Crucible, this is a natural integration given the platform already has all run context in one place.

**Implementation notes:**

- `POST /api/v1/runs/:id/explain` — server-side call to Claude API with run log as context
- Streamed response displayed in the run detail page (same SSE infrastructure as log streaming)
- Opt-in per org (requires `ANTHROPIC_API_KEY` in `.env`)
- Log content truncated/summarised before sending; state file contents never sent

### Policy-as-Code GitOps

Manage Rego policies via a dedicated Git repository using the same PR review + merge flow as infrastructure code. Policy changes go through review, get proposed runs that validate syntax, and merge to apply.

### Multi-Org Support

Single Crucible instance hosting multiple isolated organizations. Needed for MSPs and consultancies managing multiple client environments from one deployment.

**Implementation notes:** Significant schema impact — every table needs an `org_id` FK (most already have it). Main complexity is in auth (cross-org token isolation), billing hooks, and the admin UI for org management. Not worth the complexity until there is clear demand from multi-tenant operators.

### Pulumi Stack References

First-class cross-stack output sharing for Pulumi stacks using `StackReference`, rather than the `terraform_remote_state` workaround. Requires Pulumi state backend awareness and token scoping per stack reference.

### Dark / Light Mode Switcher

User-selectable UI theme with a toggle in the nav or settings, defaulting to the system preference (`prefers-color-scheme`). Dark mode is currently hardcoded.

**Implementation notes:**

- Detect system preference via `window.matchMedia('(prefers-color-scheme: dark)')` on load; persist override in `localStorage`
- Toggle stored preference via a button in the sidebar or user settings
- Drive theme via a `data-theme` attribute on `<html>` and CSS custom properties (or Tailwind's `darkMode: 'class'` strategy)
- No backend changes needed — purely client-side preference
