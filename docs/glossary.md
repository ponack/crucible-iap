# Glossary

Quick reference for terms used throughout Crucible IAP and its tool ecosystem. If a term is unclear in another doc, it's probably here.

---

## Crucible terms

**Stack** — A single unit of infrastructure managed by Crucible. One stack = one repository (or subdirectory) + one tool (OpenTofu, Terraform, Terragrunt, Pulumi, or Ansible) + one state. Most teams end up with many stacks (one per environment, one per cloud account, one per logical service).

**Run** — One execution of `plan` (and optionally `apply`) against a stack. Every run has a status, a plan artifact, logs, and an audit record.

**Run types:**

- **Tracked run** — Full lifecycle: plan → confirm → apply. Updates state. This is the production change path.
- **Proposed run** — Plan-only. Used for PR previews and "what would happen if I changed this?" exploration. Never modifies state.
- **Destroy run** — Plans `terraform destroy` and requires explicit confirmation to apply.
- **Drift run** — Scheduled plan to detect changes made outside Crucible.

**Plan** — The list of changes Terraform/OpenTofu/etc. would make if applied. Shown as `add / change / destroy` counts. Stored as an artifact you can re-view forever.

**Apply** — Executing a plan. Resources are actually created or modified at this point.

**Confirm** — The button you click after reviewing a plan in a tracked run, before apply happens. Without confirmation, the run sits in `unconfirmed` state forever.

**Approval (or approval gate)** — Some runs require approval from a designated person before they can even be confirmed. Triggered by policies (e.g. "all production changes need an approver from `@platform-admins`").

**Drift** — When the real infrastructure differs from what the state file says should exist. Caused by manual cloud-console edits, deleted resources, or other tools touching the same resources.

**Stack lock** — A stack can be locked to prevent runs (e.g. during a maintenance window or after a known-bad commit). Locked stacks reject all triggers until unlocked.

**Stack tag** — Color-coded label attached to stacks for filtering and grouping (e.g. `env:prod`, `team:platform`). Purely organizational.

**Project** — Hierarchical grouping above stacks. An org can have many projects; each project has its own member list with admin/member/viewer roles. Use projects when you want different teams to own different sets of stacks within one Crucible deployment.

**Organization (org)** — Top-level tenant. One Crucible instance can host many orgs (multi-tenant). Each org has its own users, stacks, policies, and audit log.

**Instance admin** — A user with `is_instance_admin=true` who can create, archive, and manage all organizations on the deployment. Used by MSPs and shared-platform operators.

**Worker / runner** — The ephemeral Docker container that actually executes `tofu plan` and `tofu apply`. Spawned per run, destroyed afterwards. Has read-only root, dropped capabilities, and a scoped JWT.

**Worker pool** — An external host running the `crucible-agent` binary that pulls runs from Crucible and executes them on its own machine. Use for: isolated production runs, accessing private networks, scaling beyond the Crucible host.

**Blueprint** — A parameterized stack template. Platform teams publish blueprints; app teams fill in a form (region, instance size, etc.) and Crucible creates a configured stack from it.

**Stack template** — A saved stack configuration (tool, repo, branch, hooks, env vars) used as a starting point when creating new stacks. Simpler than a blueprint; no parameter form.

**Variable set** — A named bundle of environment variables that can be attached to multiple stacks. Update once, applies everywhere.

**Run hook** — Bash script that runs at one of four points in the run lifecycle: pre-plan, post-plan, pre-apply, post-apply. Useful for notifications, custom validation, or pre-flight checks.

**Policy** — An OPA/Rego rule that evaluates a plan (or other input) and returns `deny` or `warn` messages. Policies can block applies (`deny`) or just surface warnings (`warn`).

**Policy hooks:** `pre_plan`, `post_plan`, `pre_apply`, `trigger`, `login`, `approval`, `validation`. Each hook runs at a different stage and receives different input data. See [`policies.md`](policies.md).

**Compliance pack** — A pre-built set of OPA policies for a regulatory framework (SOC 2, CIS AWS, HIPAA, PCI-DSS). Installed with one click from `ponack/crucible-policies` and attachable to any stack.

**Continuous validation** — Periodic policy evaluation against the *live state* of a stack, on a schedule. Detects drift from your compliance baseline without waiting for the next push.

**ChatOps approval** — Slack/Teams/Discord notification with HMAC-signed buttons that confirm, discard, or approve a run directly from the chat message. No bot token required.

**SIEM streaming** — Real-time forwarding of audit events to Splunk, Datadog, Elasticsearch, GCP SecOps/Chronicle, Wazuh, Graylog, or a generic webhook.

**Cloud OIDC federation** — Crucible acts as an OIDC identity provider; cloud accounts exchange short-lived Crucible-issued tokens for cloud credentials. No static cloud keys are stored anywhere.

**BYOK (Bring Your Own Key)** — Encrypt Crucible's internal vault using a key from *your* KMS (AWS KMS, HashiCorp Vault Transit, Azure Key Vault). The KMS key never leaves your KMS.

---

## Terraform / OpenTofu terms

**Provider** — Plugin that talks to a specific platform's API. `hashicorp/aws`, `cloudflare/cloudflare`, `proxmox/bpg`, etc. Declared in `required_providers`.

