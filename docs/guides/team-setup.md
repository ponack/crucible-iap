# Team Setup Guide

This guide walks through setting up Crucible IAP for a team: creating your organisation, inviting members, restricting access to individual stacks, and enforcing approval gates.

## Contents

1. [Org roles vs stack roles](#org-roles-vs-stack-roles)
2. [Inviting team members](#inviting-team-members)
3. [Per-stack access control](#per-stack-access-control)
4. [Approval policies and run gates](#approval-policies-and-run-gates)
5. [Recommended starter setup](#recommended-starter-setup)

---

## Org roles vs stack roles

Crucible has two independent access layers.

### Org roles

Set in **Settings** → **Members**. Controls what a user can do across the entire organisation.

| Role | What they can do |
| --- | --- |
| `admin` | Everything: create stacks, manage policies, invite/remove members, set org defaults |
| `member` | View all stacks and runs, trigger runs on stacks they are a member of |

Every user who can log in has at least `member`. Admins are typically the platform team leads or CI/CD owners.

### Stack roles

Set per-stack in **Stacks** → *stack name* → **Access** (admin-only section). Controls what a specific user can do on that one stack.

| Stack role | Can view | Can trigger/confirm | Can approve |
| --- | --- | --- | --- |
| `viewer` | Yes | No | No |
| `approver` | Yes | No | Yes |
| `operator` | Yes | Yes | Yes |

If a user has no explicit stack role, they default to `viewer` on any stack they can see (all stacks are visible to org members). Org admins always have full access regardless of stack role.

> **Tip:** Use `viewer` for stakeholders and auditors who need visibility without the ability to trigger changes. Use `approver` for a second set of eyes on a production stack — they can approve pending runs without being able to trigger new ones.

---

## Inviting team members

1. Go to **Settings** → **Members** → **Invite member**
2. Enter the user's email address and select an org role (`member` or `admin`)
3. Click **Send invite**

The invitee receives an email with a link to complete signup via your configured OIDC provider. Once they log in, they appear in the members list.

To change an existing member's org role, click the role badge next to their name and select the new role.

---

## Per-stack access control

By default, all org members can see every stack. To restrict who can operate on a specific stack:

1. Open the stack → scroll to the **Access** section (visible to admins only)
2. Click **Add member** and select a user from the dropdown
3. Choose the appropriate stack role (`viewer`, `approver`, or `operator`)
4. Click **Add**

To change a role for an existing member, use the inline role dropdown in the Access table. To remove access entirely, click **Remove**.

### Typical patterns

**Production stacks** — Restrict triggers to a small operator group; add the broader team as `viewer` so they can see what is deployed without being able to change it.

**Staging stacks** — Give developers `operator` access so they can iterate freely.

**Shared-infra stacks** — Keep as `viewer` for most, `operator` for the platform team only.

---

## Approval policies and run gates

Approval policies let you require human sign-off before a plan is applied, without hard-blocking the run. This is the recommended approach for production changes.

### How it works

1. Attach an `approval` policy to a stack (or set it as an org default to cover all stacks)
2. After the plan completes, Crucible evaluates all approval policies for the stack
3. If any policy returns `require_approval: true`, the run moves to **Pending approval** instead of waiting for a normal confirm
4. A user with `approver` or `operator` stack role (or an org admin) opens the run and clicks **Approve**
5. The run continues to apply normally — or, if the stack has auto-apply enabled, it applies immediately after approval

Discarding a pending-approval run cancels it with no changes applied.

### Example: require approval before any destroy

```rego
package crucible

import future.keywords.if

approval := result if {
  result := {
    "require_approval": require_approval,
    "reason":           reason,
  }
}

deletes := [r |
  some r in input.resource_changes
  "delete" in r.change.actions
]

require_approval := count(deletes) > 0

reason := msg if {
  require_approval
  msg := sprintf("%v resource(s) would be deleted", [count(deletes)])
} else := ""
```

Save this as an `approval`-type policy and set it as an org default to protect all stacks automatically. See [docs/policies/approval-for-destroy.rego](../policies/approval-for-destroy.rego) for the ready-to-use version.

### Pairing approval with warn-on-delete

For the best operator experience, attach both:

- `warn-on-delete.rego` (`post_plan`) — shows a non-blocking warning listing every resource that would be deleted, visible when the plan finishes
- `approval-for-destroy.rego` (`approval`) — gates the apply until someone approves

The warn fires first so the approver can see the full list of deletions in the run detail before approving.

---

## Recommended starter setup

Here is a minimal policy configuration that covers most teams on day one. All of these templates are in [docs/policies/](../policies/).

| Policy file | Type | Apply as |
| --- | --- | --- |
| `require-tags.rego` | `post_plan` | Org default |
| `blast-radius-guard.rego` | `post_plan` | Org default |
| `warn-on-delete.rego` | `post_plan` | Org default |
| `approval-for-destroy.rego` | `approval` | Org default |
| `no-destroy.rego` | `post_plan` | Production stacks only |
| `instance-type-allowlist.rego` | `post_plan` | Production stacks only |

**Step by step:**

1. Go to **Policies** → **New policy**
2. Paste the Rego from the template file
3. Set the policy type to match the comment at the top of the file
4. For org defaults: toggle **Set as org default** before saving
5. For stack-specific policies: save the policy, then open each stack and attach it via the **Policies** section

Once these are in place, any new stack automatically inherits the org defaults — no manual attachment needed.
