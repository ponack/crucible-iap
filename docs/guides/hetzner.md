# Guide: Hetzner Cloud with Crucible IAP

Hetzner Cloud is a popular choice for cost-conscious teams and homelab builders — significantly cheaper per-vCPU and per-GB than the hyperscalers. This guide covers Hetzner Cloud (the API-driven cloud) plus brief notes on Hetzner Robot (the dedicated server line).

By the end you'll have an OpenTofu stack managing a Hetzner Cloud server through Crucible.

---

## Hetzner Cloud vs Hetzner Robot

Hetzner runs two distinct products with separate APIs:

| Product | API | Provider | Use for |
| --- | --- | --- | --- |
| Hetzner **Cloud** | `console.hetzner.cloud` | `hetznercloud/hcloud` | Cloud VMs, volumes, networks, load balancers, managed Kubernetes |
| Hetzner **Robot** | `robot.hetzner.com` | `hetznercloud/hetznerdns` for DNS, `kreuzwerker/hrobot` for servers | Dedicated bare-metal servers |

Most teams start with Cloud — it's API-first and behaves like every other modern cloud. Robot is for dedicated hardware (e.g. 64-core AX servers) and uses a username/password API.

This guide focuses on Cloud. Brief Robot notes at the end.

---

## Prerequisites

- Crucible IAP running (see [quickstart.md](../quickstart.md))
- A Hetzner Cloud project (free to create)
- A Git repository

---

## Step 1 — Generate a Cloud API token

1. Hetzner Cloud Console: select your project → **Security → API tokens → Generate API token**.
2. Description: `crucible-iap`.
3. Permissions: **Read & Write** (you can scope tighter later).
4. Copy the token — shown once.

---

## Step 2 — Add the token to Crucible

Stack detail → **Settings → Environment variables → Add**:

| Name | Value | Secret |
| --- | --- | --- |
| `HCLOUD_TOKEN` | the token from Step 1 | yes |

The hcloud provider reads `HCLOUD_TOKEN` automatically.

For multi-stack reuse, put the token in a Variable Set ([variable-sets.md](variable-sets.md)) and attach to each stack.

---

## Step 3 — Write a first stack

**`versions.tf`**

```hcl
terraform {
  required_version = ">= 1.6.0"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.48"
    }
  }

  backend "http" {}
}
```

**`main.tf`**

```hcl
resource "hcloud_ssh_key" "default" {
  name       = "crucible-deploy"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "hcloud_server" "web" {
  name        = "web-01"
  image       = "ubuntu-24.04"
  server_type = "cx22"          # 2 vCPU, 4 GB RAM — cheapest Ampere ARM
  location    = "fsn1"           # Falkenstein, Germany
  ssh_keys    = [hcloud_ssh_key.default.id]

  labels = {
    managed_by = "crucible"
  }
}

output "server_ip" {
  value = hcloud_server.web.ipv4_address
}
```

Push to `main`.

> **SSH key gotcha:** The `file()` call above reads from your *local* filesystem when you run `terraform plan` on your laptop. Crucible runs the plan inside an ephemeral container that does **not** have your SSH key. For real use, paste the public key as a string variable or fetch it from a secret store.

For a runner-safe version, pass the public key as a `TF_VAR_*`:

```hcl
variable "ssh_public_key" {
  type      = string
  sensitive = true
}

resource "hcloud_ssh_key" "default" {
  name       = "crucible-deploy"
  public_key = var.ssh_public_key
}
```

Then set `TF_VAR_ssh_public_key` as a (non-secret) env var on the stack — `ssh-ed25519 AAAA...`.

---

## Step 4 — Create the Crucible stack

**Stacks → New Stack**:

| Field | Value |
| --- | --- |
| Name | `hcloud-demo` |
| Tool | `opentofu` |
| Repo URL | your repository URL |
| Branch | `main` |
| Working directory | `/` |
| Auto-apply | off |

Trigger a tracked run. Plan → confirm → apply. The server boots in ~10 seconds.

---

## Common Hetzner Cloud resources

