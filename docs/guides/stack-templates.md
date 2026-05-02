# Stack Templates

Stack templates let platform and ops teams pre-fill common stack settings so that new stacks start with sensible defaults rather than a blank form.

## Contents

1. [What stack templates are](#what-stack-templates-are)
2. [Creating a template (UI)](#creating-a-template-ui)
3. [Creating a template via API](#creating-a-template-via-api)
4. [Using a template to create a stack](#using-a-template-to-create-a-stack)
5. [Good template design](#good-template-design)
6. [Updating and deleting templates](#updating-and-deleting-templates)

---

## What stack templates are

A stack template is a reusable configuration that captures common stack settings. When a user creates a new stack from a template, the template's settings are copied in as defaults — the user can still override every field before saving.

Templates are created and maintained by admins. Members see the available templates in the new-stack form and pick whichever fits their use case.

### Templates vs. blueprints

| | Templates | Blueprints |
| --- | --- | --- |
| **Purpose** | Pre-fill common defaults for ops/platform teams | Form-driven self-service for app teams |
| **User experience** | Standard stack form, fields pre-populated | Dedicated deploy form with named input fields |
| **Created by** | Admins | Admins |
| **Used by** | Platform engineers creating new stacks | App teams deploying via a curated form |

Use a template when you want to encode sensible defaults without constraining the user. Use a blueprint when you want to expose a controlled deploy form with validated inputs. See the [Blueprints guide](../guides/../blueprints.md) for details on the blueprint workflow.

---

## Creating a template (UI)

1. Navigate to **Templates** in the left sidebar.
2. Click **New template**.
3. Fill in the template fields:

| Field | Required | Description |
| --- | --- | --- |
| Name | Yes | A short, descriptive label shown in the template picker (e.g. `AWS Terraform production`) |
| Description | No | Free-text note explaining the template's intended use |
| VCS repo URL | No | Git repository URL to pre-fill on all stacks created from this template |
| Branch / tag | No | Default branch or tag (e.g. `main`, `v1.2.0`) |
| Working directory | No | Path within the repo where the tool should run (leave blank to use the repo root) |
| Tool | No | One of `tofu`, `terraform`, `ansible`, or `pulumi` |
| Runner pool | No | The runner pool that stacks from this template should use by default |

4. Under **Environment variables**, add any variables that should be pre-set on every stack created from this template. Toggle **Secret** for values that should be stored encrypted and hidden in logs.
5. Under **Policies**, attach any policies that all stacks from this template should inherit — for example, tag-enforcement or blast-radius policies.
6. Click **Save**. The template is immediately available in the new-stack form.

---

## Creating a template via API

```http
POST /api/v1/stack-templates
Content-Type: application/json
```

```json
{
  "name": "AWS Terraform production",
  "description": "Standard configuration for production AWS stacks",
  "repo_url": "https://github.com/my-org/infra",
  "branch": "main",
  "working_dir": "stacks/aws",
  "tool": "tofu",
  "runner_pool_id": "pool_01hxyz",
  "env_vars": [
    { "key": "AWS_DEFAULT_REGION", "value": "eu-west-1", "secret": false },
    { "key": "TF_VAR_environment", "value": "production", "secret": false }
  ],
  "policy_ids": ["pol_01habc", "pol_01hdef"]
}
```

### Request body fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Display name for the template |
| `description` | string | No | Optional free-text description |
| `repo_url` | string | No | VCS repository URL |
| `branch` | string | No | Default branch or tag |
| `working_dir` | string | No | Working directory within the repo |
| `tool` | string | No | `tofu`, `terraform`, `ansible`, or `pulumi` |
| `runner_pool_id` | string | No | ID of the runner pool to attach by default |
| `env_vars` | array | No | List of `{ key, value, secret }` objects |
| `policy_ids` | array | No | List of policy IDs to attach to every stack from this template |

A successful request returns `201 Created` with the new template object including its `id`.

---

## Using a template to create a stack

### UI

1. Go to **Stacks** → **New stack**.
2. In the **Template** dropdown at the top of the form, select the template you want to use.
3. The form fields populate with the template's defaults. Review and override any fields that differ for this specific stack — name, working directory, environment variables, and so on.
4. Click **Save**. The new stack is fully independent from the template from this point on.

### API

Pass `template_id` in the stack creation request. Any fields you provide override the template defaults; fields you omit are filled in from the template.

```http
POST /api/v1/stacks
Content-Type: application/json
```

```json
{
  "name": "payments-service-prod",
  "template_id": "tmpl_01hxyz",
  "working_dir": "stacks/aws/payments"
}
```

In this example, `working_dir` overrides the template value while all other fields — `repo_url`, `branch`, `tool`, `runner_pool_id`, `env_vars`, and attached policies — are copied from the template.

> **Note:** Modifying a template after stacks have been created from it does **not** update those stacks. Template settings are copied at creation time. Existing stacks are never affected by template edits.

---

## Good template design

**One template per tool/team combination.** Rather than one catch-all template, create focused templates that match how teams actually work — for example, `AWS Terraform production`, `GCP staging`, and `Ansible playbooks`. This keeps the picker readable and the defaults meaningful.

**Set the runner pool on the template.** If all stacks for a given team run on the same pool, encode it in the template so new stacks do not default to the shared pool by accident.

**Pre-attach policies.** Attach blast-radius, tag-enforcement, and approval policies on the template rather than relying on per-stack configuration. Every stack created from the template inherits them automatically, which reduces the chance that a new stack skips a required control.

**Leave variable fields blank.** Only pre-fill settings that are genuinely shared. Leave the stack name and working directory empty if they vary per stack — users can fill them in during stack creation. Over-filling a template causes friction when the defaults need to be overridden every time.

**Use environment variables for non-secret config.** Pre-set variables like `AWS_DEFAULT_REGION`, `TF_VAR_environment`, or `ANSIBLE_INVENTORY` on the template so stacks pick them up automatically. Mark sensitive values as secret; note that secret values are stored encrypted and are never visible to users after the template is saved.

---

## Updating and deleting templates

### Editing a template

**UI:** Go to **Templates**, click the template name, then click **Edit**. Update any fields and click **Save**.

**API:**

```http
PATCH /api/v1/stack-templates/:id
Content-Type: application/json
```

```json
{
  "branch": "v2",
  "env_vars": [
    { "key": "AWS_DEFAULT_REGION", "value": "us-east-1", "secret": false }
  ]
}
```

Only the fields you include in the request body are updated. Fields you omit remain unchanged.

> **Note:** Editing a template does not affect any stacks that were previously created from it. Template settings are copied at stack-creation time and the stack is fully independent thereafter.

### Deleting a template

**UI:** Go to **Templates**, click the template name, click **Edit**, then click **Delete template** at the bottom of the page. Confirm the deletion in the dialog.

**API:**

```http
DELETE /api/v1/stack-templates/:id
```

Returns `204 No Content` on success. Deleting a template only removes the template itself — all stacks that were created from it continue to exist and operate normally.
