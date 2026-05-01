# Migrating from Spacelift to Crucible IAP

This guide covers the full migration path for moving a Spacelift stack to Crucible IAP. The migration is non-destructive: your IaC code and Terraform/OpenTofu state do not move. What changes is which system drives the GitOps lifecycle — webhooks, runs, approvals, and policy evaluation. You can run both systems in parallel against the same repository during validation, then cut over by disabling the Spacelift stack.

A specific worked example for a Cloudflare DNS/WAF stack appears at the end of this guide.

## Concept mapping

| Spacelift | Crucible | Notes |
| --------- | -------- | ----- |
| Stack | Stack | Same concept |
| Space | Organization | Top-level isolation boundary |
| Worker pool | Worker pool | External agents; default uses the bundled Crucible runner |
| Environment variable | Environment variable | Both encrypt at rest |
| Context | Variable set | Reusable env var collections attached to stacks |
| Rego policy | OPA policy | Crucible uses the same OPA/Rego engine |
| Push policy | (auto-trigger on push) | Crucible triggers from VCS webhook — no policy needed |
| Trigger policy | Stack dependency | Downstream stacks trigger automatically after apply |
| Notification policy | Stack notification | Per-stack Slack/Gotify/ntfy/email config |
| Blueprint | Blueprint | Same concept, slightly different model |
| Drift detection | Drift detection | Both support cron-based drift runs |
| Stack lock | Stack lock | Both support locking with a reason |
| Module registry | Module registry | Crucible has a built-in private registry |
| Audit trail | Audit log | Crucible's audit log is tamper-resistant at the database level |

## What stays the same

- Your IaC code — no changes required
- Terraform/OpenTofu state — stays where it is (S3, GCS, or Spacelift-managed; see Step 2)
- Cloud credentials — same values, different destination
- Git repository — same repo, same branch, same project root

## What changes

- Webhook URL on your git repository (Spacelift URL → Crucible URL)
- Where env vars live (Spacelift context → Crucible variable set or stack-level env vars)
- OPA policies — same Rego body, different attachment point and hook name (see Step 5)
- Notification destinations — reconfigure in Crucible per-stack

## Prerequisites

- Crucible IAP running and accessible (see the [quickstart guide](../../README.md))
- Full view of the Spacelift stack config — env vars, contexts, policies, and settings
- Admin access to the git repository to update the webhook
- The Terraform state backend URL and access credentials (if Spacelift-managed, see Step 2)

## Step 1 — Document the Spacelift stack

Before touching anything, record the complete config of the Spacelift stack. Use the Spacelift UI or query the GraphQL API:

```bash
# Export stack metadata via Spacelift GraphQL API
curl -s -X POST "https://<your-org>.app.spacelift.io/graphql" \
  -H "Authorization: Bearer $SPACELIFT_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ stack(id:\"<stack-id>\") { name branch projectRoot repository terraformVersion } }"
  }' \
  | jq .
```

Record the following before proceeding:

- Repository URL and branch
- Project root (subdirectory within the repo, or empty for repo root)
- Tool — Terraform version or OpenTofu version
- All environment variables and whether each is marked secret
- Attached contexts (which variable collections are in use)
- Attached policies (Rego body and hook type for each)
- Drift detection schedule, if configured
- Worker pool assignment, if not the default Spacelift runner
- Auto-deploy setting

> **Tip:** Export your Spacelift audit log before migrating if you need to retain a historical record. Spacelift's audit log is not portable — once the account or stack is deleted, the history is gone.

## Step 2 — Handle Terraform state

Spacelift manages state in one of two ways. Identify which applies to your stack before creating anything in Crucible.

### Option A — Spacelift-managed state (default)

Spacelift stores state in its own S3 bucket, exposed via an HTTP backend. You need to export it and re-upload it to Crucible's HTTP backend.

**Export the current state from Spacelift:**

```bash
curl -s -X GET \
  "https://<your-org>.app.spacelift.io/api/v1/stacks/<stack-id>/state" \
  -H "Authorization: Bearer $SPACELIFT_API_TOKEN" \
  -o terraform.tfstate

# Verify the export is valid JSON before proceeding
jq '.serial' terraform.tfstate
```

