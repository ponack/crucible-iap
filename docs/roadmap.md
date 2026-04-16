# Crucible IAP — Roadmap

The canonical checklist lives in [README.md](../README.md#roadmap). This document provides expanded context, rationale, and implementation notes for items that need more than one line.

---

## Up Next

### Code Quality & Developer Experience ✓

CI lint job (`gofmt`, `go vet`, `staticcheck`, `gocyclo -over 15`, `ineffassign`, `misspell`) runs on every PR. `make lint` runs the same checks locally. Go Report Card badge added to README.

---

### Runner Image Hardening ✓

- Pin runner base image to a digest (not `:latest`) to prevent supply-chain drift
- Add `HEALTHCHECK` to runner Dockerfile
- Publish signed runner image via `cosign` on release

---

### Ansible Support ✓

Implemented. Ansible runs follow the same check → confirm → apply lifecycle as OpenTofu:

- `--check --diff` output is captured and uploaded as the plan artifact
- PLAY RECAP is parsed to extract `changed`/`unreachable` counts for PR comments
- Inventory auto-detected from common repo paths; override with `CRUCIBLE_ANSIBLE_INVENTORY`
- Destroy runs require `CRUCIBLE_ANSIBLE_DESTROY_PLAYBOOK` (Ansible has no built-in destroy)
- `ANSIBLE_HOST_KEY_CHECKING=False` set automatically in ephemeral containers

---

## Medium Term

### Pulumi Support ✓

Implemented. Pulumi runs follow the same preview → confirm → apply lifecycle as OpenTofu:

- `pulumi preview --diff` output is captured and uploaded as the plan artifact
- `pulumi preview --json` is parsed for `create` / `update` / `delete` counts (shown in run summary and PR comments)
- MinIO is automatically configured as the DIY S3 backend (`PULUMI_BACKEND_URL=s3://...`) — no Pulumi Cloud account required
- `PULUMI_CONFIG_PASSPHRASE` must be set as a stack secret for state encryption
- Override the backend with `PULUMI_BACKEND_URL` on the stack for AWS S3, GCS, or any S3-compatible store
- Override the stack name with `CRUCIBLE_PULUMI_STACK` (default: `crucible-<stack-id>`)
- Node.js (for TypeScript/JavaScript programs) and Python are pre-installed in the runner image; `pulumi install` handles language plugins and dependencies at run time

Stack references for cross-stack outputs are not yet implemented — use remote state sources as a workaround.

### Stack Dependency Graph

First-class upstream/downstream stack relationships. After stack A applies successfully, automatically trigger a tracked run on downstream stacks. Replaces the current manual `trigger` policy hook for simple linear chains.

### Variable Sets

Define a named group of env vars once and attach to multiple stacks. Eliminates copy-paste across stacks that share the same provider credentials or feature flags. Encrypted at rest the same way stack env vars are.

### Fine-Grained RBAC

Resource-level permissions: per-stack viewer and approver roles, not just org-wide member/admin. Needed for larger teams where different people own different stacks.

### Exportable Config

Export the full instance configuration as a single compressed archive (`.tar.gz`) — and import it on another instance. Useful for backup, DR, staging-to-prod promotion, and onboarding new team members into an identical environment.

**What gets exported:**

- Stacks (all fields; secret values omitted unless `--password` is provided — see below)
- Policies (name, type, Rego body, stack attachments)
- Variable sets (names and attached stacks; encrypted values omitted unless `--password` is provided)
- Org settings (runner defaults, SMTP config minus password, notification defaults)
- Integration metadata (name, type; credentials omitted unless `--password` is provided)

**What is always excluded:**

- Run history, audit log, state files — operational data, not config
- Users and org membership — identity is tied to the IdP

**Archive layout:**

```text
crucible-export-<timestamp>.tar.gz
├── crucible-export.json   # plaintext config manifest (human-readable)
└── secrets.enc            # present only when --password is supplied
```

**Optional secret export (`--password`):**

When an export password is provided, all encrypted secret values (stack env var values, VCS tokens, integration credentials, SMTP password) are decrypted from the vault, re-encrypted as a single blob using AES-256-GCM with an Argon2id-derived key, and written to `secrets.enc`. The Argon2id parameters and a random salt are stored in the file header so the import side needs only the password — no shared vault key required.

On import with the matching password, `secrets.enc` is decrypted and each secret value is re-encrypted under the new instance's vault key before being written to the database. Without the password, `secrets.enc` is ignored and secrets are imported as empty (operators re-enter them post-import).

This design keeps the plaintext manifest readable and auditable regardless of whether secrets are included, and the secrets blob is clearly a separate, opt-in artifact.

**Format:** `crucible-export.json` is a versioned JSON document. The `version` field allows future schema evolution and is checked on import before committing.

**Conflict strategy:** Import by default skips objects that already exist by name/slug; an `--overwrite` flag replaces them. Stacks imported without state simply start fresh on first run.

---

## Long Term / Speculative

### Multi-node / HA

- PostgreSQL connection pooling (PgBouncer)
- Stateless API — run multiple API instances behind a load balancer
- Remote Docker host support for runner containers (not just local socket)

### Terraform Provider Caching

Vendor provider plugins into MinIO so repeated runs skip registry downloads. Critical for air-gapped deployments.

### Cost Estimation

Integrate Infracost or similar to surface estimated monthly delta in the UI alongside the plan summary.

### External Worker Agents

Lightweight agent binary that connects to the primary instance and executes jobs locally on the agent host. Decouples runner capacity from the API host; no Docker socket on the central server required.
