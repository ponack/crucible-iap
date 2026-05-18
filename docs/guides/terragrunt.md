# Guide: Terragrunt with Crucible IAP

This guide walks through setting up a GitOps workflow for Terragrunt using Crucible IAP. By the end you will have a Terragrunt root configuration that drives multiple modules through Crucible's plan → confirm → apply lifecycle, with state stored in Crucible's built-in backend.

The example uses the `random` provider so no cloud credentials are required. The patterns here apply equally to multi-environment live repositories managing real infrastructure.

---

## How Terragrunt maps to Crucible's lifecycle

| Concept | OpenTofu | Terragrunt |
| --- | --- | --- |
| Plan phase | `tofu plan` | `terragrunt run-all plan` |
| Apply phase | `tofu apply` | `terragrunt run-all apply` |
| Destroy | `tofu plan -destroy` → `tofu apply` | `terragrunt run-all destroy` |
| State | Crucible HTTP backend (injected via override file) | Crucible HTTP backend (injected via `TF_HTTP_*` env vars) |
| Change summary | structured plan JSON | aggregated across all modules |

Crucible captures the full `run-all plan` output as the plan artifact. The change summary (add / change / destroy counts) is summed across all modules in the tree and displayed in the run header and PR comments.

All runs use `--terragrunt-non-interactive` to prevent Crucible from hanging on interactive prompts.

---

## Prerequisites

- Crucible IAP v0.8.27+ running (see [operator-guide.md](../operator-guide.md))
- Terragrunt installed locally for writing and testing configurations
- A Git repository (GitHub, GitLab, Gitea, Bitbucket, or Azure DevOps)

---

## 1. Create the repository

### Option A — Use the template (recommended)

