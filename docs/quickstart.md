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

You **do not** need a DNS domain for the quickstart — it runs on `https://localhost` with a self-signed cert. A real domain is only needed when you move to production with Let's Encrypt, OIDC SSO, cloud OIDC federation, or webhooks from cloud-hosted VCS providers. See the [operator guide's "Do I need a DNS domain?" table](operator-guide.md#do-i-need-a-dns-domain) for the per-scenario breakdown.

---

## Step 1 — Deploy Crucible

```bash
git clone https://github.com/ponack/crucible-iap.git
cd crucible-iap
cp .env.example .env
```

Open `.env` and set these four values — everything else can stay as-is for local use:

```env
CRUCIBLE_BASE_URL=https://localhost
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

Wait for migrations to complete, then confirm the API is healthy:

```bash
# Poll /health until it returns status:ok (usually <30s on first start)
# -k skips TLS verification for the self-signed Caddy cert on localhost
until curl -skf https://localhost/health | grep -q '"status":"ok"'; do
  echo "waiting for crucible-api…"; sleep 2
done
docker compose ps
```

Expected: `HTTP 200` with `"status":"ok"` in the body.

The UI is at **`https://localhost`**. Open it, accept the self-signed certificate warning on first visit, and log in with the email and password you set above.

> **First-run note:** On the very first start, Crucible creates the MinIO buckets and runs all database migrations automatically. The `crucible-api` container will restart once while it waits for PostgreSQL to become healthy — this is normal. Caddy generates a local TLS cert the first time it starts; your browser will warn that it is not trusted — accept it for `https://localhost`.

---

## Step 2 — Create a test repository

### Option A — Use the template (recommended)

Go to [github.com/ponack/crucible-quickstart](https://github.com/ponack/crucible-quickstart) and click **Use this template → Create a new repository**. Name it anything you like. The template ships the two files below already — skip ahead to Step 3.

### Option B — Write the files yourself

Create a new repository on GitHub (or GitLab/Gitea). Add two files:

**`versions.tf`**

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

**`main.tf`**

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

New to Infrastructure as Code? Read **[Infrastructure as Code with Crucible — A Beginner's Introduction](iac-101.md)** for the underlying concepts, and bookmark the **[Glossary](glossary.md)** for terminology.

If something goes wrong during the walkthrough, check **[Troubleshooting](troubleshooting.md)** before opening an issue.

### Pick your next step by role

#### Infrastructure engineer — connect real infrastructure

| Goal | Where to go |
| --- | --- |
| Manage AWS resources | [guides/aws.md](guides/aws.md) |
| Manage GCP resources | [guides/gcp.md](guides/gcp.md) |
| Manage Azure resources | [guides/azure.md](guides/azure.md) |
| Manage Cloudflare DNS / WAF / Workers | [guides/cloudflare.md](guides/cloudflare.md) |
| Manage Proxmox VMs | [guides/proxmox.md](guides/proxmox.md) |
| Run Ansible playbooks | [guides/ansible.md](guides/ansible.md) |
| Use Pulumi | [guides/pulumi.md](guides/pulumi.md) |
| Use Terragrunt (multi-module repos) | [guides/terragrunt.md](guides/terragrunt.md) |

#### Platform engineer — scale to a team

| Goal | Where to go |
| --- | --- |
| Organize stacks for multiple teams | [guides/projects.md](guides/projects.md) |
| Set up org roles, approval gates | [guides/team-setup.md](guides/team-setup.md) |
| Publish reusable stack templates | [guides/stack-templates.md](guides/stack-templates.md) |
| Self-service stack creation for app teams | [guides/blueprints.md](guides/blueprints.md) |
| Deploy Crucible to production with TLS | [operator-guide.md](operator-guide.md) |

#### Security / governance — add guardrails

| Goal | Where to go |
| --- | --- |
| Write policies that block dangerous changes | [policies.md](policies.md) |
| Use ready-made policy templates | [policies/README.md](policies/README.md) |
| Sync policies from a git repo | [guides/policy-gitops.md](guides/policy-gitops.md) |
| Enforce SOC 2 / CIS / HIPAA / PCI-DSS | [operator-guide.md#compliance-packs](operator-guide.md#compliance-packs) |
| Stream audit events to Splunk / Datadog / etc. | [operator-guide.md#siem-audit-log-streaming](operator-guide.md#siem-audit-log-streaming) |
| Detect drift periodically | [operator-guide.md#continuous-validation](operator-guide.md#continuous-validation) |

#### Power user — automation

| Goal | Where to go |
| --- | --- |
| Trigger and approve runs from the terminal | [guides/cli.md](guides/cli.md) |
| Approve runs from Slack / Teams / Discord | [operator-guide.md#chatops-approvals](operator-guide.md#chatops-approvals) |
| Reuse env vars across stacks | Settings → Variable Sets |
| Enable Terraform cost estimates | Settings → Integrations → Infracost API key |
| Connect Bitbucket or Azure DevOps | [operator-guide.md#vcs-integrations](operator-guide.md#vcs-integrations) |
