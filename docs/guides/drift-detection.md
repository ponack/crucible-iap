# Drift Detection Guide

Drift detection periodically runs a plan-only check on a stack and alerts you when the actual infrastructure no longer matches the last applied state. This catches out-of-band changes — manual edits in the cloud console, changes from other automation tools, or resources modified by automated processes.

## Contents

1. [How drift detection works](#how-drift-detection-works)
2. [Enabling drift detection](#enabling-drift-detection)
3. [Auto-remediation](#auto-remediation)
4. [Reading drift alerts in the UI](#reading-drift-alerts-in-the-ui)
5. [Triggering a one-off drift check](#triggering-a-one-off-drift-check)
6. [OPA policies for drift](#opa-policies-for-drift)
7. [Troubleshooting](#troubleshooting)

---

## How drift detection works

A background scheduler runs every minute and checks each stack with drift detection enabled. When a stack's `drift_last_run_at + schedule_interval ≤ now()`, the scheduler enqueues a `proposed` run with `trigger=drift_detection` and `is_drift=true`.

The run executes the same planning step as a normal proposed run — `tofu plan` (or the equivalent for Ansible/Pulumi). If the plan shows any additions, changes, or deletions, the run is marked as a drift alert in the UI.

Drift runs do not apply changes and do not require confirmation. They are purely informational unless auto-remediation is enabled.

---

## Enabling drift detection

Open **Stacks** → *stack name* → **Edit** and scroll to **Drift detection**:

| Setting | Description |
| --- | --- |
| Enable drift detection | Toggle on to activate scheduled checks |
| Drift check interval | How often to run a check, in minutes (e.g. `60` = hourly, `1440` = daily) |
| Auto-remediate drift | When enabled, automatically applies the plan if drift is detected (see below) |

A `60`-minute interval is a reasonable starting point for most stacks. Use a longer interval (e.g. `1440`) for stable production stacks where frequent drift checks would be noisy.

---

## Auto-remediation

When **Auto-remediate drift** is enabled on a stack, Crucible automatically queues a `tracked` run with auto-apply after a drift run finishes with a non-empty plan. No human confirmation is required.

The remediation run applies the plan immediately — use this only on stacks where you are confident the Crucible state is the authoritative source of truth and all out-of-band changes should be overwritten.

> **Warning:** Auto-remediation will destroy resources that exist in the cloud but not in your Terraform state. Ensure your state is complete and accurate before enabling this option.

A safe pattern is to enable drift detection first (without auto-remediation), monitor alerts for a week, and only enable auto-remediation once you are confident in the signal quality.

---

## Reading drift alerts in the UI

When a drift check detects changes, the stack card on the dashboard shows a **Drift detected** badge. Click through to the stack detail page to see the most recent drift run.

On the run detail page:
- The plan delta badge (`+N ~N -N`) shows the number of resources that would be added, changed, or destroyed
- The full plan output is stored as an artifact — click **View plan** to inspect exactly what drifted
- The run trigger is shown as `drift_detection` in the run history

If you want to fix the drift manually, make the necessary changes in your code and push — the next triggered run will bring the stack back into sync. Or click **Trigger proposed run** to run a fresh check immediately.

---

## Triggering a one-off drift check

From the stack detail page, click **Check drift** to enqueue an immediate drift check outside the normal schedule. This is useful after a suspected manual change in the cloud console.

You can also trigger a check via the API:

```bash
curl -X POST https://crucible.example.com/api/v1/stacks/<stack-id>/drift \
  -H "Authorization: Bearer <your-jwt>"
```

---

## OPA policies for drift

Attach policies to control what happens when drift is detected.

### Alert only on significant drift

A `post_plan` policy that warns when a drift run detects destructive changes (useful as an early warning before auto-remediation is enabled):

```rego
package crucible

plan := result if {
  result := {
    "deny":    [],
    "warn":    warn_msgs,
    "trigger": [],
  }
}

warn_msgs contains msg if {
  input.run.is_drift
  input.plan_summary.destroy > 0
  msg := sprintf("drift detected: %d resource(s) would be destroyed", [input.plan_summary.destroy])
}
```

### Page on-call when drift is detected

Use a `trigger` policy to fire a downstream notification stack (a stack whose sole purpose is to page an on-call rotation):

```rego
package crucible

plan := result if {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": trigger_ids,
  }
}

trigger_ids := ["<notification-stack-id>"] if {
  input.run.is_drift
  input.plan_summary.change + input.plan_summary.add + input.plan_summary.destroy > 0
} else := []
```

---

## Troubleshooting

### No drift checks are running

Verify drift detection is enabled on the stack and the interval field is set to a valid integer (minutes). The scheduler skips stacks with blank or non-integer `drift_schedule` values.

Check worker logs:

```bash
docker compose logs crucible-worker | grep drift
```

### Drift runs show constant changes

The plan is always non-empty even after a successful apply. Common causes:

- **Non-deterministic resource attributes** — some providers generate random values (e.g. timestamps, computed IDs) that change on every plan. Mark them with `lifecycle { ignore_changes = [...] }` in your Terraform code.
- **Stale state** — the stack's state file does not match what was last applied. Trigger a manual tracked run to re-sync.
- **Clock skew** — the runner container clock differs significantly from the cloud provider API. Verify NTP is working on the host.

### Auto-remediation not triggering

Check that both drift detection and auto-remediation are enabled on the stack. Auto-remediation only fires when the drift run has a non-empty plan (`plan_add + plan_change + plan_destroy > 0`) and the run type is `proposed` with `is_drift = true`.

Also check for any `post_plan` policies attached to the stack — a policy `deny` blocks the drift run from completing, which also blocks auto-remediation.
