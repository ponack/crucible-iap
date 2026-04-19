# Remote State Sharing Guide

Crucible supports cross-stack `terraform_remote_state` data sources without sharing long-lived credentials. Each cross-stack read uses a dedicated, scoped token that can be revoked independently.

## Contents

1. [How it works](#how-it-works)
2. [Setting up a remote state source](#setting-up-a-remote-state-source)
3. [Using the shared state in OpenTofu](#using-the-shared-state-in-opentofu)
4. [Env var reference](#env-var-reference)
5. [Revoking access](#revoking-access)
6. [Troubleshooting](#troubleshooting)

---

## How it works

When you add a remote state source on a stack (the **consumer**), Crucible:

1. Mints a dedicated token pair (ID + secret) on the **source** stack's state backend
2. Encrypts the token secret using the source stack's HKDF-derived vault key
3. Stores the encrypted secret in `stack_remote_state_sources`

At run time, the worker decrypts the secret and injects three environment variables into the consumer stack's runner container:

```
CRUCIBLE_REMOTE_STATE_<SLUG>_ADDRESS
CRUCIBLE_REMOTE_STATE_<SLUG>_USERNAME
CRUCIBLE_REMOTE_STATE_<SLUG>_PASSWORD
```

`<SLUG>` is the source stack's slug, uppercased with hyphens replaced by underscores. For a source stack with slug `networking-prod`, the variables would be:

```
CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_ADDRESS
CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_USERNAME
CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_PASSWORD
```

The consumer stack's runner uses these to configure a `terraform_remote_state` data source. The token has read-only access to the source stack's state — it cannot write state or trigger runs.

---

## Setting up a remote state source

Open the **consumer** stack (the stack that needs to read another stack's state):

1. Scroll to **Remote state sources**
2. Click **Add source**
3. Select the source stack from the dropdown
4. Click **Add**

Crucible provisions the token immediately. The relationship appears in the **Remote state sources** list, showing the source stack name and slug.

No configuration is needed on the source stack — the token is minted and scoped automatically.

---

## Using the shared state in OpenTofu

In the consumer stack's Terraform code, use the injected env vars to configure the `terraform_remote_state` data source:

```hcl
data "terraform_remote_state" "networking" {
  backend = "http"

  config = {
    address  = var.networking_state_address
    username = var.networking_state_username
    password = var.networking_state_password
  }
}
```

Then pass the env vars via `TF_VAR_*`:

```hcl
variable "networking_state_address"  {}
variable "networking_state_username" {}
variable "networking_state_password" {}
```

OpenTofu reads `TF_VAR_*` variables automatically. Since Crucible injects `CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_*`, add the following environment variables to the consumer stack in the UI:

| Stack env var | Value |
| --- | --- |
| `TF_VAR_networking_state_address` | `${CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_ADDRESS}` |
| `TF_VAR_networking_state_username` | `${CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_USERNAME}` |
| `TF_VAR_networking_state_password` | `${CRUCIBLE_REMOTE_STATE_NETWORKING_PROD_PASSWORD}` |

These values are expanded at run time inside the runner container — the literal `${...}` syntax is resolved by bash before OpenTofu reads them.

### Accessing outputs from the shared state

Once the data source is configured, reference the source stack's outputs normally:

```hcl
locals {
  vpc_id     = data.terraform_remote_state.networking.outputs.vpc_id
  subnet_ids = data.terraform_remote_state.networking.outputs.private_subnet_ids
}

resource "aws_instance" "web" {
  subnet_id = local.subnet_ids[0]
  # ...
}
```

The source stack must export the values you need as outputs in its root module:

```hcl
# In the networking stack
output "vpc_id" {
  value = aws_vpc.main.id
}

output "private_subnet_ids" {
  value = aws_subnet.private[*].id
}
```

---

## Env var reference

| Variable | Description |
| --- | --- |
| `CRUCIBLE_REMOTE_STATE_<SLUG>_ADDRESS` | HTTP state backend URL for the source stack (`<base_url>/api/v1/state/<stack-id>`) |
| `CRUCIBLE_REMOTE_STATE_<SLUG>_USERNAME` | Token ID (used as HTTP Basic auth username) |
| `CRUCIBLE_REMOTE_STATE_<SLUG>_PASSWORD` | Token secret (used as HTTP Basic auth password) |

`<SLUG>` is the source stack slug uppercased with `-` replaced by `_`. For slug `my-network-stack` the prefix is `MY_NETWORK_STACK`.

---

## Revoking access

To remove a consumer stack's access to a source stack's state:

1. Open the consumer stack
2. Scroll to **Remote state sources**
3. Click **Remove** next to the source stack

This immediately deletes the dedicated token from the source stack's state backend. Any future run on the consumer stack that tries to read that remote state will receive a 401 and fail.

Runs already in progress are not affected — they hold the decrypted credentials in memory for the duration of the run.

---

## Troubleshooting

### "401 Unauthorized" reading remote state

The token has been revoked or the source stack was deleted. Remove the remote state source from the consumer stack and re-add it to provision a fresh token.

### Remote state outputs are stale

The `terraform_remote_state` data source reads state at plan time. If the source stack applied a change after the consumer stack started planning, the consumer will see the old state until its next run.

To ensure the consumer always sees fresh state, set up a [stack dependency](stack-dependencies.md) so the source stack triggers the consumer automatically after each apply.

### Variables not expanding

If OpenTofu reports that `TF_VAR_networking_state_address` is empty, verify:
1. The remote state source is configured on the consumer stack in the UI
2. The stack env var value uses `${CRUCIBLE_REMOTE_STATE_...}` syntax (not hardcoded)
3. The SLUG matches the source stack slug exactly (check the stack detail page for the slug)

Check the runner logs for the actual env vars injected at run time — look for `CRUCIBLE_REMOTE_STATE_` in the log output at the start of the run.
