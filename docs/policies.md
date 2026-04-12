# Crucible IAP — Policy Authoring Guide

Crucible uses [OPA (Open Policy Agent)](https://www.openpolicyagent.org/) with Rego policies to enforce guardrails on infrastructure runs. Policies are stored in the database, compiled at load time, and evaluated in microseconds.

## Contents

1. [How policies work](#how-policies-work)
2. [Policy types](#policy-types)
3. [Input shape](#input-shape)
4. [Output shape](#output-shape)
5. [Examples](#examples)
6. [Assigning policies to stacks](#assigning-policies-to-stacks)
7. [Org-level defaults](#org-level-defaults)
8. [Dry-run sandbox](#dry-run-sandbox)
9. [Testing policies locally](#testing-policies-locally)

---

## How policies work

1. You create a policy in the UI (**Policies** → **New policy**) or via the API
2. Crucible compiles the Rego at save time — invalid Rego is rejected immediately
3. Active policies are loaded into the in-memory OPA engine on API startup and whenever you save a change
4. When a run reaches the relevant lifecycle hook, all policies of that type assigned to the stack are evaluated
5. If any policy produces a `deny` message, the run is blocked and the message shown to the operator
6. `warn` messages are non-blocking — they are recorded and surfaced but do not stop the run
7. Per-policy evaluation results (pass / deny / warn) are stored and shown as a badge row on the run detail page

---

## Policy types

| Type | Hook | Rule name | Typical use |
| --- | --- | --- | --- |
| `post_plan` | After plan, before confirmation | `plan` | Block dangerous changes (most common) |
| `pre_plan` | Before plan starts | `plan` | Validate stack configuration |
| `pre_apply` | After confirmation, before apply | `plan` | Final safety check |
| `trigger` | After a run completes | `trigger` | Trigger downstream stacks |
| `login` | On user login | `login` | Restrict which users can log in |

The **rule name** is the top-level Rego rule Crucible queries — `data.crucible.<rule-name>`. Plan-type policies (`post_plan`, `pre_plan`, `pre_apply`) all use the `plan` rule; `trigger` and `login` use their own matching rule name.

---

## Input shape

The `input` document varies by policy type.

### Plan policies (`post_plan`, `pre_plan`, `pre_apply`)

Receive the Terraform/OpenTofu plan JSON. The top-level keys are:

```json
{
  "format_version": "1.2",
  "resource_changes": [ ... ],
  "configuration": { ... },
  "variables": { ... }
}
```

The most useful field is `resource_changes`:

```json
{
  "address": "aws_instance.web",
  "type": "aws_instance",
  "name": "web",
  "change": {
    "actions": ["create"],       // "create" | "update" | "delete" | "no-op"
    "before": null,              // state before change (null for create)
    "after": { "instance_type": "t3.large", ... }
  }
}
```

### Login policies

Receive the authenticated user's identity:

```json
{
  "user": {
    "email": "alice@example.com",
    "name": "Alice"
  },
  "groups": ["engineering", "platform-team"]
}
```

`groups` is populated from the OIDC `groups` claim if your IdP provides it; otherwise it is an empty array.

### Trigger policies

Receive the same plan JSON as plan-type policies (the plan from the completed run), plus a top-level `"run"` key with run metadata:

```json
{
  "run": {
    "id": "<uuid>",
    "type": "tracked",
    "stack_id": "<uuid>",
    "triggered_by": "push"
  },
  "resource_changes": [ ... ],
  ...
}
```

---

## Output shape

Your Rego policy must return an object with three keys from the `data.crucible` namespace. The rule name matches the policy type (see [Policy types](#policy-types)):

```rego
package crucible

# Used by post_plan, pre_plan, pre_apply
plan := result if {
  result := {
    "deny":    deny_msgs,     # set of strings — each blocks the run
    "warn":    warn_msgs,     # set of strings — non-blocking, shown to operator
    "trigger": [],            # unused for plan policies; must be present
  }
}

# Used by trigger policies
trigger := result if {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": downstream_stack_ids,  # set of stack UUIDs to queue a run on
  }
}

# Used by login policies
login := result if {
  result := {
    "deny":    deny_msgs,     # non-empty set blocks the login
    "warn":    [],
    "trigger": [],
  }
}
```

Policy results (deny/warn messages) are recorded per run and shown as a badge row on the run detail page.

---

## Examples

### Block destroy operations

```rego
package crucible

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs contains msg if {
  input.resource_changes[_].change.actions[_] == "delete"
  msg := "destroy operations are not permitted via automated runs — use an explicit destroy run"
}

warn_msgs contains msg if {
  r := input.resource_changes[_]
  r.change.actions[_] == "update"
  msg := sprintf("resource %v will be modified", [r.address])
}
```

---

### Enforce instance type allowlist

```rego
package crucible

allowed_instance_types := {"t3.micro", "t3.small", "t3.medium", "t3.large"}

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs contains msg if {
  r := input.resource_changes[_]
  r.type == "aws_instance"
  r.change.actions[_] != "delete"
  instance_type := r.change.after.instance_type
  not allowed_instance_types[instance_type]
  msg := sprintf("instance type %v is not in the approved list: %v", [instance_type, allowed_instance_types])
}
```

---

### Require tags on all resources

```rego
package crucible

required_tags := {"owner", "environment", "cost-centre"}

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs contains msg if {
  r := input.resource_changes[_]
  r.change.actions[_] != "delete"
  r.change.actions[_] != "no-op"
  tags := object.get(r.change.after, "tags", {})
  missing := required_tags - {k | tags[k]}
  count(missing) > 0
  msg := sprintf("resource %v is missing required tags: %v", [r.address, missing])
}
```

---

### Limit the blast radius (max resources changed)

```rego
package crucible

max_changes := 10

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

changing := [r | r := input.resource_changes[_]; r.change.actions[_] != "no-op"]

deny_msgs contains msg if {
  count(changing) > max_changes
  msg := sprintf("this plan modifies %v resources — limit is %v; split into smaller changes", [count(changing), max_changes])
}

warn_msgs contains msg if {
  count(changing) > (max_changes / 2)
  count(changing) <= max_changes
  msg := sprintf("this plan modifies %v resources — approaching the limit of %v", [count(changing), max_changes])
}
```

---

### Trigger a downstream stack (trigger policy)

```rego
package crucible

trigger := result if {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": downstream_stacks,
  }
}

# After this stack's run completes successfully, trigger the downstream stack.
downstream_stacks := ["<downstream-stack-uuid>"] if {
  # Only trigger if something actually changed (not a no-op plan)
  changed := [r | r := input.resource_changes[_]; r.change.actions[_] != "no-op"]
  count(changed) > 0
}

else := []
```

---

## Assigning policies to stacks

1. Create the policy in **Policies**
2. Open the stack in **Stacks**
3. Scroll to the **Policies** section
4. Select the policy from the dropdown and click **Attach**

A stack can have multiple policies of the same type — all are evaluated, and any single `deny` blocks the run.

---

## Org-level defaults

Admins can mark a policy as an **org default**, which automatically applies it to every stack in the organisation — no manual attachment needed.

To set a policy as an org default:

1. Open the policy in **Policies** → select a policy → **Edit**
2. Toggle **Set as org default** (admin-only; the toggle is hidden for non-admins)
3. Click **Save**

Policies marked as org defaults show an **Org default** badge in the policy list. They are evaluated on every run alongside any stack-specific policies. Removing the org-default flag does not delete the policy — it simply stops being applied automatically.

---

## Dry-run sandbox

The **Test policy** panel on the policy edit page lets you evaluate your policy against any JSON input without saving or triggering a real run.

1. Open the policy in **Policies** → **Edit**
2. Expand the **Test policy** panel
3. Paste a plan JSON (or user object for `login` policies) into the input field
4. Click **Run test**
5. Results appear inline — pass, deny messages, warn messages, and any triggered stack IDs

This is the fastest way to iterate on policy logic. The underlying endpoint (`POST /api/v1/policies/validate`) accepts an optional `input` field and evaluates the policy source without persisting anything.

The **Input schema explorer** panel (also on create and edit pages) shows the exact `input` shape and a typed field reference for the selected policy type, so you know what fields are available without leaving the editor.

---

## Testing policies locally

For local CI or pre-commit validation, you can evaluate policies with the OPA CLI directly — no running Crucible instance needed. For interactive iteration, the [dry-run sandbox](#dry-run-sandbox) in the UI is faster.

Install OPA:

```bash
brew install opa  # macOS
# or download from https://www.openpolicyagent.org/docs/latest/#running-opa
```

Get a plan JSON:

```bash
cd your-terraform-dir
terraform plan -out=plan.bin
terraform show -json plan.bin > plan.json
```

Evaluate your policy:

```bash
opa eval \
  --data your-policy.rego \
  --input plan.json \
  "data.crucible.plan"
```

Expected output for a passing policy:

```json
{
  "result": [{
    "expressions": [{
      "value": { "deny": [], "warn": [], "trigger": [] }
    }]
  }]
}
```