**Upload to Crucible** after creating the stack in Step 3. Retrieve the stack token from Stack → Settings → State backend in the Crucible UI.

```bash
curl -s -X POST \
  -H "Authorization: Bearer <crucible-stack-token>" \
  -H "Content-Type: application/json" \
  -d @terraform.tfstate \
  "https://<your-crucible-host>/api/v1/state/<stack-id>"
```

**Update your `backend.tf`** to use Crucible's HTTP backend:

```hcl
terraform {
  backend "http" {}
}
```

Commit this change to the repository. Do not apply it via Spacelift — Crucible initialises against the HTTP backend directly on first run.

> **Note:** Crucible injects `TF_HTTP_ADDRESS`, `TF_HTTP_USERNAME`, and `TF_HTTP_PASSWORD` automatically at run time. No credentials belong in `backend.tf`.

### Option B — External state backend (S3, GCS, Azure Blob)

If your stack already uses an external backend, no state migration is needed. Crucible connects to the same backend using the same credentials — add them as env vars on the Crucible stack.

For an S3 backend:

| Variable | Value | Secret? |
| -------- | ----- | ------- |
| `AWS_ACCESS_KEY_ID` | S3 access key | ✓ Yes |
| `AWS_SECRET_ACCESS_KEY` | S3 secret key | ✓ Yes |
| `AWS_REGION` | S3 bucket region | No |

> **Tip:** Prefer OIDC federation over static S3 credentials. See the [AWS guide](aws.md) for setup instructions. OIDC eliminates long-lived access keys entirely.

## Step 3 — Create the Crucible stack

In Crucible → Stacks → New stack, fill in the same values you recorded in Step 1:

| Field | Source |
| ----- | ------ |
| Name | Spacelift stack name, or a new name of your choosing |
| Repository URL | Same repository |
| Branch | Same branch |
| Project root | Same project root (leave empty for repo root) |
| Tool | Same tool and version |
| VCS provider | GitHub / GitLab / Gitea — match your repository host |
| Auto-apply | Match Spacelift's `autodeploy` setting |

> **Note:** Do not enable the VCS webhook in Crucible yet. Keep it disabled until you are ready to cut over in Step 7. You can trigger runs manually in the meantime to validate without risking double-triggering.

## Step 4 — Migrate environment variables

### Stack-level variables

Add each variable in Crucible → Stack → Settings → Environment Variables. Mark it Secret if Spacelift had it marked secret. Secret values are write-only — they never appear in run logs or the UI after saving.

For a Cloudflare stack the typical set is:

| Name | Secret? | Notes |
| ---- | ------- | ----- |
| `CLOUDFLARE_API_TOKEN` | ✓ Yes | Cloudflare write-scoped API token |
| `TF_VAR_zone_id` | No | Cloudflare zone ID |
| `TF_VAR_account_id` | No | Cloudflare account ID |

### Context variables → variable sets

For each Spacelift context attached to the stack, create a matching Variable Set in Crucible:

1. Go to Variable Sets → New variable set
2. Enter the same variables as the Spacelift context
3. Attach it to the stack via Stack → Variable Sets → Attach

> **Warning:** Spacelift contexts may inject variables without the `TF_VAR_` prefix — for example a variable named `zone_id` rather than `TF_VAR_zone_id`. In Crucible, variable sets inject variables as-is. If your OpenTofu/Terraform code reads `var.zone_id`, the variable set must contain `TF_VAR_zone_id`. Check the prefix for every variable when creating the Crucible variable set.

## Step 5 — Migrate OPA policies

Spacelift's Rego policies translate directly to Crucible — both use the OPA evaluation engine. Copy the Rego body verbatim. The differences are hook names and the absence of push/trigger policies (Crucible handles those via webhooks and stack dependencies):

