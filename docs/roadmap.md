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

### Scheduled Runs ✓

Cron-based plan, apply, or destroy runs per stack, independent of code pushes. Standard 5-field cron expressions (`0 2 * * *` = 2 am daily). Next run time shown inline on the stack page. Worker polls every minute and enqueues the appropriate run type automatically. Disabled stacks skip scheduled triggers.

### Stack Locking / Maintenance Mode ✓

Per-stack flag that prevents new runs from being queued. Operators set it before making manual cloud console changes or during incident response; unset when done. Lock reason shown as an amber banner on the stack page. Audit event recorded on lock and unlock. Worker checks the flag before dequeuing and fails the run immediately with the lock reason as the error message.

### Run Annotations / Operator Notes ✓

Free-text operator note on any run ("deployed for the Q2 hotfix", "reverting due to oncall alert #1234"). Closes the audit gap between who triggered a run and why. Inline click-to-edit on the run detail page. Included in outgoing webhook payloads.

### Generic Outgoing Webhooks ✓

Fire arbitrary HTTP POST to a configured URL on run state changes — covers PagerDuty, ServiceNow, Jira, custom CMDBs, and internal tooling. HMAC-SHA256 signed, configurable per event type, delivery log with up to 3 retry attempts and exponential backoff. Managed per-stack in Settings → Notifications.

### SSO Group → Role Mapping ✓

Automatically assign org roles from IdP group claims on every login. Eliminates manual invite management for teams on Authentik, Okta, Keycloak, or GitHub. Role is re-evaluated from fresh token claims on each login — not cached. Configured in Settings → Members.

### Cost Estimation ✓

Infracost integration surfaces per-run monthly cost delta (`+$12.40/mo`, `-$3.20/mo`) alongside the plan summary. Self-hosted Infracost pricing server supported. Configure `INFRACOST_API_KEY` (or `INFRACOST_PRICING_API_ENDPOINT`) in Settings → General; runner injects the key automatically.

### IaC Security Scanning ✓

Built-in Checkov / Trivy scan runs post-plan. Findings surfaced as structured results in the run detail alongside OPA policy output — not just log lines. Configurable severity threshold to block apply on CRITICAL findings. Scan tool and threshold configured in Settings → General.

### Per-Stack Run Concurrency Cap ✓

`max_concurrent_runs` INT column on stacks (null = unlimited). Worker enforces the cap at job start and fails the run immediately if the limit is reached. Configured in stack settings alongside runner image overrides. Useful for production stacks where concurrent applies would conflict.

### OPA Policy Test Playground ✓

Standalone `/policies/test` page: pick any saved policy, paste synthetic JSON, run it and see allow/deny/warn/trigger results with optional OPA evaluation trace. Sample payloads pre-filled per policy type. No stack attachment or real run required.

### PR Preview Environments ✓

Auto-create a stack from a designated template when a PR opens; auto-destroy when the PR closes or merges. Branch name drives workspace isolation. Pairs with stack dependencies for full per-PR environment chains (networking → compute → app). PR comment posted with the preview environment URL on creation.

### External Worker Agents ✓

Lightweight `crucible-agent` binary deploys on any host with Docker access. Agents poll the Crucible API for queued runs, execute them locally, and stream logs back. Multiple agents per pool with `FOR UPDATE SKIP LOCKED` claim safety. Stacks assign to a pool via Settings → Runner. Separate optional binary, not bundled with the main image.

### AI Run Troubleshooting ✓

One-click "Explain failure" on failed runs. Sends log context to the Claude API and returns a structured root-cause explanation and suggested fix. Opt-in per org via `ANTHROPIC_API_KEY` in `.env`. Log content truncated before sending; state file contents never sent.

### Dark / Light Mode Switcher ✓

System preference (`prefers-color-scheme`) detected on first visit; persists to `localStorage`. Sun/moon toggle in the sidebar footer. Implemented via Tailwind v4 CSS variable overrides — the zinc scale is flipped at `:root.light` so all 32 pages respond with zero component changes. Native browser elements (scrollbars, form inputs) follow the theme via CSS `color-scheme`. No flash of wrong theme on hard reload via an inline anti-FOUC script in `<head>`. Smooth 150ms transition on background and text colour when toggling.

### Exportable Config ✓

Export the full instance configuration as a JSON snapshot and re-import it on another instance. Covers stacks, policies, variable sets, stack templates, blueprints, and worker pool definitions. Non-secret env var and variable set values are included in plaintext; secret values are always omitted. Import skips existing resources (matched by name) — nothing is overwritten.

### Self-Service Infrastructure Blueprints ✓

Parameterized stack creation with a visible input form. Platform teams define blueprints with named fields (environment name, region, instance size); app teams self-serve new stacks by filling in the form — no Terraform knowledge required. Blueprint params are injected as `TF_VAR_*` env vars on the created stack. Blueprints must be published before they are visible to non-admin members.

### Private Provider Registry ✓

Full Terraform Provider Registry Protocol v1 endpoint for distributing custom and internal providers. Critical for air-gapped deployments. Provider binaries are uploaded per OS/arch and stored in MinIO. SHA-256 checksums are computed at upload and served via a dynamic `SHA256SUMS` endpoint. GPG public keys can be registered per namespace for `terraform providers lock` compatibility. Discovery via `.well-known/terraform.json` alongside `modules.v1`.

### Policy-as-Code GitOps ✓

Store `.rego` policy files in a git repository and Crucible syncs them automatically on every push — no manual copy-paste into the UI. A background worker fetches a VCS archive (GitHub or GitLab, including self-hosted), extracts `.rego` files, and upserts each one as a policy. Policy type is inferred from the parent directory name (`post_plan/`, `approval/`, etc.) or an inline `# crucible:type` comment, defaulting to `post_plan`. HMAC-SHA256 verified push webhooks. Optional **mirror mode** deletes policies that no longer exist in the repo. Private repos via existing org integrations (token stored encrypted). See the [Policy GitOps guide](guides/policy-gitops.md).

### Forge UI ✓

Complete visual redesign shipped across four PRs in v0.8.1:

- **Teal-slate design system** — the full zinc scale is shifted to hue 185 in OKLCH, giving every surface a subtle teal undertone without requiring class changes. Accent CSS variables (`--accent: #2DD4BF`, `--accent-muted`, `--accent-border`) are available everywhere. `field-input` utility defined with a teal focus ring.
- **Icon sidebar** — plain text nav links replaced with Heroicons v2 SVG icon + label pairs, grouped into three sections (Core · Config · Ops). Active item shows a 2 px teal left-border bar and accent-muted fill.
- **RunLifecycle rail** — 5-step horizontal progress indicator (Queued → Planning → Review → Applying → Done) at the top of every run detail page. Pulsing dot on the active step, check icon on completed steps, red ✗ on failure, cancel, or discard.
- **Terminal log viewer** — run output panel restyled with a deep teal-black background and traffic-light chrome dots.
- **Toast notifications** — all 48 browser-native `alert()` popups replaced with a teal-accented toast store. Error / success / info variants auto-dismiss after 4.5 s and stack bottom-right. `aria-live="polite"` for screen readers.
- **Consistent empty states** — shared `EmptyState` component with a teal icon badge, heading, and subtext on all 10 list pages.

---

## Medium Term

Nothing currently planned — suggest a feature by opening a GitHub issue.

---

## Long Term / Speculative

### Multi-node / HA

- PostgreSQL connection pooling (PgBouncer)
- Stateless API — run multiple API instances behind a load balancer
- Remote Docker host support for runner containers (not just local socket)

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
