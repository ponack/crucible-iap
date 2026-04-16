# Guide: Running Ansible Playbooks with Crucible IAP

This guide walks through setting up a GitOps workflow for Ansible using Crucible IAP. By the end you will have a stack that runs `ansible-playbook --check --diff` (preview) on every push, requires manual confirmation before the live run, and stores a full audit trail of every change.

The example configures an Ubuntu server — hostname, packages, a system user, and a cron job — but the pattern applies to any playbook.

---

## How Ansible runs differ from OpenTofu

| Concept | OpenTofu | Ansible |
| --- | --- | --- |
| Plan phase | `tofu plan` | `ansible-playbook --check --diff` |
| Apply phase | `tofu apply` | `ansible-playbook` (live run) |
| Destroy | `tofu plan -destroy` → `tofu apply` | separate teardown playbook via `CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK` |
| State file | managed by Crucible | none — Ansible is push-based |
| Diff format | structured JSON plan | free-text PLAY RECAP + task diffs |

Crucible captures the `--check --diff` output as the plan artifact. The PLAY RECAP is parsed to extract `changed` and `unreachable` counts, which appear in the run summary and PR comments.

---

## Prerequisites

- Crucible IAP v0.4.0+ running (see [operator-guide.md](../operator-guide.md))
- One or more target hosts with SSH access from the Crucible runner network
- A Git repository (GitHub, GitLab, or Gitea) you can push to
- An SSH private key the runner can use to reach your hosts (or password auth via `ansible_password`)

---

## 1. Repository structure

```
homelab-ansible/
├── site.yml               # main playbook (default; override with CRUCIBLE_ANSIBLE_PLAYBOOK)
├── teardown.yml           # required only if you use destroy runs
├── inventory.yml          # auto-detected by the runner
├── group_vars/
│   └── all.yml
└── roles/
    └── base/
        ├── tasks/
        │   └── main.yml
        └── handlers/
            └── main.yml
```

### `inventory.yml`

The runner auto-detects inventory files at the following paths (checked in order):

```
inventory.ini  →  inventory.yml  →  inventory.yaml  →  inventory  →  hosts  →  hosts.ini
```

The first match wins. To use a different path, set `CRUCIBLE_ANSIBLE_INVENTORY` on the stack.

```yaml
all:
  children:
    servers:
      hosts:
        web01:
          ansible_host: "{{ lookup('env', 'TARGET_HOST') }}"
          ansible_user: ubuntu
          ansible_ssh_private_key_file: /tmp/crucible_ssh_key
```

> The SSH key is written to `/tmp/crucible_ssh_key` at run time from the `ANSIBLE_SSH_KEY` secret — see step 5.

### `site.yml`

```yaml
---
- name: Configure servers
  hosts: servers
  become: true
  roles:
    - base
```

### `roles/base/tasks/main.yml`

```yaml
---
- name: Set hostname
  ansible.builtin.hostname:
    name: "{{ inventory_hostname }}"

- name: Install base packages
  ansible.builtin.apt:
    name:
      - curl
      - git
      - htop
      - unattended-upgrades
    state: present
    update_cache: true

- name: Create service user
  ansible.builtin.user:
    name: svc
    system: true
    shell: /usr/sbin/nologin
    create_home: false

- name: Deploy maintenance cron job
  ansible.builtin.cron:
    name: "apt autoclean"
    minute: "0"
    hour: "3"
    job: "apt-get autoclean -y >> /var/log/apt-autoclean.log 2>&1"
    user: root
```

### `roles/base/handlers/main.yml`

```yaml
---
- name: Reload sshd
  ansible.builtin.service:
    name: ssh
    state: reloaded
```

### `teardown.yml`

Required only if you enable destroy runs on the stack. Ansible has no built-in destroy — this playbook reverses the changes made by `site.yml`.

```yaml
---
- name: Teardown servers
  hosts: servers
  become: true
  tasks:
    - name: Remove service user
      ansible.builtin.user:
        name: svc
        state: absent
        remove: true

    - name: Remove maintenance cron job
      ansible.builtin.cron:
        name: "apt autoclean"
        state: absent
        user: root
```

### `group_vars/all.yml`

```yaml
---
# Ansible connection defaults — override per host in inventory if needed.
ansible_python_interpreter: /usr/bin/python3
```

Push all files to your repository before continuing.

---

## 2. Create the stack in Crucible

**Stacks → New Stack**:

