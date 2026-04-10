# Guide: Managing Proxmox VMs with Crucible IAP

This guide walks through setting up a complete GitOps workflow for Proxmox VM management using Crucible IAP and the `telmate/proxmox` Terraform provider. By the end you will have a stack that automatically plans on every push, requires manual confirmation before applying, stores state in Crucible's built-in backend, and enforces a safety policy to guard against accidental deletions.

---

## Prerequisites

- Crucible IAP running and accessible (see [operator-guide.md](../operator-guide.md))
- Proxmox VE 7+ with API access
- A Git repository (GitHub, GitLab, or Gitea) you can push to
- An Ubuntu 22.04 cloud-init template on your Proxmox node (or adjust the `clone` value to match what you have)

---

## 1. Proxmox API token

In the Proxmox web UI:

1. **Datacenter → Permissions → Users → Add**
   - Username: `crucible`, Realm: `pve`
2. **Datacenter → Permissions → API Tokens → Add**
   - User: `crucible@pve`, Token ID: `crucible`, uncheck **Privilege Separation**
   - Note the token secret UUID — it is shown once
3. **Datacenter → Permissions → Add**
   - Path: `/`, User: `crucible@pve`, Role: `PVEVMAdmin`
   - For testing you can use `Administrator`; tighten this down for production

Your token ID will be `crucible@pve!crucible`.

---

## 2. Git repository structure

Create a new repository (`homelab-proxmox` or similar) with the following files:

```
homelab-proxmox/
├── versions.tf
├── variables.tf
├── main.tf
└── outputs.tf
```

### `versions.tf`

```hcl
terraform {
  required_providers {
    proxmox = {
      source  = "telmate/proxmox"
      version = "~> 2.9"
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
  description = "Proxmox API endpoint, e.g. https://192.168.1.10:8006/api2/json"
}

variable "pm_api_token_id" {
  type        = string
  description = "API token ID, e.g. crucible@pve!crucible"
}

variable "pm_api_token_secret" {
  type        = string
  sensitive   = true
  description = "API token secret UUID"
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

variable "vm_template" {
  type        = string
  default     = "ubuntu-22.04-cloud"
  description = "Name of the cloud-init template to clone"
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
  default = "local-lvm"
}
```

### `main.tf`

```hcl
provider "proxmox" {
  pm_api_url          = var.pm_api_url
  pm_api_token_id     = var.pm_api_token_id
  pm_api_token_secret = var.pm_api_token_secret
  pm_tls_insecure     = var.pm_tls_insecure
}

resource "proxmox_vm_qemu" "test_vm" {
  name        = var.vm_name
  target_node = var.vm_node
  clone       = var.vm_template

  cores   = var.vm_cores
  memory  = var.vm_memory
  sockets = 1

  disk {
    slot    = 0
    size    = "20G"
    type    = "scsi"
    storage = var.vm_storage
  }

  network {
    model  = "virtio"
    bridge = "vmbr0"
  }

  ipconfig0 = "ip=dhcp"
  ciuser    = "ubuntu"
  sshkeys   = ""  # paste your public key here
  os_type   = "cloud-init"
  agent     = 1

  lifecycle {
    ignore_changes = [network]
  }
}
```

### `outputs.tf`

```hcl
output "vm_id" {
  value = proxmox_vm_qemu.test_vm.id
}

output "vm_ip" {
  value = proxmox_vm_qemu.test_vm.default_ipv4_address
}
```

Push this to your repository before continuing.

---

## 3. Create the stack in Crucible

**Stacks → New Stack**:

| Field | Value |
|---|---|
| Name | `homelab-proxmox` |
| Tool | `opentofu` |
| Tool version | `1.7` (leave blank for runner default) |
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

plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

# Block unexpected deletions
deny_msgs[msg] {
  r := input.resource_changes[_]
  r.change.actions[_] == "delete"
  r.type != "proxmox_vm_qemu"
  msg := "unexpected resource deletion — review before applying"
}

# Warn on large change sets
warn_msgs[msg] {
  count(input.resource_changes) > 3
  msg := sprintf("this plan changes %d resources — confirm carefully", [count(input.resource_changes)])
}
```

Back on the stack detail page → **Policies** → attach `proxmox-safety`.

---

## 5. Add environment variables

Stack detail page → **Environment Variables**:

| Key | Value | Secret |
| --- | --- | --- |
| `TF_VAR_pm_api_url` | `https://192.168.1.x:8006/api2/json` | no |
| `TF_VAR_pm_api_token_id` | `crucible@pve!crucible` | no |
| `TF_VAR_pm_api_token_secret` | your token UUID | **yes** |
| `TF_VAR_pm_tls_insecure` | `true` | no |
| `TF_VAR_vm_node` | `pve` | no |
| `TF_HTTP_ADDRESS` | `https://crucible.example.com/api/v1/state/<stack-id>` | no |
| `TF_HTTP_LOCK_ADDRESS` | same URL | no |
| `TF_HTTP_UNLOCK_ADDRESS` | same URL | no |
| `TF_HTTP_USERNAME` | `<token-id>` from stack tokens | no |
| `TF_HTTP_PASSWORD` | `<token-secret>` from stack tokens | **yes** |

The **Secret** toggle controls whether the value is masked in the UI. Secret variables (default) are shown as `••••••` in the variable list and the value field switches to a password input. Plain variables are shown as-is — useful for non-sensitive config like URLs and node names.

The `TF_HTTP_*` variables supply the state backend credentials without hardcoding them in the repository. Create a stack token first under **Stack → Tokens → New Token**.

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

```
Initializing provider plugins...
- Finding telmate/proxmox versions matching "~> 2.9"...
- Installing telmate/proxmox v2.9.14

Plan: 1 to add, 0 to change, 0 to destroy.
```

The run stays in `unconfirmed` — proposed runs never apply; they are plan-only.

### GitOps apply (tracked)

Push a commit to `main`. Crucible creates a `tracked` run automatically:

1. Status: `planning` — OpenTofu runs `plan`
2. Status: `unconfirmed` — review the plan in the UI (or approve directly from the **Dashboard**)
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

If **Auto-remediate drift** is enabled on the stack, Crucible automatically queues a tracked apply run to restore the desired state — no manual intervention needed.

---

## 8. Destroy the test VM

When you're done testing, Crucible can destroy the infrastructure through the same policy-gated, audit-logged flow as any other change.

Stack detail → **Destroy infra** → type the stack name to confirm → **Queue destroy run**.

The run lifecycle is:

1. Status: `planning` — OpenTofu runs `tofu plan -destroy` and uploads the plan
2. Status: `unconfirmed` — review the full destroy plan in the UI
3. Click **Confirm destroy** — OpenTofu applies the destroy plan
4. Status: `finished` — VM is removed from Proxmox and state is cleared

> **Note:** The `proxmox-safety` policy attached in step 4 blocks unexpected deletions of non-VM resources, but explicitly allows `delete` on `proxmox_vm_qemu`. A destroy run on this stack will pass policy evaluation.

Destroy runs always require explicit confirmation — auto-apply is never used, even if the stack has auto-apply enabled.
