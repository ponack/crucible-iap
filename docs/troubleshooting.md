# Troubleshooting

When something goes wrong, start here. This page covers errors users hit during *daily use* — running stacks, reviewing plans, hitting policies. For deployment / installation problems (API won't start, migrations fail, runner network missing), see [`operator-guide.md#troubleshooting`](operator-guide.md#troubleshooting).

---

## Plan failed — how do I read the error?

A run with status `failed` means the runner exited non-zero. Open the run detail page and scroll the log to find the line that starts with `Error:` (or `error:` for OpenTofu's lowercase variant).

**Common patterns:**

| Log message | What it means | Fix |
| --- | --- | --- |
| `Error: Reference to undeclared resource` | You referenced `aws_instance.web.id` but never declared `aws_instance.web` | Add the missing resource or fix the typo |
| `Error: Missing required argument` | A required field on a resource is empty | Add the field, often `name`, `region`, or `ami` |
| `Error: Invalid provider configuration` | Provider credentials are missing or wrong | Check the stack's environment variables (or OIDC federation settings) |
| `Error: Failed to query available provider packages` | Network problem reaching the provider registry | Retry; if persistent, configure the runner's HTTP proxy |
| `Error: Inconsistent dependency lock file` | The `.terraform.lock.hcl` in your repo doesn't match the providers you're using | Run `terraform init -upgrade` locally and commit the new lock file |
| `Error: state snapshot was created by Terraform vX.Y.Z, which is newer than current vA.B.C` | Stack tool version is older than what produced the state | Set the **Tool version** on the stack to match (or upgrade Terraform locally and re-init) |

Use the **AI explain failure** button (when `ANTHROPIC_API_KEY` is configured) for a plain-English summary of the error and likely fix.

---

## "Confirm" button is missing on a tracked run

A run in `unconfirmed` state should show a **Confirm** button. If it doesn't:

1. **You lack permission.** Confirming requires `member` or `admin` role on the org (or `approver` membership on the stack). Viewers can read plans but not apply.
2. **A policy denied the plan.** Look for a red banner above the plan output. The run cannot be confirmed until the policy violation is fixed in code.
3. **An approval gate is pending.** Approval policies require a designated approver to approve before anyone (including the original requester) can confirm. The Approve button is visible only to the approver(s).
4. **The plan has no changes.** Crucible auto-finishes runs with empty plans — no confirm needed.

---

## Run stuck in `queued` or `preparing`

The run was triggered but the worker hasn't picked it up.

**Check these in order:**

1. **Worker container is running:** `docker compose ps crucible-worker` should show `Up`.
2. **Worker logs:** `docker compose logs crucible-worker --tail 50`. Look for "claim returned no work" (worker is idle, queue is genuinely empty) vs "failed to claim" (DB connection issue) vs nothing at all (worker is wedged — restart it).
3. **Concurrent run cap reached:** `RUNNER_MAX_CONCURRENT` defaults to `5`. If 5 runs are already in `preparing` or `planning`, new runs wait. Bump the cap or wait.
4. **Stack is assigned to a worker pool with no agents:** If you set the stack to use a worker pool, the queue advances only when an agent claims it. Check Settings → Worker Pools.
5. **Stack is locked:** Locked stacks queue runs but don't execute them. Unlock the stack.

---

## State is locked — what does that mean?

Terraform uses a per-state lock to prevent two apply operations from running simultaneously. The lock is released when the run finishes; if the runner is killed mid-apply (timeout, crash, manual `docker kill`), the lock can stay held.

**To release:**

1. Stack detail → **State** tab → **Force unlock** (admin only).
2. Verify no other run is actually applying — force-unlocking a real concurrent run will corrupt state.

If you're not sure whether a run is still active, check the runs list. If the most recent run is in a terminal state (`finished`, `failed`, `discarded`) but the lock persists, force-unlock is safe.

---

## Policy denied my plan — how do I see why?

When a policy returns `deny`, the run is blocked. The denial messages appear:

- On the run detail page in a red banner above the plan
- In PR comments (if webhook is connected)
- In Slack/Teams notifications

Each message tells you *which* policy denied and *what* condition failed. The policy itself is viewable at **Policies → *name***.

**To unblock:**

- **Fix the code** — change your Terraform so the policy passes (e.g. add a required tag, remove a public security group rule).
- **Request a policy exception** — talk to the policy author; some teams maintain an allowlist or accept overrides for specific stacks.
- **Bypass via approval** — some policies emit `deny` but mark the run as `pending_approval` instead of `failed`. An approver can override these.

---

## Webhook isn't triggering runs on push

The most common causes, in order of likelihood:

1. **Wrong webhook URL.** Each stack has its own URL. Copy from Stack detail → Settings.
2. **Wrong content type.** GitHub: must be `application/json`. GitLab: default is fine. Bitbucket: default is fine.
3. **Wrong secret.** The webhook secret in your VCS settings must exactly match the stack's webhook secret. Copy-paste to be sure.
4. **Events not subscribed.** GitHub: tick **Pushes** and **Pull requests**. GitLab: tick **Push events** and **Merge request events**.
5. **Crucible URL not reachable from the VCS.** If you're hosting Crucible on a private network, the public VCS (github.com, gitlab.com) cannot reach it. Either expose Crucible publicly or run a self-hosted VCS that can see Crucible.
6. **Branch mismatch.** Push events for branches *other than* the stack's configured branch are ignored. PR events from any branch are processed.

To debug: the VCS webhook UI shows recent deliveries and the response Crucible returned. A `200` means Crucible received and processed it; a `4xx` or `5xx` shows what failed.

---

## "Failed to authenticate to provider"

Cloud credentials aren't reaching the runner.

**Static credentials (e.g. `AWS_ACCESS_KEY_ID`):**

1. Stack detail → **Environment variables** → confirm the keys are set.
2. Confirm they're set as **Secret** (visible value is masked) or **Plain** depending on whether they're sensitive.
3. Trigger a new run — env var changes don't affect in-flight runs.

**OIDC workload identity federation:**

1. Confirm the IAM role / workload identity pool trust policy includes the right `sub` value (`stack:<your-stack-slug>`).
2. Confirm `CRUCIBLE_BASE_URL` is reachable from the cloud provider (needed for JWKS fetch).
3. Look at the runner log for the token exchange — failure messages name the specific claim that didn't match.

See [`operator-guide.md#cloud-oidc-workload-identity-federation`](operator-guide.md#cloud-oidc-workload-identity-federation) for the full setup.

---

## Login redirects in a loop / OIDC errors

Symptoms: clicking "Sign in" sends you to your IdP, which sends you back to Crucible, which sends you back to the IdP, etc.

**Common causes:**

1. **`OIDC_REDIRECT_URL` doesn't match.** Must be `<CRUCIBLE_BASE_URL>/auth/callback` exactly, including scheme. Update the same URL in your IdP's allowed redirect URIs.
2. **`CRUCIBLE_BASE_URL` is `http://localhost`** but OIDC requires HTTPS. Use a real hostname behind TLS for OIDC flows.
3. **Clock skew.** JWTs include an `iat` claim; if the Crucible host's clock is more than ~5 minutes off your IdP's clock, tokens are rejected as already-expired or not-yet-valid. Run `timedatectl status` and fix the time source.

For local-auth-only deployments, set `LOCAL_AUTH_ENABLED=true` and skip OIDC.

---

## My run finished but outputs are missing

Two possibilities:

1. **The output is `sensitive = true`.** Sensitive outputs are stored in state but not shown in the UI. Read them via the API or by exporting state.
2. **The plan succeeded but apply was skipped.** Proposed runs never apply; their outputs are computed values from the plan, not real values. Trigger a tracked run instead.

---

## "Provider produced inconsistent result after apply"

This is a Terraform internal-consistency check failing. Usually means a provider bug or a race condition (e.g. a resource that takes time to settle). Try:

1. Re-running the same plan — many cases self-heal on the second try.
2. Upgrading the provider version in `required_providers`.
3. Filing a bug with the provider author with the run log.

Not a Crucible bug — same behaviour occurs running Terraform locally.

---

## I see "drift detected" but I didn't change anything

Drift can happen for legitimate reasons:

- Someone changed the resource manually in the cloud console.
- Another tool (a CI pipeline, a script, an operator running `aws cli`) modified the same resource.
- The cloud provider auto-updated something (e.g. AWS adds default tags).
- A provider version bump changed how the resource is read back.

Open the drift report on the stack detail page; the diff shows exactly what changed.

**Three responses:**

- **Accept the change** — trigger a tracked apply. Terraform updates state to match reality.
- **Revert the change** — trigger a tracked apply *after* fixing your code to enforce the desired state.
- **Investigate** — drift you can't explain might indicate a compromise. Check the audit log of the affected cloud account.

---

## Stack health is red — what does that mean?

"Health" is a rolling indicator based on:

- Recent run success rate
- Drift status
- Open policy violations
- Time since last successful apply

Red means something is meaningfully wrong. Click into the stack to see which factor is contributing. A long-untouched stack with no recent runs can appear amber even if everything is fine — it's a signal to verify, not necessarily a fault.

---

## I can't find an old plan / log

By default Crucible keeps run logs and plan artifacts forever. If you've set **Artifact retention days** in Settings → Retention, anything older than the retention window is deleted by the daily cleanup job.

If your retention is `0` (forever) and a log is missing, check the audit log for any manual deletion events.

---

## The "Explain failure" button is missing

It only appears on failed runs when `ANTHROPIC_API_KEY` is set in `.env`. Set the key (Anthropic Console → API keys) and restart `crucible-api`.

---

## Still stuck?

- Check [`operator-guide.md#troubleshooting`](operator-guide.md#troubleshooting) for deployment-side issues.
- Browse the audit log — every state change is recorded with who/what/when.
- File an issue at [github.com/ponack/crucible-iap/issues](https://github.com/ponack/crucible-iap/issues) with: the run ID, the redacted log, what you expected to happen, and what actually happened.