| Field | Value |
| --- | --- |
| Name | `homelab-ansible` |
| Tool | `ansible` |
| Tool version | leave blank (runner image ships Ansible 11) |
| Repo URL | your repository URL |
| Branch | `main` |
| Working directory | `/` |
| Auto-apply | off — confirm applies manually |

---

## 3. Add a policy (optional but recommended)

Ansible's check output is free text, so policy evaluation uses the parsed summary counts rather than a structured plan. A useful guard blocks applies when unreachable hosts are detected.

**Policies → New Policy**:

- Name: `ansible-safety`
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

# Block apply when any host was unreachable during the check run.
deny_msgs contains msg if {
  input.plan_summary.destroy > 0
  msg := sprintf("%d host(s) were unreachable during check — fix connectivity before applying", [input.plan_summary.destroy])
}

# Warn on large change sets.
warn_msgs contains msg if {
  input.plan_summary.change > 10
  msg := sprintf("check run shows %d changes — review the diff carefully before confirming", [input.plan_summary.change])
}
```

> Crucible maps Ansible's `unreachable` count to the `destroy` field in the plan summary (closest semantic match). The `change` field maps to `changed`.

Back on the stack detail page → **Policies** → attach `ansible-safety`.

---

## 4. Prepare the SSH key

The runner container needs an SSH private key to reach your hosts. The recommended approach is:

1. Generate a dedicated key pair:
   ```bash
   ssh-keygen -t ed25519 -C "crucible-runner" -f ~/.ssh/crucible_runner -N ""
   ```

2. Append the public key to `~/.ssh/authorized_keys` on each target host.

3. Copy the private key content — you will paste it as a secret in the next step.

---

## 5. Add environment variables

Stack detail page → **Environment Variables**:

| Key | Value | Secret |
| --- | --- | --- |
| `TARGET_HOST` | IP or hostname of your target server | no |
| `ANSIBLE_SSH_KEY` | contents of the private key file | **yes** |

The inventory references `/tmp/crucible_ssh_key`. Add a pre-task to your playbook to write the key at run time:

```yaml
# site.yml — add this play before your existing plays
- name: Write SSH key
  hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - name: Write private key from environment
      ansible.builtin.copy:
        content: "{{ lookup('env', 'ANSIBLE_SSH_KEY') }}"
        dest: /tmp/crucible_ssh_key
        mode: "0600"
```

> **Ephemeral containers:** the runner container is destroyed after every job, so writing secrets to `/tmp` is safe — they never persist between runs.

Alternatively, if your hosts are reachable with a password, skip the key setup and set `ANSIBLE_PASSWORD` + use `ansible_password` in your inventory instead. Vault-encrypted variables work the same way — add `ANSIBLE_VAULT_PASSWORD` and reference it via `--vault-password-file` in `CRUCIBLE_ANSIBLE_PLAYBOOK_ARGS` (see [advanced options](#advanced-options) below).

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

### Manual check run (proposed)

Stack detail → **Trigger proposed run**. The runner executes:

```bash
ansible-playbook site.yml --check --diff -i inventory.yml
```

A successful check looks like:

```
PLAY [Configure servers] *****************************************************

TASK [base : Set hostname] ***************************************************
ok: [web01]

TASK [base : Install base packages] ******************************************
ok: [web01]

