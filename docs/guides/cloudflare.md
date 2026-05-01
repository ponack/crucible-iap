# Cloudflare Infrastructure as Code with Crucible IAP

This guide covers how to bring existing Cloudflare resources under infrastructure-as-code control using [cf-terraforming](https://github.com/cloudflare/cf-terraforming), then hand the GitOps lifecycle to Crucible IAP. By the end you will have DNS records, WAF rulesets, Page Rules, and Access applications all version-controlled and applied through pull requests.

## Why manage Cloudflare as code?

DNS changes made in the Cloudflare dashboard leave no audit trail, cannot be reviewed before they go live, and are impossible to roll back cleanly. A CNAME that points a subdomain at the wrong origin, a misconfigured WAF ruleset that blocks legitimate traffic, or a Page Rule that silently redirects your API — these are hard to catch and slow to diagnose. Managing Cloudflare through IaC gives you:

- **Audit trail** — every change is a git commit with an author, timestamp, and reason
- **PR review** — teammates see exactly which DNS records change before they take effect
- **Rollback** — revert a commit; Crucible applies the diff back
- **Dependency ordering** — bring a new origin up in one stack, add the DNS record in a downstream stack once the origin is healthy
- **Drift detection** — Crucible plans run on a schedule and surface any change made outside of git

## Prerequisites

- A Cloudflare account with at least one zone
- [cf-terraforming](https://github.com/cloudflare/cf-terraforming) installed (see Step 2)
- OpenTofu ≥ 1.6 or Terraform ≥ 1.5 installed locally for the import phase
- A git repository to host the code
- Crucible IAP running — follow the [quickstart guide](../quickstart.md) if you haven't set it up yet

## Step 1 — Create Cloudflare API tokens

You need two tokens with different scopes. Keep them separate so that the read-only export token can never mutate your zone, and the write token used by Crucible never appears in your local shell history after the initial setup.

**Read-only token** — used by cf-terraforming to export existing resources. Required permissions:

- Zone → Zone: Read
- Zone → DNS: Read

**Write token** — used by Crucible to plan and apply changes. Scope it to the resources you intend to manage:

| Resource | Permission |
|---|---|
| Zone | Edit |
| DNS | Edit |
| Page Rules | Edit |
| Firewall Services | Edit |
| Workers Scripts | Edit (if managing Workers) |

Create both tokens at: **My Profile → API Tokens → Create Token → Create Custom Token**.

> **Note:** Cloudflare API tokens are zone-scoped by default. If you manage multiple zones, either create one token per zone (cleanest) or create a single token scoped to "All zones" in the account — but then protect that token with additional IP restrictions in the token settings.

> **Tip:** Pin each token to your office/VPN IP range using the "Client IP Address Filtering" option in the token editor. This limits blast radius if a token leaks.

## Step 2 — Export existing resources with cf-terraforming

### Install cf-terraforming

```bash
brew install cloudflare/cloudflare/cf-terraforming
# or
go install github.com/cloudflare/cf-terraforming/cmd/cf-terraforming@latest
```

Verify the install:

```bash
cf-terraforming version
```

### Set environment variables

Use the **read-only** token here. The zone ID and account ID are visible in the Cloudflare dashboard: select your zone, then look in the right-hand sidebar under "API".

```bash
export CLOUDFLARE_API_TOKEN="<your-read-only-token>"
export CLOUDFLARE_ZONE_ID="<your-zone-id>"
export CLOUDFLARE_ACCOUNT_ID="<your-account-id>"
```

### Generate resource HCL

Each `cf-terraforming generate` command writes HCL to stdout. Redirect each to a file. Run these for every resource type you want under management:

```bash
# DNS records
cf-terraforming generate --resource-type cloudflare_dns_record \
  --zone $CLOUDFLARE_ZONE_ID > dns.tf

# Page rules
cf-terraforming generate --resource-type cloudflare_page_rule \
  --zone $CLOUDFLARE_ZONE_ID > page_rules.tf

# Rulesets (WAF, rate limiting, transform rules)
cf-terraforming generate --resource-type cloudflare_ruleset \
  --zone $CLOUDFLARE_ZONE_ID > rulesets.tf

# Email routing rules (if used)
cf-terraforming generate --resource-type cloudflare_email_routing_rule \
  --zone $CLOUDFLARE_ZONE_ID > email_routing.tf

# Zero Trust Access Applications (account-level, not zone-level)
cf-terraforming generate --resource-type cloudflare_access_application \
  --account $CLOUDFLARE_ACCOUNT_ID > access.tf
```

> **Note:** Not all resource types support `cf-terraforming generate`. Run `cf-terraforming generate --help` to see the full list. For unsupported types, write the HCL by hand and use a targeted `terraform import` to bring them into state.

### Generate import blocks

OpenTofu 1.6+ and Terraform 1.5+ support native `import` blocks in HCL. cf-terraforming can generate these for you:

```bash
cf-terraforming import --resource-type cloudflare_dns_record \
  --zone $CLOUDFLARE_ZONE_ID >> dns.tf
```

Repeat for each resource type. The `>>` appends import blocks to the same file as the resource declarations — they sit alongside each other and are consumed on the next `terraform apply`.

> **Note:** Older versions of cf-terraforming generate `terraform import` shell commands instead of HCL import blocks. Both approaches work. If you get shell commands, run them directly after `terraform init -backend=false` in Step 5.

## Step 3 — Set up the provider and backend

Create `main.tf` at the root of your repository:

```hcl
terraform {
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.0"
    }
  }

  # Crucible's built-in HTTP backend — filled in by Crucible automatically.
  # Remove this block if using an external S3/GCS backend instead.
  backend "http" {}
}

provider "cloudflare" {
  # CLOUDFLARE_API_TOKEN is read from the environment.
  # Never hard-code credentials here.
}

variable "zone_id" {
  description = "Cloudflare zone ID — set as TF_VAR_zone_id in Crucible."
}

variable "account_id" {
  description = "Cloudflare account ID — set as TF_VAR_account_id in Crucible."
}
```

The `zone_id` and `account_id` variables keep hard-coded IDs out of every resource block. Resources reference `var.zone_id` instead of a literal string, which makes the code portable if you later clone the stack for a second zone.

## Step 4 — Clean up generated code

cf-terraforming includes computed fields that will cause plan diffs on every run. Clean them before committing:

- Remove `id`, `created_on`, `modified_on`, and `version_id` from all resource blocks — these are server-managed and cause perpetual diffs if present
- Replace every hard-coded zone ID string with `var.zone_id`
- Replace every hard-coded account ID string with `var.account_id`
- Delete any `data` source blocks for resources you are not managing
- Group resources by type into separate files: `dns.tf`, `security.tf`, `workers.tf`, and so on

**Before (generated output):**

```hcl
resource "cloudflare_dns_record" "example_com_a" {
  id      = "abc123"                          # remove — computed
  zone_id = "your-zone-id"                    # replace with var reference
  name    = "example.com"
  content = "192.0.2.1"
  type    = "A"
  ttl     = 1
  proxied = true
  created_on  = "2024-01-01T00:00:00Z"        # remove — computed
  modified_on = "2024-01-15T00:00:00Z"        # remove — computed
}
```

**After (clean):**

```hcl
resource "cloudflare_dns_record" "example_com_a" {
  zone_id = var.zone_id
  name    = "example.com"
  content = "192.0.2.1"
  type    = "A"
  ttl     = 1
  proxied = true
}
```

> **Tip:** For proxied records, always set `ttl = 1`. Cloudflare silently overrides any other TTL value to `1` (meaning "automatic") for proxied records, which creates a perpetual diff in your plans if your code specifies anything else.

## Step 5 — Run the initial import locally

Import state locally before handing off to Crucible. This seeds the state file and lets you verify the generated code produces no changes against the live zone.

**With native import blocks (OpenTofu 1.6+ / Terraform 1.5+):**

```bash
# Initialise with a local backend — no remote state yet
terraform init -backend=false

# Apply the import blocks; OpenTofu fetches current state from the API
terraform apply

# Verify — this must show no changes before you commit
terraform plan
```

**With legacy shell import commands:**

```bash
terraform init -backend=false

# Run each generated terraform import command, e.g.:
terraform import cloudflare_dns_record.example_com_a <zone_id>/<record_id>

terraform plan
```

The plan must show **No changes** before you move on. If it shows changes, the generated code still contains computed attributes or values that differ from what Cloudflare returns. Fix each diff before committing — you don't want Crucible's first run to make unintended mutations.

> **Warning:** Do not skip the local plan verification. Once state is uploaded to Crucible and the stack triggers its first run, any diff will be applied automatically if auto-apply is enabled.

## Step 6 — Commit to git and migrate state

Add a `.gitignore` at the repository root before committing:

```
.terraform/
*.tfstate
*.tfstate.backup
*.tfvars
```

Keep `*.tfvars` out of git — pass variable values through Crucible environment variables instead.

Then commit:

```bash
git add main.tf dns.tf page_rules.tf rulesets.tf .gitignore
git commit -m "chore: initial Cloudflare IaC import via cf-terraforming"
git push
```

## Step 7 — Create the Crucible stack

In Crucible, navigate to **Stacks → New stack** and fill in:

| Field | Value |
|---|---|
| Repository | URL of your git repository |
| Branch | `main` |
| Project root | `.` (or subdirectory if the IaC lives in a subfolder) |
| Tool | OpenTofu (or Terraform) |
| VCS provider | GitHub / GitLab / Gitea |

### Environment variables

Add these in **Stack → Settings → Environment Variables**:

| Name | Value | Secret? |
|---|---|---|
| `CLOUDFLARE_API_TOKEN` | Your **write** API token | Yes |
| `TF_VAR_zone_id` | Your Cloudflare zone ID | No |
| `TF_VAR_account_id` | Your Cloudflare account ID | No |

Mark `CLOUDFLARE_API_TOKEN` as **Secret** — the value will never appear in run logs, plan output, or the Crucible UI.

### Migrate existing state to Crucible

If you have a local `terraform.tfstate` from the import run, upload it to Crucible's state backend before triggering the first run. The state backend URL and bearer token are shown under **Stack → Settings → State backend**.

```bash
curl -X POST \
  -H "Authorization: Bearer <stack-token>" \
  -H "Content-Type: application/json" \
  -d @terraform.tfstate \
  "https://<your-crucible-host>/api/v1/state/<stack-id>"
```

After uploading, re-initialise with the HTTP backend to confirm Crucible can read the state:

```bash
terraform init   # reads backend "http" block from main.tf
terraform plan   # should show no changes
```

> **Note:** If you initialised locally with `-backend=false`, running `terraform init` without that flag will prompt you to migrate local state. Select "yes" only if you have not already uploaded via the curl command above — migrating twice will result in a 409 conflict.

## Step 8 — First run in Crucible

Trigger a plan from the Crucible UI (**Stack → Trigger plan**). The run should report **No changes** if state was uploaded and cleaned correctly.

If you see unexpected changes in this first plan, see the Troubleshooting section before enabling auto-apply.

Once the first clean plan passes:

- Enable **Auto-apply** for DNS records and Page Rules — a plan-to-apply cycle of under a minute is safe for most teams
- Keep **manual approval** for WAF rulesets and Zero Trust Access policies — a misconfigured rule can block all traffic to your zone

## Day-to-day workflow

All changes flow through git once the stack is running:

1. Create a branch: `git checkout -b dns/add-staging-cname`
2. Edit a resource in `dns.tf`
3. Open a PR — Crucible posts a plan comment showing exactly which records change, are created, or are deleted
4. Review and merge — Crucible applies automatically (or you click **Confirm** if auto-apply is off)

### Adding a new resource

For resources that do not exist in Cloudflare yet, add the HCL and push — Crucible plans and creates it.

For resources that already exist in the Cloudflare dashboard (someone created a record manually), generate an import block first:

```bash
cf-terraforming import --resource-type cloudflare_dns_record \
  --zone $CLOUDFLARE_ZONE_ID >> dns.tf
```

Add the corresponding resource HCL, commit both together, and push. Crucible runs the import block to bring the existing resource under management without recreating it.

### Recommended directory structure

```
cloudflare-iac/
├── main.tf              # provider, backend, variables
├── dns.tf               # cloudflare_dns_record resources
├── security.tf          # cloudflare_ruleset, firewall rules
├── page_rules.tf        # cloudflare_page_rule
├── workers.tf           # cloudflare_worker_script, routes
├── access.tf            # cloudflare_access_application, policies
└── email.tf             # cloudflare_email_routing_rule
```

## OPA policies for Cloudflare stacks

Attach OPA policies in Crucible to guard against accidental deletion of critical records. The policy below blocks any automated destroy of a root apex A or AAAA record:

```rego
package crucible

import future.keywords.if

# Deny destruction of root-level A or AAAA records.
deny[msg] if {
  resource := input.resource_changes[_]
  resource.type == "cloudflare_dns_record"
  resource.change.actions[_] == "delete"
  resource.change.before.name == resource.change.before.zone_id
  msg := sprintf("destroying root DNS record %q is not allowed via automation", [resource.address])
}
```

Attach the policy via **Stack → Policies → Attach policy**. Plans that violate the policy will be blocked and require an administrator to override.

For WAF rulesets, add a policy that requires a named approver before any ruleset is modified:

```rego
package crucible

import future.keywords.if

# Require approval for any WAF ruleset change.
deny[msg] if {
  resource := input.resource_changes[_]
  resource.type == "cloudflare_ruleset"
  resource.change.actions[_] != "no-op"
  not input.run.approved_by
  msg := sprintf("WAF ruleset change to %q requires manual approval before apply", [resource.address])
}
```

## Troubleshooting

### `cf-terraforming generate` returns empty output

The API token does not have the required permissions for that resource type, or the zone/account ID does not match the token's scope. Verify the token works and the zone is reachable:

```bash
curl -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones?name=<your-domain>"
```

A `403` response means insufficient permissions — check the token's permission list. A `200` with an empty `result` array means the zone ID does not fall within the token's scope. A `200` with results confirms the token and zone ID are correct; the issue is with the specific resource type's permissions.

### Plan shows unexpected replacements after import

Generated code often includes computed attributes that force resource replacement when present in configuration. Remove any field that appears as `# (known after apply)` in the plan output. Common culprits for Cloudflare resources: `id`, `created_on`, `modified_on`, `version_id`, `meta`. Remove them from the resource block and re-run the plan.

### `Error: Invalid API Token` in a Crucible run

The `CLOUDFLARE_API_TOKEN` environment variable is missing from the stack, or the token has been revoked. Open **Stack → Settings → Environment Variables** and confirm the variable is present — it will display as `***` if marked secret. Check that the name matches exactly, including case. If the token was recently rotated in the Cloudflare dashboard, update the secret value in Crucible to match.

### DNS record shows a perpetual diff on `ttl`

Cloudflare always returns `ttl = 1` for proxied records, regardless of what value was set. If your code specifies any other TTL and `proxied = true`, the provider will detect a diff on every plan. Set `ttl = 1` in all proxied record blocks:

```hcl
resource "cloudflare_dns_record" "www" {
  zone_id = var.zone_id
  name    = "www"
  content = "192.0.2.1"
  type    = "A"
  proxied = true
  ttl     = 1   # must be 1 for proxied records
}
```

### State upload returns 409 Conflict (lock)

Another process holds the state lock on the stack. Check the Crucible UI for an active or stuck run. If no run is active, an administrator can release the lock via **Stack → Settings → Force unlock state**. Do not force-unlock while a run is genuinely in progress — this will corrupt the state.

### `cloudflare_ruleset` generates invalid HCL for complex rule expressions

cf-terraforming sometimes produces malformed string escaping for complex WAF rule expressions containing backslashes or nested quotes. If `terraform validate` reports an error on a generated ruleset file, open the file and manually correct the expression string, then test with `terraform plan` before committing.

### Resources in the plan that you did not change

If Crucible's plan shows changes to resources you did not touch, the Cloudflare API likely returned a slightly different representation of an existing value (a normalised IP, a canonicalised hostname, a reordered JSON object). Run `terraform plan` locally to inspect the diff in detail:

```bash
terraform plan -out=review.tfplan
terraform show review.tfplan
```

For each unexpected diff, update your code to match what Cloudflare returns — not what you wrote. This is the source of truth for computed normalisation behaviour.