| Spacelift hook | Crucible hook | Action |
| -------------- | ------------- | ------ |
| `plan` | `post_plan` | Paste the Rego body, change hook to `post_plan` |
| `push` | not needed | Crucible triggers automatically from the VCS webhook |
| `trigger` | stack dependency | Replace with a dependency relationship (Stack → Dependencies) |
| `login` | `login` | Paste the Rego body unchanged |
| `approval` | `approval` | Paste the Rego body unchanged |

In Crucible → Policies → New policy, paste the Rego body and set the type to the matching hook. Attach it to the stack via Stack → Policies → Attach policy.

> **Note:** Spacelift's `plan` hook receives a plan JSON document at `input.spacelift`. Crucible's `post_plan` hook uses `input.crucible`. If your Rego references `input.spacelift.*`, update those references to `input.crucible.*`. The plan JSON structure itself (resource changes, outputs, variables) is standard Terraform plan format and does not change.

## Step 6 — Validate with a manual run

Trigger a plan manually in Crucible: Stack → New run → Plan. At this point Spacelift still holds the active webhook — Crucible runs are triggered only when you initiate them manually.

A clean plan shows **No changes**. This confirms:

- State is in sync and correctly uploaded
- All required env vars are present
- Provider version constraints resolve to the same provider version

If the plan shows unexpected changes, see the Troubleshooting section before proceeding.

> **Tip:** Run the plan twice. The first run forces provider initialisation. If the second plan is clean, the migration is ready for cut-over.

## Step 7 — Cut over

Once Crucible plans are consistently clean, execute the cut-over in this order to keep the window of double-triggering as short as possible:

1. **Disable the Spacelift stack.** Set the stack to disabled in Spacelift so it no longer processes webhook events or scheduled runs. Do not delete the stack yet — you may need to reference it during validation.

2. **Enable the VCS webhook in Crucible.** Go to Stack → Settings → Webhook, copy the webhook URL and secret.

3. **Add the webhook to your git repository.**

   For GitHub: Settings → Webhooks → Add webhook
   - Payload URL: the Crucible webhook URL
   - Content type: `application/json`
   - Secret: the Crucible webhook secret
   - Events: select **Push** and **Pull requests**

   For GitLab: Settings → Webhooks → Add new webhook
   - URL: the Crucible webhook URL
   - Secret token: the Crucible webhook secret
   - Triggers: Push events, Merge request events

4. **Push a test commit.** A small change — a comment or whitespace edit — is sufficient. Verify that Crucible triggers a plan, the plan is clean, and (if auto-apply is on) the apply completes successfully.

5. **Remove the Spacelift webhook** from the git repository settings to eliminate any possibility of double-triggering.

> **Warning:** During the window between enabling the Crucible webhook and removing the Spacelift webhook, both systems will trigger runs from the same push events. Read-only plans are safe. If both systems reach the apply stage concurrently, one will acquire the state lock and the other will fail — Terraform/OpenTofu state locking prevents corruption. Keep this window short — minutes, not hours — and avoid merging infrastructure PRs during it.

## Step 8 — Clean up Spacelift

After running in Crucible for several days with no issues:

1. Export any Spacelift audit events you need to retain for compliance purposes
2. Delete the Spacelift stack
3. Remove Spacelift-specific context variables and policies no longer in use
4. Update team runbooks and documentation to reference Crucible

## Worked example — Cloudflare IaC stack

This section covers the concrete migration steps for a Cloudflare DNS and WAF stack previously managed in Spacelift. The stack manages DNS records, firewall rules, and page rules across one or more Cloudflare zones using the `cloudflare` Terraform provider.

### Stack config before (Spacelift)

| Field | Value |
| ----- | ----- |
| Repository | `github.com/<org>/cloudflare-iac` |
| Branch | `main` |
| Project root | `.` |
| Tool | Terraform 1.6 |
| Context attached | `cloudflare-prod` (contains `CLOUDFLARE_API_TOKEN`) |
| Auto-deploy | Yes |
| State backend | Spacelift-managed |

### Stack config after (Crucible)