**Resource** — A managed thing (`aws_s3_bucket`, `cloudflare_record`). Created, updated, destroyed by its provider.

**Data source** — Read-only lookup (`data "aws_vpc" "main"`). Use to reference existing resources without managing them.

**Variable** — Input. Pass values via `.tfvars`, `-var`, or env vars (`TF_VAR_name=...`).

**Output** — Value exported from your module. Other stacks can reference outputs via the state backend.

**Module** — Reusable bundle of resources, called like a function. Sourced from a local path, the Terraform registry, a private registry, or a Git URL.

**State** — Terraform's memory of what it has created. Stored in a backend (local file, S3, GCS, Azure Blob, Terraform Cloud, or Crucible's built-in HTTP backend).

**Backend** — Where state is stored. Crucible's HTTP backend is the default; per-stack overrides allow S3 / GCS / Azure Blob if you need it.

**Workspace** — Multiple state files for the same code (e.g. `dev` and `prod` workspaces). Crucible uses one workspace per stack; manage environments with separate stacks instead.

**Plan output** — The text or JSON description of what `apply` would do. Crucible stores this as an artifact and uses the JSON form for policy evaluation and Infracost.

**Drift** — Difference between real infrastructure and what state says exists. Detected by re-running `plan` with no code changes; any non-empty plan is drift.

**Provider cache** — Terraform downloads provider binaries on every `init`. Crucible caches them in MinIO so repeat runs skip the download.

---

## Terragrunt terms

**Root `terragrunt.hcl`** — Top-level configuration shared by all child modules (`remote_state`, common inputs, etc.).

**Child module** — A directory containing its own `terragrunt.hcl` that `include`s the root and points at a Terraform module.

**`run-all`** — Terragrunt command that walks the module tree, plans or applies each module in dependency order. Crucible uses `run-all` for all Terragrunt runs.

**`remote_state` block** — Tells Terragrunt where to put state. Crucible injects `TF_HTTP_*` env vars so this can read from Crucible's built-in backend with no hard-coding.

**`dependency` block** — Declares that module A needs outputs from module B. `run-all` respects this graph.

**`mock_outputs`** — Fake dependency outputs used during `plan` before the dependency has ever been applied. Necessary for first-run planning.

---

## Pulumi terms

**Pulumi stack** — A named instance of a Pulumi program (similar concept to a Crucible stack, but the names collide). Crucible refers to its own stacks; "Pulumi stack" refers to the Pulumi-internal concept.

**`pulumi preview`** — Pulumi's equivalent of `terraform plan`.

**`pulumi up`** — Pulumi's equivalent of `terraform apply`.

**`PULUMI_CONFIG_PASSPHRASE`** — Required env var; Pulumi uses it to encrypt stack config and state. Set as a secret env var on the Crucible stack.

---

## Ansible terms

**Playbook** — YAML file describing tasks to run on a set of hosts.

**Inventory** — List of hosts to run against.

**Check mode (`--check`)** — Dry-run that reports what *would* change. Crucible uses this as its "plan" phase.

**Play recap** — Summary at the end of an Ansible run (`ok=N changed=N failed=N`). Crucible parses this into the add/change/destroy counts.

---

## OPA / Rego terms

**OPA (Open Policy Agent)** — General-purpose policy engine. Crucible embeds OPA to evaluate policies on plans.

**Rego** — The policy language used by OPA. Looks like a constraint language; declarative.

**Policy hook** — *Where* in the run lifecycle a policy fires. See "Policy hooks" above under Crucible terms.

**`deny`** — Set of error messages that block a run from proceeding. Any non-empty `deny` set fails the policy.

**`warn`** — Set of non-blocking warning messages. Shown to the user but doesn't stop the run.

**Policy bundle** — A `.tar.gz` of one or more `.rego` files used by OPA in distribution mode. Crucible doesn't use bundles; it stores each policy individually in the database.

**Policy GitOps** — Storing `.rego` files in a Git repo and having Crucible sync them on push. See [`guides/policy-gitops.md`](guides/policy-gitops.md).

---

## VCS terms

**VCS (Version Control System)** — Git host. Crucible supports GitHub, GitLab, Gitea, Gogs, Bitbucket Cloud, and Azure DevOps as first-class providers.

**Webhook** — HTTP POST from the VCS to Crucible whenever a relevant event happens (push, PR open, PR close). Each Crucible stack has a unique webhook URL.

**Webhook secret** — Shared secret used to sign webhook payloads. Crucible verifies the signature before processing any event.

**GitHub App** — Alternative to webhooks for GitHub. Cleaner permission model; per-installation. Crucible supports both.

**PR comment** — Auto-posted comment on a pull request with the plan summary, link to the run, and policy violations.

---

## Cloud terms

**Workload Identity Federation (WIF)** — Mechanism where one platform's tokens are accepted by another platform without static credentials. AWS, GCP, Azure all support it. Crucible mints OIDC tokens that your cloud account accepts.

**KMS (Key Management Service)** — Cloud-provider service for managing encryption keys. AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault Transit.

**Service account** — Cloud identity for non-human callers. Crucible runs as a service account (or assumes a role) when calling cloud APIs.
