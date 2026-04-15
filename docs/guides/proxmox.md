# Guide: Managing Proxmox VMs with Crucible IAP

This guide walks through setting up a complete GitOps workflow for Proxmox VM management using Crucible IAP and the `bpg/proxmox` OpenTofu provider. By the end you will have a stack that automatically plans on every push, requires manual confirmation before applying, stores state in Crucible's built-in backend, and enforces a safety policy to guard against accidental deletions.

> **Provider note:** This guide uses [`bpg/proxmox`](https://registry.terraform.io/providers/bpg/proxmox/latest) — the actively maintained community provider. The older `telmate/proxmox` provider has not had a release since early 2023, has a broken preflight permission check that fails even with Administrator role, and is not recommended for new deployments.

---

## Prerequisites

- Crucible IAP v0.3.0+ running and accessible (see [operator-guide.md](../operator-guide.md))
- Proxmox VE 7+ with API access
- A Git repository (GitHub, GitLab, or Gitea) you can push to
- An Ubuntu 24.04 cloud-init template on your Proxmox node — note its **numeric VMID** (shown in the Proxmox UI next to the template name)

---

## 1. Proxmox API token

In the Proxmox web UI:

1. **Datacenter → Permissions → Users → Add**
   - Username: `crucible`, Realm: `pve`

2. **Datacenter → Permissions → API Tokens → Add**
   - User: `crucible@pve`, Token ID: `crucible`
   - Uncheck **Privilege Separation** — the token inherits the user's permissions
   - Note the token secret UUID — it is shown once only

3. **Datacenter → Permissions → Add → User Permission**
   - Path: `/`, User: `crucible@pve`, Role: `Administrator`, Propagate: ✓

4. **Datacenter → Permissions → Add → User Permission**
   - Path: `/vms`, User: `crucible@pve`, Role: `PVEAdmin`, Propagate: ✓

   The second entry is required because Proxmox's permissions API only returns **explicitly set** paths. Even with `Administrator` propagated from `/`, the API won't include `/vms` in its response unless it is set explicitly — and `bpg/proxmox` (like most providers) queries that path directly.

Your token ID will be `crucible@pve!crucible`.

> **bpg vs telmate permission behaviour:** Unlike `telmate/proxmox`, `bpg/proxmox` does **not** run a preflight permission check at provider init time. If a permission is missing you will get an error at apply time (when the specific API call is made) rather than at plan time.

---

## 2. Git repository structure

The reference repository for this guide is [`ponack/homelab-proxmox`](https://github.com/ponack/homelab-proxmox). Create a new repository with the following files:

```
homelab-proxmox/
├── versions.tf
├── variables.tf
├── main.tf
├── outputs.tf
└── terraform.tfvars.example
```

### `versions.tf`

```hcl
terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "~> 0.101"
    }
  }

  backend "http" {}
}
```

The `backend "http" {}` block is intentionally empty — credentials are supplied via environment variables set in the Crucible stack (see step 5).

### `variables.tf`

```hcl
variable "pm_api_url" {
  type        = string
  description = "Proxmox API endpoint — host and port only, e.g. https://192.168.1.10:8006"
}

variable "pm_api_token" {
  type        = string
  sensitive   = true
  description = "API token in bpg format: \"<tokenid>=<secret>\", e.g. \"crucible@pve!crucible=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx\""
}

variable "pm_tls_insecure" {
  type        = bool
  default     = true
  description = "Set false if Proxmox has a valid TLS certificate"
}

variable "vm_name" {
  type    = string
  default = "crucible-test-vm"
}

variable "vm_node" {
  type        = string
  description = "Proxmox node name, e.g. pve"
}

variable "vm_template_id" {
  type        = number
  description = "Numeric VMID of the ubuntu-24.04 cloud-init template to clone from"
}

variable "vm_cores" {
  type    = number
  default = 2
}

variable "vm_memory" {
  type    = number
  default = 2048
}

variable "vm_storage" {
  type    = string
  default = "local"
}
```

> **Key difference from telmate:** `bpg/proxmox` takes a single `api_token` string in the format `"tokenid=secret"` rather than separate `pm_api_token_id` and `pm_api_token_secret` arguments. The `pm_api_url` is also the host/port only — do **not** append `/api2/json`.

### `main.tf`

```hcl
provider "proxmox" {
  endpoint  = var.pm_api_url
  api_token = var.pm_api_token
  insecure  = var.pm_tls_insecure
}

resource "proxmox_virtual_environment_vm" "test_vm" {
  name      = var.vm_name
  node_name = var.vm_node

  clone {
    vm_id = var.vm_template_id
    full  = true
  }

  cpu {
    cores   = var.vm_cores
    sockets = 1
  }

  memory {
    dedicated = var.vm_memory
  }

  disk {
    datastore_id = var.vm_storage
    interface    = "scsi0"
    size         = 40  # must be >= the template disk size; OpenTofu cannot shrink a cloned disk
  }

  network_device {
    model  = "virtio"
    bridge = "vmbr0"
  }

  initialization {
    datastore_id = var.vm_storage

    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }

    user_account {
      username = "ubuntu"
      keys     = []  # add SSH public keys here
    }
  }

  agent {
    # Set to true only if qemu-guest-agent is installed and running inside the template.
    # If enabled = true and the agent is absent, the provider waits indefinitely and
    # the run will not complete until Crucible's job timeout kills it (default 60 min).
    enabled = false
  }

  lifecycle {
    ignore_changes = [network_device]
  }
}
```

> **Key differences from telmate's `proxmox_vm_qemu`:**
>
> - Resource type is `proxmox_virtual_environment_vm`
> - `clone` takes a numeric `vm_id`, not a template name string
> - CPU and memory use block syntax (`cpu { cores }`, `memory { dedicated }`)
> - Network is `network_device`, disk uses `interface = "scsi0"` instead of `slot = 0`
> - Cloud-init is an `initialization` block; DHCP is `ip_config { ipv4 { address = "dhcp" } }`

### `outputs.tf`

```hcl
output "vm_id" {
  value = proxmox_virtual_environment_vm.test_vm.vm_id
}

output "vm_ip" {
  description = "First IPv4 address reported by the QEMU guest agent (empty until agent is running in the guest)"
  value       = try(proxmox_virtual_environment_vm.test_vm.ipv4_addresses[0][0], null)
}
```

### `terraform.tfvars.example`

```hcl
# Proxmox API endpoint — host:port only, no /api2/json suffix
pm_api_url = "https://192.168.1.10:8006"

# API token: combine token ID and secret into one string
# Format: "<user>@<realm>!<tokenid>=<secret-uuid>"
pm_api_token = "crucible@pve!crucible=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

pm_tls_insecure = true

vm_node        = "pve"
vm_name        = "crucible-test-vm"
vm_template_id = 9000   # numeric VMID of the ubuntu-24.04 template
vm_cores       = 2
vm_memory      = 2048
vm_storage     = "local"   # check Datacenter → Storage in Proxmox UI for the correct pool name
```

Push all files to your repository before continuing.

---

## 3. Create the stack in Crucible

**Stacks → New Stack**:

| Field | Value |
| --- | --- |
| Name | `homelab-proxmox` |
| Tool | `opentofu` |
| Tool version | leave blank (uses runner default) |
| Repo URL | your repository URL |
| Branch | `main` |
| Working directory | `/` |
| Auto-apply | off — confirm applies manually |

---

## 4. Add a safety policy

Before configuring secrets, create a policy so it can be attached immediately.

**Policies → New Policy**:

- Name: `proxmox-safety`
- Type: `post_plan`

```rego
package crucible

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

# Block unexpected deletions of non-VM resources
deny_msgs contains msg if {
  r := input.resource_changes[_]
  r.change.actions[_] == "delete"
  r.type != "proxmox_virtual_environment_vm"
  msg := "unexpected resource deletion — review before applying"
}

# Warn on large change sets
warn_msgs contains msg if {
  count(input.resource_changes) > 3
  msg := sprintf("this plan changes %d resources — confirm carefully", [count(input.resource_changes)])
}
```

Back on the stack detail page → **Policies** → attach `proxmox-safety`.

---

## 5. Add environment variables

Stack detail page → **Environment Variables**. OpenTofu reads `TF_VAR_*` environment variables as input variable values.

| Key | Value | Secret |
| --- | --- | --- |
| `TF_VAR_pm_api_url` | `https://192.168.1.x:8006` | no |
| `TF_VAR_pm_api_token` | `crucible@pve!crucible=<uuid>` | **yes** |
| `TF_VAR_pm_tls_insecure` | `true` | no |
| `TF_VAR_vm_node` | `pve` | no |
| `TF_VAR_vm_template_id` | numeric VMID of your ubuntu-24.04 template | no |

> **Note on `pm_api_url`:** the value must be the host and port only — `https://192.168.1.x:8006`. Do not append `/api2/json`; that suffix was required by telmate but `bpg/proxmox` constructs the API path internally.
>
> **Note on `pm_api_token`:** this is a single combined string — not two separate variables. Format: `"crucible@pve!crucible=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"`.

The state backend credentials (`TF_HTTP_*`) are injected automatically by the runner — you do not need to set them manually.

---

## 6. Connect the webhook

Stack detail page → copy **Webhook URL** and **Webhook Secret**.

**GitHub**: Repository → Settings → Webhooks → Add webhook

- Payload URL: paste webhook URL
- Content type: `application/json`
- Secret: paste webhook secret
- Events: **Pushes** and **Pull requests**

**GitLab**: Project → Settings → Webhooks → Add new webhook

- URL and Secret token as above
- Events: **Push events** and **Merge request events**

---

## 7. Test the run flow

### Manual plan (proposed)

Stack detail → click **Trigger proposed run**. Watch logs stream in real time. A successful plan looks like:

```text
Initializing provider plugins...
- Finding bpg/proxmox versions matching "~> 0.101"...
- Installing bpg/proxmox v0.101.1 (signed, key ID F0582AD6AE97C188)

Plan: 1 to add, 0 to change, 0 to destroy.
```

### GitOps apply (tracked)

Push a commit to `main`. Crucible creates a `tracked` run automatically:

1. Status: `planning` — OpenTofu runs `plan`
2. Status: `unconfirmed` — review the plan in the UI; buttons update automatically when the run finishes
3. Click **Confirm** — OpenTofu applies
4. Status: `finished` — VM appears in Proxmox

> **Tip:** The Dashboard shows all runs awaiting approval in one place with inline Approve/Discard buttons, so you don't need to navigate into each stack individually.

### Pull request preview (proposed)

Open a PR changing `vm_cores` from `2` to `4`. Crucible:

- Creates a `proposed` run (plan only)
- Posts a plan summary comment on the PR
- Sets a commit status check

No apply happens until the PR is merged and a tracked run completes.

### Drift detection

1. In Proxmox, manually change the VM's CPU count to `1`
2. Wait for the scheduled drift check (or stack detail → **Trigger drift check**)
3. Crucible detects the diff and surfaces it in the run output

If **Auto-remediate drift** is enabled on the stack, Crucible automatically queues a tracked apply run to restore the desired state.

---

## 8. Destroy the test VM

Stack detail → **Destroy infra** → type the stack name to confirm → **Queue destroy run**.

The run lifecycle is:

1. Status: `planning` — OpenTofu runs `tofu plan -destroy` and uploads the plan
2. Status: `unconfirmed` — review the full destroy plan in the UI
3. Click **Confirm destroy** — OpenTofu applies the destroy plan
4. Status: `finished` — VM is removed from Proxmox and state is cleared

> **Note:** The `proxmox-safety` policy blocks unexpected deletions of non-VM resources, but explicitly allows `delete` on `proxmox_virtual_environment_vm`. A destroy run on this stack will pass policy evaluation.

Destroy runs always require explicit confirmation — auto-apply is never used, even if the stack has auto-apply enabled.

---

## Troubleshooting

### "Error acquiring the state lock"

A runner container that was killed mid-operation (OOM, host restart, network drop, job timeout) may leave a lock in Crucible's database without releasing it. OpenTofu cannot acquire the lock on the next run.

**Fix:** Stack detail page → **Force unlock** button (admin only). This clears the lock row from Crucible's database. Only use this after confirming the run that held the lock has fully stopped — check `docker ps | grep crucible-run` to verify no runner containers are still alive.

If the Force unlock button reports "no lock held" but runs still fail, check for stale River retry jobs accumulating in the queue — many failed runs competing for the lock will each acquire it briefly then release it, making the table appear empty by the time you check. Clear stuck retry jobs:

```bash
docker compose exec postgres psql -U crucible -c \
  "UPDATE river_job SET state = 'discarded', finalized_at = now() \
   WHERE state = 'retryable';"
```

Then trigger a fresh Plan run.

### Run hangs for 60 minutes then fails with io-error in Proxmox

`agent { enabled = true }` in your `main.tf` causes the `bpg/proxmox` provider to wait indefinitely for the QEMU guest agent to respond after VM creation. If your template does not have `qemu-guest-agent` installed and running, this wait never completes.

**Fix:** Set `agent { enabled = false }` unless your template has the agent installed. To add the agent to your template:

```bash
apt-get install -y qemu-guest-agent
systemctl enable --now qemu-guest-agent
```

Then shut down the template VM and convert it back to a template in the Proxmox UI before re-enabling the agent in your OpenTofu config.

### "disk resize failure: requested size is lower than current size"

OpenTofu cannot shrink a cloned disk. The `size` value in the `disk` block must be **greater than or equal to** the template's disk size.

**Fix:** Check the template's disk size in Proxmox UI (select the template → Hardware tab → Hard Disk) and set `size` in `main.tf` to at least that value.

### Wrong storage pool name

Proxmox storage pool names vary by installation. Common names are `local` (directory storage), `local-lvm` (LVM-thin), and `local-zfs` (ZFS). If the pool name in `vm_storage` doesn't match an existing pool, the apply will fail with `storage '...' does not exist`.

**Fix:** Check **Datacenter → Storage** in the Proxmox UI for the correct pool name and update the `TF_VAR_vm_storage` env var on the Crucible stack (or the default in `variables.tf`).