| Field | Value |
| ----- | ----- |
| Repository | `github.com/<org>/cloudflare-iac` |
| Branch | `main` |
| Project root | `.` |
| Tool | OpenTofu 1.7 (or Terraform 1.6 — your choice) |
| Variable set | `cloudflare-creds` (created from the `cloudflare-prod` context) |
| Auto-apply | Yes |
| State backend | Crucible HTTP backend |

### Step-by-step for the Cloudflare stack

**1. Export state from Spacelift.**

```bash
curl -s -X GET \
  "https://<your-org>.app.spacelift.io/api/v1/stacks/cloudflare-iac/state" \
  -H "Authorization: Bearer $SPACELIFT_API_TOKEN" \
  -o cloudflare-iac.tfstate

# Confirm the state serial — note this number
jq '.serial' cloudflare-iac.tfstate
```

**2. Switch the backend.**

Update `backend.tf` in the `cloudflare-iac` repository:

```hcl
# backend.tf — before (Spacelift-managed state)
terraform {
  backend "s3" {
    bucket = "spacelift-state-<org>"
    key    = "cloudflare-iac/terraform.tfstate"
    region = "us-east-1"
  }
}
```

```hcl
# backend.tf — after (Crucible HTTP backend)
terraform {
  backend "http" {}
}
```

Commit this change to `main`:

```bash
git add backend.tf
git commit -m "chore: switch state backend from Spacelift to Crucible HTTP"
git push origin main
```

Do not trigger a Spacelift run — it will attempt to migrate state using the old S3 credentials and fail. Crucible initialises against the HTTP backend fresh on first run.

**3. Create the Crucible stack.**

In Crucible → Stacks → New stack:

- Name: `cloudflare-iac`
- Repository: `github.com/<org>/cloudflare-iac`
- Branch: `main`
- Project root: `.`
- Tool: OpenTofu 1.7 (or match the Terraform version from Spacelift)
- Auto-apply: Yes
- VCS webhook: **disabled for now**

**4. Upload the exported state.**

Retrieve the stack token from Stack → Settings → State backend in Crucible.

```bash
curl -s -X POST \
  -H "Authorization: Bearer <crucible-stack-token>" \
  -H "Content-Type: application/json" \
  -d @cloudflare-iac.tfstate \
  "https://<your-crucible-host>/api/v1/state/cloudflare-iac"

# Verify the upload
curl -s \
  -H "Authorization: Bearer <crucible-stack-token>" \
  "https://<your-crucible-host>/api/v1/state/cloudflare-iac" \
  | jq '.serial'
```

The serial number in the response must match the value you noted in step 1.

**5. Add environment variables.**

In Crucible → Variable Sets → New variable set, create a set named `cloudflare-creds`:

| Name | Value | Secret? |
| ---- | ----- | ------- |
| `CLOUDFLARE_API_TOKEN` | `<your Cloudflare API token>` | ✓ Yes |
| `TF_VAR_zone_id` | `<zone ID>` | No |
| `TF_VAR_account_id` | `<account ID>` | No |

Attach it to the `cloudflare-iac` stack via Stack → Variable Sets → Attach.

> **Note:** Generate the Cloudflare API token from the Cloudflare dashboard under Profile → API Tokens. The token needs at minimum `Zone:DNS:Edit` and `Zone:Firewall Services:Edit` permissions scoped to the zones this stack manages. Do not reuse an existing token — create one scoped specifically for Crucible.

**6. Trigger a manual plan.**

Go to Stack → New run → Plan. The plan should complete with **No changes**. If it shows changes, see the Troubleshooting section.

**7. Cut over.**

Follow Step 7 from the main guide:

- Disable the Spacelift `cloudflare-iac` stack
- Enable the Crucible VCS webhook
- Add the webhook to `github.com/<org>/cloudflare-iac` → Settings → Webhooks

Test the integration with a real change — for example, add a comment to a DNS record description:

```hcl
resource "cloudflare_record" "www" {
  zone_id = var.zone_id
  name    = "www"
  value   = "203.0.113.10"
  type    = "A"
  ttl     = 300
  comment = "Managed by Crucible IAP" # add this line
}
```

