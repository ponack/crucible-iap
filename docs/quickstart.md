# Quick Start — Your First Stack in 15 Minutes

This guide gets you from zero to a working GitOps run on your local machine. No cloud credentials needed — the example stack uses OpenTofu's `random` provider so you can focus on the workflow.

By the end you will have:
- Crucible running locally
- A Git repository connected to a stack
- A plan that requires confirmation before it applies
- State stored in Crucible's built-in backend

When you're ready to manage real infrastructure, see the tool guides:
- [docs/guides/proxmox.md](guides/proxmox.md) — Proxmox VMs
- [docs/guides/ansible.md](guides/ansible.md) — Ansible playbooks

---

## Prerequisites

- Docker Engine 24+ and Docker Compose v2.20+
- Git
- A GitHub, GitLab, or Gitea account (for webhook auto-triggering — or use manual runs, which need no webhook at all)

---

## Step 1 — Deploy Crucible

```bash
git clone https://github.com/ponack/crucible-iap.git
cd crucible-iap
cp .env.example .env
```

Open `.env` and set these four values — everything else can stay as-is for local use:

```env
CRUCIBLE_BASE_URL=http://localhost:8080
CRUCIBLE_SECRET_KEY=<paste output of: openssl rand -hex 32>
POSTGRES_PASSWORD=<any strong password>
MINIO_SECRET_KEY=<any strong password>
```

Enable local auth so you can log in without an IdP:

```env
LOCAL_AUTH_ENABLED=true
LOCAL_AUTH_EMAIL=admin@example.com
LOCAL_AUTH_PASSWORD=<any strong password>
```

Create the runner network and start everything:

```bash
docker network create crucible-runner
docker compose up -d
```

Wait about 20 seconds for migrations to complete, then check:

```bash
docker compose ps
curl -s http://localhost:8080/health | grep '"status":"ok"'
```

The UI is at **http://localhost:3000**. Open it and log in with the email and password you set above.

> **First-run note:** On the very first start, Crucible creates the MinIO buckets and runs all database migrations automatically. The `crucible-api` container will restart once while it waits for PostgreSQL to become healthy — this is normal.

---

## Step 2 — Create a test repository

Create a new repository on GitHub (or GitLab/Gitea) named `crucible-quickstart`. Add two files:

### `versions.tf`

```hcl
terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  backend "http" {}
}
```

The `backend "http" {}` block is intentionally empty — Crucible injects the state backend credentials automatically when the runner starts.

### `main.tf`

```hcl
resource "random_pet" "name" {
  length    = 2
  separator = "-"
}

resource "random_integer" "port" {
  min = 1024
  max = 9999
}

output "service_name" {
  value = random_pet.name.id
}

output "service_port" {
  value = random_integer.port.result
}
```

Push both files to the `main` branch.

---

## Step 3 — Create the stack

In the Crucible UI:

1. **Stacks → New Stack**

   | Field | Value |
   | --- | --- |
   | Name | `quickstart` |
   | Tool | `opentofu` |
   | Repo URL | your repository URL (HTTPS) |
   | Branch | `main` |
   | Working directory | `/` |
   | Auto-apply | leave off |

2. Click **Create stack**.

---

## Step 4 — Trigger your first run

No webhook needed yet — trigger a run manually.

Stack detail page → **Trigger proposed run**.

The run appears in the run list with status `planning`. Click it to watch logs stream in real time:

```text
Initializing provider plugins...
- Finding hashicorp/random versions matching "~> 3.6"...
- Installing hashicorp/random v3.6.3 (signed, key ID 34365D9472D7468F)

Terraform used the selected providers to generate the following execution plan.

Plan: 2 to add, 0 to change, 0 to destroy.

Changes to Outputs:
  + service_name = (known after apply)
  + service_port = (known after apply)
```

Status moves to `unconfirmed`. This is a `proposed` run — plan-only, no confirmation needed. The plan artifact is stored and viewable in the **Plan** tab.

---

## Step 5 — Apply with confirmation

Now trigger a **tracked** run — this one goes through the full plan → confirm → apply lifecycle.

Stack detail → **Trigger tracked run**.

1. Status: `planning` — OpenTofu plans
2. Status: `unconfirmed` — review the plan in the UI
3. Click **Confirm** — OpenTofu applies
4. Status: `finished` — outputs appear in the **Outputs** tab:

```text
service_name = "happy-lemur"
service_port = 7341
```

State is now stored in Crucible's built-in backend. Click **State** on the stack detail page to view the full state file.

---

## Step 6 — Connect the webhook (optional)

To trigger runs automatically on every push and get PR plan comments, connect a webhook.

Stack detail page → copy the **Webhook URL** and **Webhook Secret**.

**GitHub**: Repository → Settings → Webhooks → Add webhook
- Payload URL: paste webhook URL
- Content type: `application/json`
- Secret: paste webhook secret
- Events: **Pushes** and **Pull requests**

**GitLab**: Project → Settings → Webhooks → Add new webhook
- URL and Secret token as above
- Events: **Push events** and **Merge request events**

Now push a change to `main.tf` — for example, change `separator` from `"-"` to `"_"` — and Crucible will automatically plan the change.

---

## Step 7 — Add a policy

Policies evaluate the plan output before a run can be confirmed. Add a simple guard that warns when both resources change at once.

**Policies → New Policy**:
- Name: `quickstart-guard`
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

deny_msgs := []

warn_msgs contains msg if {
  input.plan_summary.add > 1
  msg := sprintf("plan adds %d resources — confirm this is intentional", [input.plan_summary.add])
}
```

Stack detail → **Policies** → attach `quickstart-guard`.

Trigger another tracked run — the warning will appear in the run output below the plan, and the **Confirm** button will still be enabled (warnings are non-blocking). Change `deny_msgs := []` to a populated set to block applies instead.

---

## Step 8 — Explore the dashboard

The **Dashboard** (home page after login) shows all runs awaiting approval across all stacks, with inline Approve / Discard buttons. As you add more stacks you can approve multiple pending runs from one place without drilling into each stack individually.

---

## What's next

| Goal | Where to go |
| --- | --- |
| Manage Proxmox VMs | [docs/guides/proxmox.md](guides/proxmox.md) |
| Run Ansible playbooks (including OS updates) | [docs/guides/ansible.md](guides/ansible.md) |
| Deploy to production with TLS | [docs/operator-guide.md](operator-guide.md) |
| Write advanced policies | [docs/policies.md](policies.md) |
| Add team members with OIDC | [docs/operator-guide.md](operator-guide.md#authentication) |
| Reuse env vars across stacks | Settings → Variable Sets |
