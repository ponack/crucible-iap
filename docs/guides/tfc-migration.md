# Migrating from Terraform Cloud to Crucible IAP

This guide covers the full migration path from HashiCorp's Terraform Cloud (TFC) or Terraform Enterprise (TFE) to Crucible IAP. The migration is non-destructive: your IaC code and Terraform state do not move locations within Terraform — what changes is the orchestrator. You can run both systems in parallel during validation, then cut over by removing the TFC webhook.

If you're migrating from Spacelift instead, see [`spacelift-migration.md`](spacelift-migration.md) — the patterns are analogous.

---

## Concept mapping

| Terraform Cloud | Crucible | Notes |
| --- | --- | --- |
| Workspace | Stack | The atomic unit of `plan`/`apply` |
| Organization | Organization | Both use the same name |
| Project (TFC) | [Project](projects.md) | Hierarchical grouping with per-project RBAC |
| Workspace variables | Stack environment variables | Both encrypt secrets at rest |
| Variable sets | [Variable sets](variable-sets.md) | Crucible has the same concept and name |
| Sentinel / OPA policies | OPA policies | Crucible uses OPA/Rego — Sentinel needs translation; OPA policies are a direct lift |
| Run triggers (workspace → workspace) | [Stack dependencies](stack-dependencies.md) | Same model: upstream apply triggers downstream |
| Notification configurations | Per-stack notifications | Slack/Teams/Discord/email/webhook |
| Agent pools | [Worker pools](../operator-guide.md#external-worker-agents) | Same concept; Crucible agents are a binary you run on your own host |
| Private module registry | Module registry | Crucible has a built-in private registry |
| Cost estimation | Infracost integration | Crucible uses the Infracost OSS engine |
| Drift detection (health assessments) | Drift detection | Both support cron-scheduled drift runs |
| Audit log | Audit log | Crucible's is tamper-resistant at the DB level (`RULE` blocks UPDATE/DELETE) |
| API tokens | API tokens | Settings → API tokens |

---

## What stays the same

- Your `.tf` files — no changes required.
- Terraform state contents — you migrate the state file, not the code.
- Cloud credentials — same values, stored in Crucible's encrypted vault instead of TFC's variable store.
- Git repository — same repo, same branch.

## What changes

- The state **backend location** moves from `terraform { cloud { ... } }` to `terraform { backend "http" {} }` (or an external backend of your choice — S3, GCS, Azure Blob).
- VCS webhook on the git repository (TFC URL → Crucible URL).
- Variable storage (TFC workspace vars → Crucible stack env vars or variable sets).
- Policy engine: if you used Sentinel, you'll rewrite in Rego. If you used HCP Terraform's native OPA, the Rego body lifts directly.

---

## Prerequisites

- Crucible IAP running (see [quickstart.md](../quickstart.md)).
- A TFC/TFE user token with `read` on the workspace you're migrating.
- Admin access to the git repository to update the webhook (later in the process).
- The OpenTofu or Terraform CLI installed locally for state migration.

---

## Step 1 — Document the TFC workspace

Before changing anything, record the full config of the workspace you're migrating. The TFC UI shows this on **Settings → General** plus the **Variables** tab. For a scripted export:

```bash
curl -s \
  -H "Authorization: Bearer $TFC_TOKEN" \
  -H "Content-Type: application/vnd.api+json" \
  "https://app.terraform.io/api/v2/organizations/$ORG/workspaces/$WORKSPACE_NAME" \
  | jq '.data.attributes'
```

Record:

- Repository URL, branch, working directory.
- Terraform version (or OpenTofu version if you're using HCP Terraform's OpenTofu support).
- All variables: name, value, sensitive flag, env-var or HCL category.
- Attached variable sets.
- Run triggers (which workspaces trigger this one, and which workspaces this one triggers).
- Notification configurations.
- Sentinel or OPA policy sets attached to the workspace.
- Auto-apply setting.

---

## Step 2 — Export the state file

This is the load-bearing step. Pull the current state from TFC to a local file:

```bash
mkdir -p ~/tfc-migration && cd ~/tfc-migration

# Authenticate the Terraform CLI to TFC
terraform login

# Initialise against the TFC backend and pull state
cat > providers.tf <<EOF
terraform {
  cloud {
    organization = "$ORG"
    workspaces { name = "$WORKSPACE_NAME" }
  }
}
EOF

terraform init
terraform state pull > terraform.tfstate

# Verify it's valid
jq '.serial, .terraform_version, (.resources | length)' terraform.tfstate
```

A `serial` number, a `terraform_version`, and a non-zero resource count means the export worked.

**Keep this file safe and do not delete the TFC workspace yet** — it's your fallback if migration goes sideways.

---

## Step 3 — Create the Crucible stack (do NOT apply yet)

In Crucible: **Stacks → New stack**.

| Field | Value from TFC |
| --- | --- |
| Name | TFC workspace name, or any new name |
| Tool | `opentofu` (or `terraform` if you used Terraform-tool-specific features) |
| Tool version | Match the TFC workspace's Terraform version |
| Repository URL | Same as TFC |
| Branch | Same as TFC |
| Working directory | Same as TFC's "Terraform Working Directory" |
| Auto-apply | Off for the initial cut-over; enable later if desired |

**Do not configure the webhook yet.** Keep TFC as the active orchestrator until validation succeeds.

---

## Step 4 — Decide on the state backend

Two paths:

### Path A — Use Crucible's built-in HTTP backend (recommended for most teams)

Update `providers.tf` (or wherever your `terraform` block lives) to remove the `cloud` block and add `backend "http" {}`:

```hcl
# Before
terraform {
  cloud {
    organization = "acme"
    workspaces { name = "prod-network" }
  }
}

# After
terraform {
  backend "http" {}
}
```

Crucible injects `TF_HTTP_ADDRESS`, `TF_HTTP_USERNAME`, and `TF_HTTP_PASSWORD` at run time. **Do not** hardcode the address — let Crucible inject it.

Commit this change to a branch but **do not merge** until cut-over (Step 7). For local testing, you can keep the change in a working branch.

Then upload the exported state to Crucible's backend:

```bash
# Retrieve the stack token from: Stack → Settings → State backend → "Stack token"
STACK_ID="<from the stack URL>"
STACK_TOKEN="<from the UI>"
CRUCIBLE_URL="https://crucible.example.com"

curl -sf -X POST \
  -u "$STACK_ID:$STACK_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @terraform.tfstate \
  "$CRUCIBLE_URL/api/v1/state/$STACK_ID"
```

Verify by viewing the stack → **State** tab — you should see the resources.

### Path B — Use an external backend (S3 / GCS / Azure Blob)

If you prefer state on your own object storage (e.g. compliance requires it on your account, not on the Crucible host), update the `terraform` block to your chosen backend and run `terraform init -migrate-state` locally to push the state there:

```hcl
terraform {
  backend "s3" {
    bucket = "acme-tfstate"
    key    = "prod-network/terraform.tfstate"
    region = "eu-west-1"
  }
}
```

```bash
# In the local working copy with the new backend block
terraform init -migrate-state
# Confirm when prompted; existing state pulled from TFC is pushed to S3.
```

Add the S3 credentials (or OIDC role config) as env vars on the Crucible stack. Crucible reads the standard `AWS_*` env vars; the OpenTofu provider connects to S3 directly during `init`.

---

## Step 5 — Migrate variables

For each TFC variable, create the equivalent in Crucible:

**Env-var-category TFC variables** → Stack env var with the same name. Mark **Secret** if the TFC variable was marked sensitive.

**HCL-category TFC variables** → Stack env var with the `TF_VAR_` prefix:

| TFC variable | Crucible env var |
| --- | --- |
| HCL `region = "eu-west-1"` | `TF_VAR_region = eu-west-1` |
| HCL `instance_count = 3` | `TF_VAR_instance_count = 3` |

OpenTofu/Terraform reads `TF_VAR_*` env vars and binds them to declared `variable` blocks automatically.

**Variable sets** → recreate each TFC variable set as a Crucible [variable set](variable-sets.md), then attach to the stack.

> **Sensitive value loss:** TFC never exposes sensitive variable values through its API. You'll need to re-supply them from your own records (password manager, vault). This is normal — every TFC migration involves re-entering sensitive values.

---

## Step 6 — Migrate policies

### Path A — You used HCP Terraform's native OPA policy sets

Crucible runs the same OPA engine. The Rego body lifts directly. Differences:

- **Input shape:** TFC's OPA `data.plan` becomes Crucible's `input` directly. Replace `data.plan.resource_changes` with `input.plan.resource_changes`.
- **Policy attachment:** in Crucible, attach each policy to a stack via **Stack → Policies → Attach**. There's no "policy set" multi-attach UI — attach to each stack individually, or use [policy GitOps](policy-gitops.md) to sync from a Git repo automatically.

### Path B — You used Sentinel

Sentinel and Rego are different languages with similar semantics. You'll rewrite each policy. For each Sentinel policy, identify whether it's an advisory (`soft-mandatory`/`hard-mandatory`) and translate to OPA `warn`/`deny`. The Crucible [policies guide](../policies.md) and [policy templates](../policies/README.md) cover common patterns.

Common Sentinel → Rego translations:

| Sentinel pattern | Rego equivalent |
| --- | --- |
| `import "tfplan/v2" as tfplan` | `input.plan.resource_changes[_]` |
| `tfplan.resource_changes[_]` | `input.plan.resource_changes[_]` |
| `if-then-else` rules | `deny[msg] { ... }` blocks |
| `print(...)` | `msg := sprintf(...)` |

For a complex policy set, plan for one Rego policy per Sentinel policy and translate incrementally.

---

## Step 7 — Validate with a manual run

Trigger a plan manually in Crucible: **Stack detail → Trigger proposed run**.

A clean plan should show **No changes**. This confirms:

- State migrated correctly (Crucible reads the same state TFC was using).
- All required variables are present.
- The provider versions resolve identically.

If the plan shows unexpected changes:

| Symptom | Likely cause |
| --- | --- |
| Resources appear as "to be created" | State didn't migrate or wrong stack pointed at wrong state |
| Resources appear as "to be replaced" | Provider version differs from what TFC last ran; pin the version |
| One resource shows a diff in tags / metadata | TFC injected workspace/run tags that local plans don't add — add them to your code |
| Lots of "no-op" reads but no resource changes | Just the plan refresh; this is fine |

> **Tip:** Run the plan twice. First run forces provider initialisation. If the second is clean, you're ready for cut-over.

---

## Step 8 — Cut over

Execute these steps in order to minimise the double-trigger window:

1. **Disable the TFC workspace's VCS connection.** Settings → Version Control → Disconnect. The workspace remains, but VCS pushes no longer trigger runs in TFC.

2. **Lock the TFC workspace** to prevent any manual runs. Settings → General → Lock workspace.

3. **Merge your branch** with the `backend "http" {}` change (or external backend change).

4. **Connect the Crucible webhook.** Stack detail → copy webhook URL and secret. Add to the repo's webhook settings (GitHub: Settings → Webhooks → Add webhook). See [webhooks.md](webhooks.md) for per-platform details.

5. **Push a test commit** — a comment or whitespace edit. Verify Crucible triggers a plan and it's clean.

6. **Remove the TFC webhook entry** from the repo (TFC's old webhook URL).

---

## Step 9 — Clean up TFC

After running in Crucible for a week with no issues:

1. **Export the TFC audit log** if you need to retain history (TFC's audit log isn't portable once the workspace is deleted).
2. **Delete the workspace** (TFC → Workspace → Settings → Destruction and Deletion → Delete from Terraform Cloud).
3. **Remove TFC-specific variables** and revoke TFC user tokens that were specific to this workspace.
4. **Update runbooks** to point at Crucible URLs and policy locations.

---

## Worked example — single-workspace migration

Migrating workspace `prod-network` from TFC org `acme`:

```bash
# Step 1 — record config
curl -s -H "Authorization: Bearer $TFC_TOKEN" \
  "https://app.terraform.io/api/v2/organizations/acme/workspaces/prod-network" \
  | jq '.data.attributes' > workspace-config.json

# Step 2 — pull state
terraform login
cat > providers.tf <<EOF
terraform {
  cloud {
    organization = "acme"
    workspaces { name = "prod-network" }
  }
}
EOF
terraform init
terraform state pull > terraform.tfstate

# Step 3-4 — create stack in Crucible UI, upload state
STACK_ID="..."   # from Crucible UI after creating stack
STACK_TOKEN="..." # from Stack → Settings → State backend
curl -sf -X POST \
  -u "$STACK_ID:$STACK_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @terraform.tfstate \
  "https://crucible.example.com/api/v1/state/$STACK_ID"

# Step 5 — migrate env vars (manual UI work)

# Step 6 — translate any policies (manual)

# Step 7 — trigger a proposed run in Crucible UI; verify "No changes"

# Step 8 — cut over (manual UI work + webhook reconfiguration)
```

The whole migration usually takes 30–60 minutes per workspace once you've done one.

---

## Multi-workspace migrations

For an org with dozens of workspaces, you have two options:

### Sequential

Migrate one at a time. Recommended — you build confidence and patch the process as you discover edge cases. Each workspace's state and config is independent.

### Big-bang

If you have strong CI/CD discipline and identical workspaces across environments, you can script the whole thing. The bottleneck is usually re-supplying sensitive variables (no API path); that has to be manual unless you've already stored them in an external secret store ([external-secrets.md](external-secrets.md)).

---

## Troubleshooting

### "State migration succeeded but plan shows everything to-be-created"

The Crucible stack is reading from a different state file than the one you uploaded. Confirm:

- The `STACK_ID` in your upload URL matches the stack you triggered the plan against.
- The stack's tool matches the version that produced the state (older state → newer Terraform usually works; newer state → older Terraform fails).

### "Plan shows persistent diff on Terraform meta-tags"

TFC silently injects workspace-name and workspace-ID tags onto some resources. Local plans don't add these. Either:

- Add the tags explicitly in your code so they're persisted intentionally, or
- Use `lifecycle { ignore_changes = [tags["..."]] }` for the tags TFC was injecting.

### "Policy denied my plan after migration"

You translated a Sentinel `hard-mandatory` to OPA `deny` — that's correct, but the plan that was always allowed in TFC is now blocked. Either fix the underlying issue in your code, soften the policy to `warn`, or temporarily detach the policy during the migration window.

### "OIDC federation isn't working with cloud providers"

TFC's dynamic credentials (workload identity) don't carry over — Crucible has its own OIDC issuer at `<CRUCIBLE_BASE_URL>`. Reconfigure your cloud's trust policy to accept Crucible's issuer. See [`operator-guide.md#cloud-oidc-workload-identity-federation`](../operator-guide.md#cloud-oidc-workload-identity-federation).

---

## What's next

- [Operator guide](../operator-guide.md) — production-grade Crucible deployment.
- [Policies](../policies.md) — write the Rego policies you'll attach to your migrated stacks.
- [Stack dependencies](stack-dependencies.md) — replace TFC run triggers.
- [Worker pools](../operator-guide.md#external-worker-agents) — equivalent of TFC agent pools.
