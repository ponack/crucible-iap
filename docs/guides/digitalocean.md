# Guide: DigitalOcean with Crucible IAP

DigitalOcean is one of the friendliest entry points to cloud-managed Infrastructure as Code — flat pricing, simple resource model, no IAM maze. This guide walks through running OpenTofu against DigitalOcean from a Crucible stack.

You'll set up authentication, create your first Droplet through a Crucible stack, and learn the recommended state-backend options.

---

## Prerequisites

- Crucible IAP running (see [quickstart.md](../quickstart.md))
- A DigitalOcean account with billing enabled
- A Git repository to hold your `.tf` files

---

## Step 1 — Generate a DigitalOcean API token

1. In the DigitalOcean console: **API → Generate New Token**.
2. Name: `crucible-iap`.
3. Scopes: select **Full Access** for a starter setup, or scope per-resource if you have multiple Crucible stacks managing different resource families.
4. Copy the token — it is shown once.

---

## Step 2 — Add the token to Crucible

You have three options:

### Option A — Per-stack environment variable (simplest)

Stack detail → **Settings → Environment variables → Add**.

| Name | Value | Secret |
| --- | --- | --- |
| `DIGITALOCEAN_TOKEN` | the token from Step 1 | yes |

The DigitalOcean provider reads `DIGITALOCEAN_TOKEN` automatically — no further wiring.

### Option B — Variable Set (multiple stacks share the token)

If you'll have several DO stacks (dev / staging / prod, or one per service), put the token in a Variable Set and attach it to each stack. See [variable-sets.md](variable-sets.md).

### Option C — External secret store (token lives in Vault / Bitwarden SM / AWS SM)

If you already keep secrets in an external store, configure a secret-store integration and inject `DIGITALOCEAN_TOKEN` at run time. See [external-secrets.md](external-secrets.md).

---

## Step 3 — Write a first stack

Create a new repo (or subdirectory) with `versions.tf` and `main.tf`:

**`versions.tf`**

```hcl
terraform {
  required_version = ">= 1.6.0"

  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.40"
    }
  }

  backend "http" {}
}
```

The empty `backend "http" {}` is filled in by Crucible at run time — state is stored in Crucible's built-in HTTP backend.

**`main.tf`**

```hcl
resource "digitalocean_droplet" "web" {
  image  = "ubuntu-24-04-x64"
  name   = "web-01"
  region = "ams3"
  size   = "s-1vcpu-1gb"

  tags = ["crucible-managed"]
}

output "droplet_ip" {
  value = digitalocean_droplet.web.ipv4_address
}
```

Push to the `main` branch.

---

## Step 4 — Create the Crucible stack

**Stacks → New Stack**:

| Field | Value |
| --- | --- |
| Name | `do-droplet-demo` |
| Tool | `opentofu` |
| Repo URL | your repository URL |
| Branch | `main` |
| Working directory | `/` |
| Auto-apply | off |

Trigger a **tracked run**. Plan → confirm → apply. The Droplet appears in your DO console within ~30 seconds.

---

## State backend options

### Crucible built-in (recommended for getting started)

The default. No extra setup. State is encrypted and stored on Crucible's storage; access is gated by stack token.

### DigitalOcean Spaces (S3-compatible)

If you want state on DO infrastructure, use a Spaces bucket as an S3-compatible backend:

```hcl
terraform {
  backend "s3" {
    bucket                      = "my-tfstate"
    key                         = "stacks/web.tfstate"
    region                      = "us-east-1"        # required but ignored by Spaces
    endpoint                    = "ams3.digitaloceanspaces.com"
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
  }
}
```

Set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` env vars to your Spaces access keys.

For a production setup with state locking, pair Spaces with a managed PostgreSQL instance and use the `pg` backend instead.

---

## Recommended OPA policies for DO stacks

DigitalOcean's flat pricing makes cost-control policies straightforward:

### Block expensive Droplet sizes

```rego
package crucible

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "digitalocean_droplet"
  size := resource.change.after.size
  expensive := {"c-32", "c-48", "gd-32-intel", "gd-48-intel", "m-16x"}
  expensive[size]
  msg := sprintf("droplet size %q exceeds policy — open a ticket for approval", [size])
}
```

### Require the `crucible-managed` tag on all resources

```rego
package crucible

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "digitalocean_droplet"
  tags := resource.change.after.tags
  not "crucible-managed" in tags
  msg := sprintf("droplet %q must include the 'crucible-managed' tag", [resource.address])
}
```

### Restrict regions

```rego
package crucible

allowed_regions := {"ams3", "fra1", "lon1"}

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "digitalocean_droplet"
  region := resource.change.after.region
  not allowed_regions[region]
  msg := sprintf("region %q not allowed; permitted: %v", [region, allowed_regions])
}
```

Attach these policies to your DO stacks via **Policies → Attach to stack**.

---

## Common DigitalOcean resource patterns

| Resource | Provider type | Notes |
| --- | --- | --- |
| Droplet (VM) | `digitalocean_droplet` | The basic compute unit |
| Managed Kubernetes | `digitalocean_kubernetes_cluster` | Pair with `kubernetes` provider — see [kubernetes.md](kubernetes.md) |
| Managed database | `digitalocean_database_cluster` | PostgreSQL, MySQL, Redis, MongoDB |
| Spaces (S3-compatible) | `digitalocean_spaces_bucket` | Object storage |
| DNS | `digitalocean_domain` + `digitalocean_record` | Manage zones declaratively |
| Load Balancer | `digitalocean_loadbalancer` | TCP/HTTP/HTTPS LBs |
| Project | `digitalocean_project` | Group resources for billing visibility |

A complete provider reference: [registry.terraform.io/providers/digitalocean/digitalocean](https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs).

---

## Troubleshooting

### "Error: 401 Unauthorized" on plan

Your `DIGITALOCEAN_TOKEN` is missing or invalid. Re-generate in the DO console and update the stack env var. Confirm the env var is marked **Secret** so its value is preserved across UI saves.

### "Error: 422 Unprocessable Entity" creating a Droplet

The combination of `image`, `region`, and `size` isn't valid. Common causes:

- Image not available in the target region (newer images roll out regionally).
- Size too small for the image (Ubuntu 24.04 needs at least `s-1vcpu-1gb`).
- SSH key ID doesn't exist in the account.

### Destroyed Droplet still appears in DO console for a few minutes

DigitalOcean's API confirms destroy immediately but UI reflects it on the next refresh cycle. Not a Crucible bug.

---

## What's next

- [Policies](../policies.md) — write Rego policies tailored to your DO usage.
- [variable-sets.md](variable-sets.md) — if you'll have many DO stacks, share the token across them.
- [drift-detection.md](drift-detection.md) — schedule periodic `plan` to detect changes made in the DO console.
- [kubernetes.md](kubernetes.md) — pair DO Kubernetes with in-cluster manifests.
