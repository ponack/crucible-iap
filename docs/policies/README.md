# Crucible IAP — Policy Templates

Ready-to-use Rego policies. Copy any file into the Crucible UI (**Policies → New policy**), select the matching type, and attach it to your stacks.

See [docs/policies.md](../policies.md) for a full reference on policy types, input shapes, and the dry-run sandbox.

---

## Templates

| File | Type | What it does |
| --- | --- | --- |
| [`no-destroy.rego`](no-destroy.rego) | `post_plan` | Hard-block any run that would delete resources |
| [`warn-on-delete.rego`](warn-on-delete.rego) | `post_plan` | Warn (not block) when resources are deleted |
| [`blast-radius-guard.rego`](blast-radius-guard.rego) | `post_plan` | Block or warn when too many resources change at once |
| [`require-tags.rego`](require-tags.rego) | `post_plan` | Enforce mandatory resource tags |
| [`instance-type-allowlist.rego`](instance-type-allowlist.rego) | `post_plan` | Restrict AWS EC2 instance types to an approved list |
| [`no-public-access.rego`](no-public-access.rego) | `post_plan` | Block publicly-accessible S3 buckets and EC2 instances |
| [`restrict-regions.rego`](restrict-regions.rego) | `post_plan` | Enforce an AWS region allowlist |
| [`approval-for-destroy.rego`](approval-for-destroy.rego) | `approval` | Require sign-off before any destroy run proceeds |
| [`approval-high-blast-radius.rego`](approval-high-blast-radius.rego) | `approval` | Require sign-off when many resources change |
| [`login-domain-allowlist.rego`](login-domain-allowlist.rego) | `login` | Restrict logins to specific email domains |
| [`trigger-downstream.rego`](trigger-downstream.rego) | `trigger` | Trigger a downstream stack after a successful apply |

---

## Usage

1. Open **Policies → New policy** in the Crucible UI
2. Paste the template content into the **Rego** editor
3. Set the **Type** to match the template (shown in the table above)
4. Click **Save**
5. Attach the policy to one or more stacks from the **Stack → Policies** section, or set it as an **Org default** to apply it everywhere

Most templates contain a configuration block near the top — update the values for your environment before saving.