| Resource | Provider type | Notes |
| --- | --- | --- |
| Server (VM) | `hcloud_server` | x86 `cx*` line or ARM `cax*` line; ARM is cheaper |
| Network | `hcloud_network` + `hcloud_network_subnet` | Private networking between servers |
| Floating IP | `hcloud_floating_ip` | Movable public IPs |
| Load Balancer | `hcloud_load_balancer` | TCP / HTTP / HTTPS |
| Volume | `hcloud_volume` | Network-attached block storage |
| Firewall | `hcloud_firewall` | Stateful packet filtering |
| Managed Kubernetes | `hcloud_kubernetes_cluster` | Beta as of 2026 |
| Snapshot / Image | `hcloud_snapshot` | Custom images |

Provider reference: [registry.terraform.io/providers/hetznercloud/hcloud](https://registry.terraform.io/providers/hetznercloud/hcloud/latest/docs).

---

## Recommended OPA policies

### Cap server size

Hetzner's largest cloud servers (`cx52`, `cax41`) cost ~€100/month — useful to gate behind approval.

```rego
package crucible

require_approval[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "hcloud_server"
  st := resource.change.after.server_type
  large := {"cx52", "cax41", "ccx53", "ccx63"}
  large[st]
  msg := sprintf("server_type %q requires approval — please tag a reviewer", [st])
}
```

### Require labels

```rego
package crucible

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "hcloud_server"
  labels := resource.change.after.labels
  not labels.managed_by
  msg := sprintf("server %q must have a 'managed_by' label", [resource.address])
}
```

### Restrict to EU datacentres

```rego
package crucible

eu_locations := {"fsn1", "nbg1", "hel1"}

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "hcloud_server"
  loc := resource.change.after.location
  not eu_locations[loc]
  msg := sprintf("location %q outside EU; allowed: %v", [loc, eu_locations])
}
```

Attach via **Policies → Attach to stack**.

---

## State backend options

The built-in Crucible HTTP backend is the simplest path. If you specifically want state on Hetzner infrastructure:

- **Hetzner Object Storage** (S3-compatible, beta in some regions) can host state with `backend "s3"`.
- **A self-hosted MinIO/Garage** on a Hetzner server gives full control.

For most setups, the built-in backend is fine.

---

## Hetzner Robot (dedicated servers)

If you're managing AX/EX dedicated servers via Robot:

```hcl
terraform {
  required_providers {
    hrobot = {
      source  = "kreuzwerker/hrobot"
      version = "~> 0.3"
    }
  }
}

provider "hrobot" {
  username = var.hrobot_username
  password = var.hrobot_password
}
```

Robot's API is username/password (Basic auth). Set both as **Secret** env vars on the stack.

The Robot provider's resource coverage is narrower than hcloud — most teams use it for boot configuration, rDNS, and SSH key management; OS installation and partitioning are still done via Hetzner's web installimage interface.

---

## Troubleshooting

### "Error: invalid token"

The `HCLOUD_TOKEN` is wrong, was rotated, or was created in a different project than the resources you're managing. Tokens are project-scoped.

### "Error: server_type 'cx22' not available in location 'hel1'"

Not all server types are available in every datacentre. Check the Hetzner Cloud Console's pricing page for current availability. ARM (`cax*`) types are only in Falkenstein and Helsinki as of 2026.

### "Error: rate limit exceeded"

Hetzner's API rate limit is 3,600 requests/hour per project. Large `terraform plan` operations on big stacks can hit this. Mitigations: split the stack, add `parallelism = 5` to your provider config, or contact Hetzner to raise the limit.

---

## What's next

- [policies.md](../policies.md) — write Rego specific to your Hetzner usage.
- [variable-sets.md](variable-sets.md) — share the token across multiple Hetzner stacks.
- [ansible.md](ansible.md) — pair Hetzner provisioning with Ansible configuration.
- [proxmox.md](proxmox.md) — Hetzner is a popular host for running Proxmox; manage VMs inside the Proxmox node with Crucible.
