# Blueprints Guide

Blueprints are parameterized stack templates with a self-service deploy form — platform engineers publish a blueprint once, and app teams spin up new stacks without touching IaC config.

## Contents

1. [Overview](#overview)
2. [Creating a blueprint](#creating-a-blueprint)
3. [Managing params](#managing-params)
4. [Publishing a blueprint](#publishing-a-blueprint)
5. [Deploying a blueprint](#deploying-a-blueprint)
6. [How params map to Terraform](#how-params-map-to-terraform)
7. [Best practices](#best-practices)

---

## Overview

A blueprint wraps an IaC module with a named set of input parameters. When an app team deploys the blueprint, Crucible presents those parameters as a form, creates the stack, and injects the submitted values as encrypted environment variables — all in one click.

**Blueprints vs stack templates:** Stack templates pre-fill stack settings (repo, branch, runner pool, etc.) and are copied as-is. Blueprints go further: they expose named input fields as a form and trigger an automatic first run on deploy. Use blueprints when the consuming team should not need to understand the underlying IaC at all.

**Real-world example:** A "Deploy a Postgres RDS instance" blueprint might expose the following params:

| Param name | Label | Type | Notes |
| --- | --- | --- | --- |
| `environment` | Environment | string | Validated against `^(dev\|staging\|prod)$` |
| `instance_class` | Instance class | string | e.g. `db.t3.micro` |
| `db_name` | Database name | string | Required |
| `allocated_storage` | Allocated storage (GB) | number | e.g. `20` |

The platform team writes the RDS Terraform module once. Any app team can deploy their own RDS instance by filling in the form — no Terraform knowledge required.

---

## Creating a blueprint

Blueprint creation is restricted to admin users.

1. Navigate to **Blueprints** in the left sidebar
2. Click **New blueprint**
3. Fill in the blueprint settings:

| Field | Description |
| --- | --- |
| Name | Display name shown in the deploy catalog |
| Description | Short explanation of what the blueprint provisions |
| Repo URL | Git repository containing the IaC module |
| Branch | Branch or tag to deploy from |
| Working directory | Path within the repo to the root module (leave blank for repo root) |
| Tool | IaC tool: `tofu`, `terraform`, `ansible`, or `pulumi` |
| Runner pool | Runner pool that will execute runs for deployed stacks |

4. Click **Save** — the blueprint is saved as a draft and is not yet visible to non-admin members

You can add and edit params immediately after saving (see [Managing params](#managing-params)).

---

## Managing params

Params are the input fields shown to app teams when they deploy the blueprint. Manage them from the blueprint detail page.

### Param fields

| Field | Required | Description |
| --- | --- | --- |
| `name` | Yes | Identifier used to construct the env var (e.g. `foo` → `TF_VAR_foo`) |
| `label` | Yes | Human-readable label shown in the deploy form |
| `description` | No | Help text shown beneath the field |
| `type` | Yes | `string`, `number`, `bool`, or `select` |
| `default` | No | Pre-filled value; for `select` type, provide comma-separated options (e.g. `dev,staging,prod`) |
| `required` | No | When checked, the form will not submit without a value |
| `validation` | No | Regex applied to the submitted value (e.g. `^(dev\|staging\|prod)$`) |
| `env_prefix` | No | Overrides the default `TF_VAR_` prefix (see [Custom env_prefix](#custom-env_prefix)) |

### Form order

Params are displayed in the deploy form in the order they appear on the blueprint detail page. Reorder them to group related inputs together.

### Validation regex

Use the `validation` field to constrain inputs and prevent runtime errors:

```
^(dev|staging|prod)$          # restrict to known environments
^[a-z][a-z0-9-]{2,30}$        # lowercase slug, 3–31 characters
^db\.(t3|t4g)\.(micro|small)$ # approved RDS instance classes only
```

Crucible evaluates the regex on the client before submission. The run is not triggered if validation fails.

### Custom env_prefix

By default, each param `foo` is injected as `TF_VAR_foo`. Set `env_prefix` to override this:

| env_prefix | Injected env var |
| --- | --- |
| *(blank)* | `TF_VAR_foo` |
| `ANSIBLE_EXTRA_VARS_` | `ANSIBLE_EXTRA_VARS_foo` |
| `APP_` | `APP_foo` |

This is useful when deploying Ansible playbooks (which read extra-vars differently from Terraform) or when mixing multiple tools in a single runner.

---

## Publishing a blueprint

A blueprint must be published before app team members can see or deploy it.

1. Open the blueprint detail page
2. Toggle the **Published** switch to on
3. The blueprint immediately appears in the deploy catalog for all org members

To take a blueprint offline without deleting it, toggle **Published** off at any time. Existing stacks that were already deployed from the blueprint are unaffected — they continue to run normally.

> **Note:** Admins can see and deploy all blueprints regardless of published status. Use draft mode to iterate on params before exposing the blueprint to the wider org.

---

## Deploying a blueprint

1. Navigate to **Blueprints** in the left sidebar
2. Find the blueprint in the catalog and click **Deploy**
3. Fill in the param form — required fields are marked and enforced before submission
4. Click **Deploy stack**

Crucible creates a new stack, stores the submitted param values as encrypted environment variables, and triggers an initial run automatically. The stack then appears in **Stacks** like any other stack — it can be edited, have policies attached, be triggered manually, and so on.

---

## How params map to Terraform

### Default mapping

Each param named `foo` is stored as `TF_VAR_foo` in the stack's encrypted environment. OpenTofu and Terraform both read `TF_VAR_*` variables automatically, so no extra wiring is needed in the provider configuration.

Declare the corresponding variable in your module:

```hcl
variable "environment" {
  description = "Deployment environment (dev, staging, prod)"
  type        = string
}

variable "instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.micro"
}

variable "db_name" {
  description = "Name of the database to create"
  type        = string
}

variable "allocated_storage" {
  description = "Allocated storage in GB"
  type        = number
}
```

The variable names must match the blueprint param `name` values exactly.

### Security

Param values are stored in the same encrypted vault as all other stack environment variables. Secret values are never exposed in the UI or run logs after the initial deployment.

### Custom env_prefix example (Ansible)

When the blueprint tool is set to `ansible`, Terraform variables are not relevant. Use `env_prefix` to inject params as a prefix your playbook actually reads:

```yaml
# playbook.yml
- hosts: all
  vars:
    db_name: "{{ lookup('env', 'ANSIBLE_EXTRA_VARS_db_name') }}"
```

Set `env_prefix = ANSIBLE_EXTRA_VARS_` on each param in the blueprint. The submitted values will be available to the playbook under `ANSIBLE_EXTRA_VARS_<name>`.

---

## Best practices

**Keep repos focused.** Use one repo per blueprint, or a mono-repo with per-blueprint subdirectories and set `working_dir` accordingly. Avoid sharing a root module between blueprints that have different param sets.

**Use validation regex.** Constrain environment names, regions, and instance classes at the form level rather than inside Terraform. Catching bad input before the run saves time and avoids partial deploys.

**Attach policies at the blueprint level.** Because every stack deployed from a blueprint shares the same underlying template, attaching policies to that template means every deployed stack inherits them automatically — no per-stack policy wiring needed.

**Version your module with git tags.** Set the blueprint's branch/tag to a specific release tag (e.g. `v1.2.0`) rather than `main`. When you cut a new version, create a new tag and update the blueprint. This prevents in-flight deployments from picking up unreviewed changes.

**Draft before publishing.** Add all params and test a deployment yourself before toggling **Published**. It is much easier to change param names and defaults before app teams have deployed stacks that depend on them.

**Use `select` type for small finite sets.** For params with a fixed set of valid values (environment, region, instance tier), prefer `select` over `string` with a regex — the dropdown prevents typos entirely and communicates valid options without needing to read the description.