PLAY RECAP *******************************************************************
web01                      : ok=4    changed=0    unreachable=0    failed=0
```

The output is stored as the plan artifact and shown in the UI. `changed=0` means the host is already in the desired state; push a change to see diffs.

### GitOps apply (tracked)

Push a commit adding a new package to the `apt` task. Crucible creates a `tracked` run automatically:

1. Status: `planning` — runner executes `--check --diff`, captures output
2. Status: `unconfirmed` — review the task diff in the UI
3. Click **Confirm** — runner executes `ansible-playbook site.yml -i inventory.yml` (live)
4. Status: `finished` — host is updated

### Pull request preview (proposed)

Open a PR adding a new role. Crucible:

- Creates a `proposed` run (check only — no apply)
- Posts the PLAY RECAP as a comment on the PR
- Sets a commit status check

No live run happens until the PR is merged and a tracked run completes.

### Destroy run

Stack detail → **Destroy infra** → type the stack name to confirm.

The runner executes `teardown.yml --check --diff` first. After you confirm, it runs `teardown.yml` live. If `CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK` is not set, the run fails immediately with a clear error.

---

## Advanced options

All Ansible-specific behaviour is controlled via environment variables on the stack.

| Variable | Default | Description |
| --- | --- | --- |
| `CRUCIBLE_ANSIBLE_PLAYBOOK` | `site.yml` | Playbook to run for plan and apply phases |
| `CRUCIBLE_ANSIBLE_INVENTORY` | auto-detect | Path to inventory file or directory; overrides auto-detection |
| `CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK` | *(none)* | Playbook to run for destroy runs; required if you use the destroy feature |
| `ANSIBLE_HOST_KEY_CHECKING` | `False` | Set by runner automatically; override to `True` if your hosts are in a known_hosts file baked into a custom runner image |
| `ANSIBLE_FORCE_COLOR` | `True` | Set by runner automatically |

Any `ANSIBLE_*` variable set on the stack is passed through to the runner container unchanged — this is the standard way to configure Ansible settings (callback plugins, vault password files, retry limits, etc.).

### Using ansible-vault

If your repository contains vault-encrypted files:

1. Add `ANSIBLE_VAULT_PASSWORD` as a secret environment variable containing the vault password.
2. Add a pre-task or use a custom runner image that writes it to a file, then reference it:

```yaml
# site.yml — add before your main plays
- name: Configure vault password file
  hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - name: Write vault password
      ansible.builtin.copy:
        content: "{{ lookup('env', 'ANSIBLE_VAULT_PASSWORD') }}"
        dest: /tmp/.vault_pass
        mode: "0600"
```

Then set `ANSIBLE_VAULT_PASSWORD_FILE=/tmp/.vault_pass` as a non-secret env var on the stack.

### Multiple inventories

To target a subset of hosts per run, use separate stacks — one per environment — each with a different `CRUCIBLE_ANSIBLE_INVENTORY` pointing to an environment-specific file:

```
inventory/
├── production.yml
└── staging.yml
```

Stack `homelab-ansible-staging` → `CRUCIBLE_ANSIBLE_INVENTORY=inventory/staging.yml`  
Stack `homelab-ansible-prod` → `CRUCIBLE_ANSIBLE_INVENTORY=inventory/production.yml`

### Custom runner image with collections

The default runner image ships with `ansible-core` + the `ansible` community package (which bundles most common collections). If you need additional collections, build a custom image:

```dockerfile
FROM ghcr.io/ponack/crucible-runner:latest
RUN ansible-galaxy collection install \
    community.postgresql \
    community.docker \
    --collections-path /usr/share/ansible/collections
```

Push the image to a registry accessible to your Crucible instance and set it as the runner image in **Admin → Settings → Runner default image**, or override it per-stack via the **Runner image** field.

---

## Troubleshooting

### Check run fails: "unreachable"

The runner cannot SSH to the target host. Common causes:

- **Key not written:** verify the `Write SSH key` pre-task is the first play in `site.yml` and that `ANSIBLE_SSH_KEY` is set as a secret
- **Wrong user:** `ansible_user` in inventory doesn't match the user on the host
- **Firewall:** runner container IP not allowed; check host firewall rules
- **Host key verification:** `ANSIBLE_HOST_KEY_CHECKING` defaults to `False` in the runner, but a custom override may have re-enabled it without a known_hosts file

Run a manual proposed run and expand the full log output — Ansible prints the exact SSH error before marking the host unreachable.

### "MODULE FAILURE" or Python errors

The runner ships Python 3 and uses `/usr/bin/python3` on the target. If the target host has Python in a different location, set `ansible_python_interpreter` in your inventory or `group_vars/all.yml`:

```yaml
ansible_python_interpreter: /usr/bin/python3.11
```

### Idempotency warnings on check runs

`changed=N` on every check run when nothing should have changed usually means a task is not idempotent. Common culprits are `ansible.builtin.shell` / `command` tasks without a `creates` or `changed_when: false` guard. Fix the task; check runs should consistently report `changed=0` on a host that is already in the desired state.

### Destroy run fails: "CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK not set"

You triggered a destroy run without setting the destroy playbook variable. Either:
- Set `CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK=teardown.yml` (or your teardown playbook path) on the stack, or
- Disable destroy runs on the stack if you don't want to support teardown

### Policy blocks apply: "host(s) unreachable"

Fix SSH connectivity to the unreachable host(s) before confirming the apply. If the host is intentionally decommissioned, remove it from the inventory and push a commit to trigger a fresh check run.