Go to [github.com/ponack/crucible-quickstart-terragrunt](https://github.com/ponack/crucible-quickstart-terragrunt) and click **Use this template → Create a new repository**. The root `terragrunt.hcl` and both child modules are already in place — skip ahead to step 2.

### Option B — Write the files yourself

Create a new repository and add the following structure:

```text
crucible-terragrunt-demo/
├── terragrunt.hcl          ← root: state backend + shared inputs
├── name/
│   ├── terragrunt.hcl      ← child: include root, optional overrides
│   └── main.tf
└── port/
    ├── terragrunt.hcl
    └── main.tf
```

### Root `terragrunt.hcl`

This configures the state backend for all child modules. Crucible injects `TF_HTTP_*` environment variables at run time; this block reads them so Terragrunt can generate a `backend.tf` in each child module automatically:

```hcl
remote_state {
  backend = "http"
  config = {
    address        = get_env("TF_HTTP_ADDRESS", "")
    lock_address   = get_env("TF_HTTP_ADDRESS", "")
    unlock_address = get_env("TF_HTTP_ADDRESS", "")
    username       = get_env("TF_HTTP_USERNAME", "")
    password       = get_env("TF_HTTP_PASSWORD", "")
  }
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
}
```

> **Local development:** When you run `terragrunt` on your local machine the `TF_HTTP_*` variables are not set, so `get_env` returns the empty string defaults. Use `--terragrunt-ignore-external-dependencies` or configure a local backend override for local testing.

### `name/main.tf`

```hcl
terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

resource "random_pet" "name" {
  length    = 2
  separator = "-"
}

output "service_name" {
  value = random_pet.name.id
}
```

### `name/terragrunt.hcl`

```hcl
include "root" {
  path = find_in_parent_folders()
}
```

### `port/main.tf`

```hcl
terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

resource "random_integer" "port" {
  min = 1024
  max = 9999
}

output "service_port" {
  value = random_integer.port.result
}
```

### `port/terragrunt.hcl`

```hcl
include "root" {
  path = find_in_parent_folders()
}
```

Commit everything to the `main` branch.

---

## 2. Create the stack in Crucible

**Stacks → New Stack**:

| Field | Value |
| --- | --- |
| Name | `terragrunt-demo` |
| Tool | `terragrunt` |
| Tool version | leave blank (uses `0.72.1`) or set a specific version (e.g. `0.73.0`) |
| Repo URL | your repository URL |
| Branch | `main` |
| Working directory | `/` |
| Auto-apply | off |

The **Tool version** field controls which Terragrunt binary the runner downloads from GitHub releases. Leave it blank to use the runner default; set it to any published Terragrunt release version to pin.

---

## 3. Trigger your first run

Stack detail → **Trigger tracked run**.

The runner downloads the Terragrunt binary, then executes `terragrunt run-all plan`. Because this is the first run, both modules plan resource creation:

```text
Group 1
- Module /workspace/name
- Module /workspace/port

Module /workspace/name
  + random_pet.name

Module /workspace/port
  + random_integer.port

Plan: 2 to add, 0 to change, 0 to destroy.
```

Status moves to `unconfirmed`. Review the plan, then click **Confirm**. The runner executes `terragrunt run-all apply` and both modules are created. State for each module is stored independently in Crucible's backend under the stack's state path.

---

## 4. Connect the webhook (optional)

Stack detail page → copy the **Webhook URL** and **Webhook Secret**.

**GitHub**: Repository → Settings → Webhooks → Add webhook
- Content type: `application/json`
- Events: **Pushes** and **Pull requests**

Push a change to any module — for example, change `separator` in `name/main.tf` from `"-"` to `"_"` — and Crucible plans it automatically.

---

## Using an external state backend

To manage state outside Crucible (AWS S3, GCS, Azure Blob, etc.), configure your `remote_state` block normally in `terragrunt.hcl`:

```hcl
remote_state {
  backend = "s3"
  config = {
    bucket  = "my-terraform-state"
    key     = "${path_relative_to_include()}/terraform.tfstate"
    region  = "us-east-1"
    encrypt = true
  }
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
}
```

Add your cloud credentials as secret environment variables on the stack. Crucible does not interfere with backend configuration when `remote_state` is provided — the `TF_HTTP_*` injection is a no-op because Terragrunt's generated `backend.tf` takes precedence.

---

## Version pinning

Set the **Tool version** field on the stack to pin a specific Terragrunt version:

| Tool version | Behavior |
| --- | --- |
| *(blank)* | Terragrunt `0.72.1` (runner default) |
| `0.73.0` | Downloads exactly this version from GitHub releases |
| `0.68.21` | Any published release version is valid |

You can also set `CRUCIBLE_TOOL_VERSION` as a non-secret environment variable on the stack — the **Tool version** stack field and the env var are equivalent. The stack field is easier to see at a glance.

The binary is cached for the lifetime of the runner container. Multiple runs within the same container do not re-download.

---

## Multi-environment layout

A common Terragrunt pattern separates environments using directory structure:

```text
live/
├── terragrunt.hcl           ← shared remote_state + common inputs
├── dev/
│   ├── vpc/
│   │   └── terragrunt.hcl
│   └── app/
│       └── terragrunt.hcl
└── prod/
    ├── vpc/
    │   └── terragrunt.hcl
    └── app/
        └── terragrunt.hcl
```

Create a **separate Crucible stack per environment** and set the **Working directory** accordingly:

| Stack | Working directory |
| --- | --- |
| `infra-dev` | `/live/dev` |
| `infra-prod` | `/live/prod` |

Each stack gets its own state path in Crucible's backend. `run-all` plans and applies only the modules under the configured working directory, so `dev` and `prod` runs are isolated.

---

## Module dependencies (`dependency` blocks)

Terragrunt `dependency` blocks work as expected. Crucible runs `run-all` which respects the dependency graph — modules are planned and applied in dependency order:

```hcl
# live/dev/app/terragrunt.hcl
dependency "vpc" {
  config_path = "../vpc"
}

inputs = {
  vpc_id = dependency.vpc.outputs.vpc_id
}
```

> **Dependency outputs during plan:** Terragrunt fetches dependency outputs before planning. If the dependency module has never been applied, `run-all plan` will either fail or use mock outputs. Add `mock_outputs` to dependency blocks for graceful first-run behaviour:
>
> ```hcl
> dependency "vpc" {
>   config_path = "../vpc"
>   mock_outputs = {
>     vpc_id = "vpc-00000000"
>   }
>   mock_outputs_allowed_terraform_commands = ["plan"]
> }
> ```

---

## Infracost cost estimates

Infracost cost estimates work with Terragrunt the same way as with OpenTofu — configure your API key in **Settings → Integrations** and cost deltas appear on every plan. The runner runs `infracost breakdown` against the combined plan output after `run-all plan` completes.

Resource types not covered by Infracost's pricing database (`null_resource`, `local_file`, etc.) show `$0` in the estimate.

---

## Troubleshooting

### "Error: No root terragrunt.hcl found"

Terragrunt searches for `terragrunt.hcl` in the working directory and all parent directories. Verify that:
1. The root `terragrunt.hcl` exists in the repository
2. The **Working directory** on the stack matches where Terragrunt should start (`/` for the repo root, `/live/dev` for an environment subdirectory)

### State backend connection errors on first run

The runner injects `TF_HTTP_*` variables but the `remote_state` block is not reading them. Check:
1. Your root `terragrunt.hcl` uses `get_env("TF_HTTP_ADDRESS", "")` as shown in this guide
2. The `generate` block uses `if_exists = "overwrite_terragrunt"` so it does not conflict with any existing `backend.tf`
3. No child module has its own hard-coded `backend` block that would override the generated one

### "Error: state file locked" after a failed run

If a runner container is killed mid-run, the state lock may not be released. Navigate to the stack detail → **State** tab → **Force unlock** to release it.

### Module ordering is not what I expected

Terragrunt infers execution order from `dependency` blocks. If a module is running before its dependencies, check that the `dependency` path is correct and that Terragrunt can resolve outputs. Use `terragrunt graph-dependencies` locally to visualise the order.

### Terragrunt version mismatch between local and CI

Pin the same version locally and in Crucible. Set **Tool version** on the stack to the same version you use locally (check with `terragrunt --version`). Version drift is a common source of plan differences.
