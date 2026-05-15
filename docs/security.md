# Crucible IAP — Security Model

## Contents

1. [Threat model](#threat-model)
2. [Security controls](#security-controls)
3. [Runner isolation](#runner-isolation)
4. [Authentication and authorisation](#authentication-and-authorisation)
5. [Secrets handling](#secrets-handling)
6. [Network architecture](#network-architecture)
7. [Audit log](#audit-log)
8. [Hardening checklist](#hardening-checklist)
9. [Reporting vulnerabilities](#reporting-vulnerabilities)

---

## Threat model

Crucible runs infrastructure automation jobs on your behalf. The principal threats are:

| Threat | Mitigations |
|--------|------------|
| **Malicious plan output** — a stack's repo injects commands into the runner | Ephemeral containers with read-only root, tmpfs workspace, no network to internal services, `--cap-drop ALL` |
| **Credential theft** — attacker exfiltrates cloud credentials from a running job | Per-job scoped JWTs (15min expiry, `aud=runner`); credentials never stored in DB or queue |
| **Privilege escalation** — authenticated user accesses another org's resources** | JWT carries `org_id`; all DB queries are scoped to org; RBAC enforced at middleware layer |
| **Audit tampering** — operator or compromised admin modifies audit history | Append-only audit table enforced at DB level (PostgreSQL `RULE` blocks UPDATE/DELETE) |
| **Supply chain** — malicious runner image executes arbitrary code | Runner image is pinned; operators control `RUNNER_DEFAULT_IMAGE`; image pull policy enforced by Docker |
| **Unauthenticated API access** — scraping metrics, reading state | `/metrics` is not proxied through Caddy; state backend requires per-stack token auth |

---

## Security controls

### Transport

- TLS enforced by Caddy for all external traffic (Let's Encrypt or self-signed)
- `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload`
- `Content-Security-Policy` blocks inline script injection, clickjacking via `frame-ancestors 'none'`
- `X-Content-Type-Options: nosniff` prevents MIME sniffing
- `Referrer-Policy: strict-origin-when-cross-origin`

### Authentication tokens

| Token type | Expiry | Audience | Purpose |
|------------|--------|----------|---------|
| Access JWT | 15 minutes | `api` | UI/API access |
| Refresh JWT | 7 days | `refresh` | Obtain new access token |
| Runner JWT | Job duration | `runner` | Container → API callbacks only |
| Stack token | Until revoked | — | Terraform state backend HTTP Basic auth |

Runner JWTs are issued per-job and include the `stack_id` claim, validated on every callback. A compromised runner container cannot access other stacks.

### Password storage

Passwords are never stored in the database for local auth. The email/password is held in environment variables (`LOCAL_AUTH_EMAIL`, `LOCAL_AUTH_PASSWORD`) and compared at runtime. Stack tokens are stored as SHA-256 hashes only.

---

## Runner isolation

Each infrastructure job runs in a dedicated Docker container:

```
--read-only                    root filesystem is immutable
--no-new-privileges            seccomp profile cannot be bypassed
--cap-drop ALL                 all Linux capabilities removed
--tmpfs /workspace             ephemeral RAM disk; wiped on exit
--network crucible-runner      isolated network; no access to DB, MinIO, or API
--memory 2g                    bounded resource usage
--cpus 1.0                     bounded CPU
```

The runner container communicates back to the API using a short-lived JWT. It cannot reach PostgreSQL or MinIO directly.

**Egress control:** The `crucible-runner` Docker network is isolated from `frontend` and `backend` networks by default. If your IaC requires outbound internet access (e.g. to reach cloud provider APIs), ensure your Docker host's network policy allows it. For tighter control, use a network proxy or firewall rules on the host to restrict egress to known endpoints.

---

## Authentication and authorisation

### RBAC roles

| Role | Capabilities |
|------|-------------|
| `viewer` | Read stacks, runs, audit log |
| `member` | All viewer + create/trigger runs, manage stack tokens, create policies |
| `admin` | All member + delete stacks, manage org members, manage invites, delete policies |

Roles are enforced in Echo middleware for every mutating API route. The JWT carries the `org_id`; role is looked up from `organization_members` on each request.

### Org invites

Invite tokens are:
- 32 bytes of cryptographically random data (`crypto/rand`)
- Stored as SHA-256 hash only (raw token returned once, not recoverable)
- Single-use (marked accepted on first use inside a transaction)
- 7-day expiry

### OIDC

PKCE (`S256` challenge) is always used for the OIDC flow. Client secrets are never sent to the browser.

---

## Secrets handling

Crucible does not currently store stack-level credentials. Cloud provider credentials (AWS keys, etc.) must be provided via:

1. Environment variables injected at runner container start (planned: built-in vault + external vault integrations)
2. Instance IAM roles / workload identity (recommended for production)

**Do not** put cloud credentials in stack policies or Rego source — they are stored in the database.

---

## BYOK — customer-managed master key

By default, the HKDF master key that protects every vault row is derived from `CRUCIBLE_SECRET_KEY` (server env). BYOK replaces that derivation with a random 32-byte master wrapped by a key in your own KMS. The wrapped blob is stored in `vault_config.master_key_wrapped`; the plaintext master is unwrapped once at server boot and held in memory.

Enable, rotate, and disable from **Settings → BYOK** (admin only). Each transition re-encrypts every vault-protected row in a single transaction and atomically swaps the in-memory master after commit.

### Supported providers

| Provider | `provider` value | Key identifier |
| -------- | ---------------- | -------------- |
| AWS KMS | `aws_kms` | KMS key ARN or alias |
| HashiCorp Vault Transit | `hc_vault_transit` | Transit key name |
| Azure Key Vault | `azure_kv` | Full key URL `https://{vault}.vault.azure.net/keys/{name}[/{version}]` |

### Environment variables

KMS auth lives in env vars — never the database — so the vault can boot without first decrypting any rows.

**AWS KMS:**

- `CRUCIBLE_KMS_AWS_REGION` (required; falls back to `AWS_REGION`)
- `CRUCIBLE_KMS_AWS_ACCESS_KEY_ID` (falls back to `AWS_ACCESS_KEY_ID`)
- `CRUCIBLE_KMS_AWS_SECRET_ACCESS_KEY` (falls back to `AWS_SECRET_ACCESS_KEY`)
- IAM permissions on the key: `kms:Encrypt`, `kms:Decrypt`

**HashiCorp Vault Transit:**

- `CRUCIBLE_KMS_VAULT_ADDR` (required, e.g. `https://vault.example.com`)
- Either `CRUCIBLE_KMS_VAULT_TOKEN` (static token) or `CRUCIBLE_KMS_VAULT_ROLE_ID` + `CRUCIBLE_KMS_VAULT_SECRET_ID` (AppRole)
- Policy must allow `transit/encrypt/{key}` and `transit/decrypt/{key}`

**Azure Key Vault:**

- `CRUCIBLE_KMS_AZURE_TENANT_ID`
- `CRUCIBLE_KMS_AZURE_CLIENT_ID`
- `CRUCIBLE_KMS_AZURE_CLIENT_SECRET`
- The service principal needs `wrapKey` and `unwrapKey` permissions on the key

### Operational notes

- **KMS outage at boot** — a running server keeps working (master is in memory); only restarts during the outage will fail. Treat your KMS as a hard dependency.
- **Rotation** is online — every vault row is re-encrypted in a single transaction, the in-memory master swaps post-commit, and no restart is required.
- **Disable** reverts to `CRUCIBLE_SECRET_KEY`. The KMS-wrapped blob is cleared and `CRUCIBLE_SECRET_KEY` must remain set in the server env afterwards.
- All three transitions emit audit events: `byok.enabled`, `byok.rotated`, `byok.disabled`.

---

## Network architecture

```
Internet
    │  (TLS)
    ▼
Caddy (frontend network)
    ├── /auth, /api, /health  → crucible-api (frontend + backend networks)
    ├── /grafana              → grafana      (frontend + backend networks)
    └── /*                   → crucible-ui  (frontend network only)

crucible-api (backend network)
    ├── postgres  (backend, internal)
    ├── minio     (backend, internal)
    └── prometheus (backend, internal)

crucible-runner network (isolated)
    └── runner containers spawn here; API is reachable via Docker host routing
```

`/metrics` is only reachable from the backend network — it is intentionally not proxied through Caddy.

---

## Audit log

All state-mutating actions are recorded in `audit_events` before the API response is returned. The table is append-only at the database level:

```sql
CREATE RULE no_update_audit AS ON UPDATE TO audit_events DO INSTEAD NOTHING;
CREATE RULE no_delete_audit AS ON DELETE TO audit_events DO INSTEAD NOTHING;
```

Failed login attempts (`auth.login.failed`) are also recorded including the source IP address.

---

## Hardening checklist

Before exposing Crucible to the internet:

- [ ] `CRUCIBLE_SECRET_KEY` is at least 32 random characters (`openssl rand -hex 32`)
- [ ] `POSTGRES_PASSWORD` is strong and not a default value
- [ ] `MINIO_SECRET_KEY` is strong and not a default value
- [ ] `GRAFANA_ADMIN_PASSWORD` is changed from `changeme`
- [ ] `LOCAL_AUTH_PASSWORD` (if used) is strong
- [ ] `CADDY_ACME_EMAIL` is set for Let's Encrypt production certificates
- [ ] `/metrics` is not reachable from the public internet (verified: not proxied by Caddy)
- [ ] Docker socket mount is reviewed — Crucible needs it to spawn runners; restrict with `--group-add` or a Docker socket proxy if required
- [ ] `RUNNER_DEFAULT_IMAGE` is pinned to a specific digest for supply chain control
- [ ] Egress from the `crucible-runner` network is firewalled to only necessary endpoints

---

## Reporting vulnerabilities

Please report security vulnerabilities privately via GitHub's **Security** → **Report a vulnerability** feature on the repository, or email the maintainer directly.

Do not open public issues for security vulnerabilities.
