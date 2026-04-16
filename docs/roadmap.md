# Crucible IAP — Roadmap

The canonical checklist lives in [README.md](../README.md#roadmap). This document provides expanded context, rationale, and implementation notes for items that need more than one line.

---

## Up Next

### Code Quality & Developer Experience

**Go Report Card integration** ([gojp/goreportcard](https://github.com/gojp/goreportcard))

Run goreportcard-equivalent checks in CI on every PR targeting `main`. Goal: clean score before adding the badge to README.

Checks to add to `.github/workflows/ci.yml`:

| Check | Tool | Notes |
| ----- | ---- | ----- |
| Formatting | `gofmt -l` | Fail if any files differ |
| Suspicious constructs | `go vet ./...` | Already in most Go CIs |
| Cyclomatic complexity | `gocyclo -over 15 .` | Threshold 15; ignore generated files |
| Unused assignments | `ineffassign ./...` | |
| Common typos | `misspell -error .` | Catches comments + strings |
| Static analysis | `staticcheck ./...` | Superset of golint; preferred over deprecated golint |

Also add a `make lint` target so contributors can run the same checks locally before pushing. The target should run both Go linting and `pnpm lint` for the UI in one command.

Once the codebase passes cleanly, add the goreportcard badge to README alongside the existing CI and license badges.

Implementation sketch for `.github/workflows/ci.yml`:

```yaml
lint:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: api/go.mod
    - name: Install linters
      run: |
        go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
        go install github.com/client9/misspell/cmd/misspell@latest
        go install honnef.co/go/tools/cmd/staticcheck@latest
        go install github.com/gordonklaus/ineffassign@latest
    - name: gofmt
      run: |
        files=$(gofmt -l ./api/...)
        [ -z "$files" ] || (echo "Unformatted files: $files" && exit 1)
    - name: go vet
      run: cd api && go vet ./...
    - name: gocyclo
      run: gocyclo -over 15 api/
    - name: misspell
      run: misspell -error api/
    - name: staticcheck
      run: cd api && staticcheck ./...
    - name: ineffassign
      run: ineffassign api/...
```

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

### Pulumi Support

- Implement `run_pulumi` in `entrypoint.sh`
- Pulumi DIY backend via MinIO (Pulumi supports `PULUMI_BACKEND_URL=s3://...`)
- Stack reference support for cross-stack outputs

### Stack Dependency Graph

First-class upstream/downstream stack relationships. After stack A applies successfully, automatically trigger a tracked run on downstream stacks. Replaces the current manual `trigger` policy hook for simple linear chains.

### Variable Sets

Define a named group of env vars once and attach to multiple stacks. Eliminates copy-paste across stacks that share the same provider credentials or feature flags. Encrypted at rest the same way stack env vars are.

### Fine-Grained RBAC

Resource-level permissions: per-stack viewer and approver roles, not just org-wide member/admin. Needed for larger teams where different people own different stacks.

### Exportable Config

Export the full instance configuration as a single compressed archive (`.tar.gz` or `.zip`) — and import it on another instance. Useful for backup, DR, staging-to-prod promotion, and onboarding new team members into an identical environment.

**What gets exported:**

- Stacks (all fields except encrypted secrets — env var names are included but values are omitted)
- Policies (name, type, Rego body, stack attachments)
- Variable sets (names and attached stacks; encrypted values omitted)
- Org settings (runner defaults, SMTP config minus password, notification defaults)
- Integration metadata (name, type — no credentials)

**What is intentionally excluded:**

- Encrypted secret values (env var values, VCS tokens, integration credentials) — these are write-only and cannot be exported safely
- Run history, audit log, state files — operational data, not config
- Users and org membership — identity is tied to the IdP

**Format:** A JSON manifest (`crucible-export.json`) inside a gzip-compressed tar. Importable via `POST /api/v1/admin/import` with a dry-run mode that reports conflicts before committing.

**Conflict strategy:** Import by default skips objects that already exist by name/slug; a `--overwrite` flag replaces them. Stacks imported without state simply start fresh on first run.

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