Open a PR with this change. Crucible should:

1. Post a plan comment on the PR showing one resource to update
2. Apply automatically on merge (if auto-apply is on)
3. Show the run as succeeded in the Crucible dashboard

**8. Remove the Spacelift webhook** from the GitHub repository settings. Delete the Spacelift `cloudflare-iac` stack after a few days of clean operation in Crucible.

## Troubleshooting

### Plan shows a diff on every run after state migration

The state file was uploaded before the backend switch commit was applied, so provider metadata or resource addresses differ between what is in state and what Crucible sees. Run `terraform init -migrate-state` locally against the Crucible HTTP backend to force a clean re-upload, then re-trigger the plan.

```bash
export TF_HTTP_ADDRESS="https://<your-crucible-host>/api/v1/state/<stack-id>"
export TF_HTTP_USERNAME="crucible"
export TF_HTTP_PASSWORD="<crucible-stack-token>"

terraform init -migrate-state
```

### `Error: Failed to get existing workspaces` on first plan

The HTTP backend credentials are not being passed. Test the state endpoint directly to isolate whether the issue is the token or the URL:

```bash
curl -I \
  -H "Authorization: Bearer <crucible-stack-token>" \
  "https://<your-crucible-host>/api/v1/state/<stack-id>"
```

A `200` means the token is valid and the endpoint is reachable. A `401` means the token was not found or has expired — regenerate it via Stack → Settings → State backend → Rotate token. A `404` means the stack ID in the URL is wrong — verify it matches the stack ID shown in the Crucible UI.

### Variable not being passed to Terraform

Spacelift contexts inject variables exactly as named. A Spacelift context variable named `zone_id` (without prefix) is accessible in the Spacelift runner via a Spacelift-specific mechanism. In Crucible, variable sets inject variables as plain environment variables — `zone_id` becomes the env var `zone_id`, which OpenTofu/Terraform does not map to `var.zone_id`.

Rename the variable in the Crucible variable set to `TF_VAR_zone_id`. OpenTofu/Terraform automatically maps `TF_VAR_*` env vars to input variables.

### Both systems triggered a run from the same push

You added the Crucible webhook before removing the Spacelift webhook and a push arrived during that window. Check which apply finished first using the state serial number — the apply that incremented the serial won. The other apply will show a state lock error or will detect no changes.

Once both runs settle, verify that `terraform show` output matches your expected infrastructure state. If the state serial is correct, no action is needed.

### PR plan comment not appearing

The VCS integration is not configured with sufficient permissions. In Crucible → Settings → Integrations, verify you have a GitHub or GitLab integration configured:

- GitHub: the token needs `pull_requests:write` scope (classic token) or the Pull Requests read/write permission (fine-grained token)
- GitLab: the token needs `api` scope

If the integration exists but comments are still not posting, check that the integration is attached to the stack via Stack → Settings → Integrations.

### Drift detection not firing at the expected schedule

Drift detection in Crucible uses cron expressions in UTC. Verify the schedule in Stack → Settings → Drift detection. Spacelift also uses UTC cron, but the field format differs slightly — Spacelift accepts five-field cron expressions; Crucible accepts standard five-field cron (`* * * * *`). If you copied a Spacelift schedule verbatim, confirm it is valid standard cron syntax.

See the [drift detection guide](drift-detection.md) for the full configuration reference.

### `Error: Invalid provider configuration` after switching to OpenTofu

If you migrated from Terraform 1.6 to OpenTofu 1.7 at the same time as the Crucible migration, the provider lock file (`.terraform.lock.hcl`) may reference Terraform-registry checksums that OpenTofu cannot verify. Delete the lock file from the repository and let OpenTofu regenerate it on first run:

```bash
git rm .terraform.lock.hcl
git commit -m "chore: remove Terraform lock file for OpenTofu regeneration"
git push origin main
```

Trigger a new plan in Crucible — OpenTofu regenerates `.terraform.lock.hcl` during `init` and commits it back if you have the auto-commit lock file option enabled. If not, copy the regenerated file from the run output and commit it manually.
