# Crucible IAP — Policy Authoring Guide

Crucible uses [OPA (Open Policy Agent)](https://www.openpolicyagent.org/) with Rego policies to enforce guardrails on infrastructure runs. Policies are stored in the database, compiled at load time, and evaluated in microseconds.

## Contents

1. [How policies work](#how-policies-work)
2. [Policy types](#policy-types)
3. [Input shape](#input-shape)
4. [Output shape](#output-shape)
5. [Examples](#examples)
6. [Assigning policies to stacks](#assigning-policies-to-stacks)
7. [Testing policies locally](#testing-policies-locally)

---

## How policies work

1. You create a policy in the UI (**Policies** → **New policy**) or via the API
2. Crucible compiles the Rego at save time — invalid Rego is rejected immediately
3. Active policies are loaded into the in-memory OPA engine on API startup and whenever you save a change
4. When a run reaches the relevant lifecycle hook, all policies of that type assigned to the stack are evaluated
5. If any policy produces a `deny` message, the run is blocked and the message shown to the operator
6. `warn` messages are non-blocking — they are recorded and surfaced but do not stop the run

---

## Policy types

| Type | Hook | Typical use |
|------|------|-------------|
| `post_plan` | After plan, before confirmation | Block dangerous changes (most common) |
| `pre_plan` | Before plan starts | Validate stack configuration |
| `pre_apply` | After confirmation, before apply | Final safety check |
| `trigger` | After a run completes | Trigger downstream stacks |
| `login` | On user login | Restrict which users can log in |

---

## Input shape

All policy types receive the Terraform/OpenTofu plan JSON as input. The top-level keys are:

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

---

## Output shape

Your Rego policy must return an object with three keys from the `data.crucible` namespace:

```rego
package crucible

plan := result {
  result := {
    "deny":    deny_msgs,     # set of strings — each blocks the run
    "warn":    warn_msgs,     # set of strings — non-blocking, shown to operator
    "trigger": [],            # set of stack IDs to trigger (trigger policies only)
  }
}
```

The policy is evaluated with the query matching its type (e.g. `data.crucible.plan` for `post_plan`).

---

## Examples

### Block destroy operations

```rego
package crucible

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "delete"
  msg := "destroy operations are not permitted via automated runs — use an explicit destroy run"
}

warn_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "update"
  msg := sprintf("resource %v will be modified", [input.resource_changes[_].address])
}
```

---

### Enforce instance type allowlist

```rego
package crucible

allowed_instance_types := {"t3.micro", "t3.small", "t3.medium", "t3.large"}

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs[msg] {
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

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs[msg] {
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

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

changing := [r | r := input.resource_changes[_]; r.change.actions[_] != "no-op"]

deny_msgs[msg] {
  count(changing) > max_changes
  msg := sprintf("this plan modifies %v resources — limit is %v; split into smaller changes", [count(changing), max_changes])
}

warn_msgs[msg] {
  count(changing) > (max_changes / 2)
  count(changing) <= max_changes
  msg := sprintf("this plan modifies %v resources — approaching the limit of %v", [count(changing), max_changes])
}
```

---

### Trigger a downstream stack (trigger policy)

```rego
package crucible

trigger := result {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": downstream_stacks,
  }
}

# After this stack's run completes successfully, trigger the downstream stack.
downstream_stacks := ["<downstream-stack-uuid>"] {
  # Only trigger if something actually changed (not a no-op plan)
  changed := [r | r := input.resource_changes[_]; r.change.actions[_] != "no-op"]
  count(changed) > 0
}

downstream_stacks := [] { true }
```

---

## Assigning policies to stacks

1. Create the policy in **Policies**
2. Open the stack in **Stacks**
3. Scroll to the **Policies** section
4. Select the policy from the dropdown and click **Attach**

A stack can have multiple policies of the same type — all are evaluated, and any single `deny` blocks the run.

---

## Testing policies locally

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
