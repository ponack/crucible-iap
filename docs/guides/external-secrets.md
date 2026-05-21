# Guide: External Secret Stores

Crucible can fetch secrets from an external secret store at run time and inject them into the runner container as environment variables — so you never need to copy production credentials into Crucible's own vault. Supported stores:

| Store | Type slug | Notes |
| --- | --- | --- |
| AWS Secrets Manager | `aws_sm` | Per-region; supports JSON-valued secrets |
| HashiCorp Vault (KV v2) | `hc_vault` | Token or AppRole auth; HCP Vault namespaces supported |
| Bitwarden Secrets Manager | `bitwarden_sm` | Machine account access token |
| Vaultwarden / self-hosted Bitwarden | `vaultwarden` | SecureNote items in a specified folder |

For storing credentials directly in Crucible (no external store needed), use [Variable Sets](variable-sets.md) or per-stack env vars.

---

## How it works

```text
   Stack run starts
        │
        ▼
   ┌──────────────────────────┐
   │ Crucible API:            │
   │  - decrypt integration   │  ← config encrypted in Crucible vault
   │    config (vault key)    │     (HKDF-derived or BYOK-wrapped)
   │  - call provider API     │
   │  - normalize keys to     │
   │    UPPER_SNAKE_CASE      │
   └────────────┬─────────────┘
                ▼
   Runner container starts with the fetched secrets
   exported as environment variables.
```

The integration config (API keys, endpoints, paths) is encrypted at rest in Crucible's vault using a per-integration HKDF context. With [BYOK](../operator-guide.md#byok--customer-managed-master-key) enabled, the master key is wrapped by your own KMS — Crucible never holds plaintext config across restarts.

If the external store is unreachable when a run starts, the run **fails fast** rather than running with missing secrets. There is no caching of fetched values.

---

## Configuration overview

Two-step setup:

1. **Create an integration** at **Settings → Integrations → New integration**. Choose the provider type and fill in the credentials/connection details. The config is encrypted on save.
2. **Attach the integration to a stack** at **Stack detail → Settings → Secret store**. One integration per stack; runs without an attached integration are unaffected.

Detailed per-provider configuration follows.

---

## AWS Secrets Manager (`aws_sm`)

Fetches one or more secrets from AWS Secrets Manager. JSON-valued secrets are auto-expanded into individual env vars.

### Config fields

| Field | Required | Notes |
| --- | --- | --- |
| `region` | yes | AWS region where the secrets live (`us-east-1`, etc.) |
| `access_key_id` | optional | Falls back to the `AWS_ACCESS_KEY_ID` env var on the Crucible host if blank |
| `secret_access_key` | optional | Falls back to `AWS_SECRET_ACCESS_KEY` on the host if blank |
| `secret_names` | yes | Array of secret ARNs or names to fetch |

**Recommended:** Don't set static `access_key_id` / `secret_access_key`. Instead, attach an IAM role to the Crucible host (EC2 instance role, ECS task role, IRSA on EKS) and leave both fields blank. The AWS SDK fallback uses the host's role.

Minimum IAM permissions on the secrets:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["secretsmanager:GetSecretValue"],
    "Resource": [
      "arn:aws:secretsmanager:us-east-1:111122223333:secret:prod/myapp/*"
    ]
  }]
}
```

### Secret value handling

- **Plain string secret** (`SecretString` is not JSON) → injected as one env var, name derived from the last path component of the secret name, upper-cased.
  - `prod/myapp/db-password` → `DB_PASSWORD=<value>`
- **JSON object secret** → each key becomes its own env var, name upper-cased.
  - Value `{"username":"app","password":"s3cret"}` → `USERNAME=app`, `PASSWORD=s3cret`

### Example

Integration config:

```json
{
  "region": "us-east-1",
  "secret_names": [
    "prod/checkout/db-creds",
    "prod/checkout/stripe-key"
  ]
}
```

If `prod/checkout/db-creds` contains `{"username":"app","password":"abc123"}` and `prod/checkout/stripe-key` is a plain string `sk_live_xxx`, the runner sees:

```bash
USERNAME=app
PASSWORD=abc123
STRIPE_KEY=sk_live_xxx
```

---

## HashiCorp Vault KV v2 (`hc_vault`)

Reads a single KV v2 path; all keys at that path become env vars.

### Config fields

| Field | Required | Notes |
| --- | --- | --- |
| `address` | yes | Vault URL, e.g. `https://vault.example.com` |
| `namespace` | optional | HCP Vault / Vault Enterprise namespace |
| `token` | optional | Static Vault token — simplest, but rotates manually |
| `role_id` + `secret_id` | optional | AppRole auth — preferred for automation |
| `mount` | yes | KV v2 mount name, e.g. `secret` |
| `path` | yes | Path within the mount, e.g. `myapp/prod/config` |

Provide *either* `token` or `role_id`/`secret_id` — not both.

### Vault policy

For AppRole, the role needs:

```hcl
path "secret/data/myapp/prod/config" {
  capabilities = ["read"]
}
```

For static tokens, the token's policies must grant the same `read`.

### Key handling

KV v2 values that are strings become env vars directly. Non-string values (numbers, booleans, nested objects, arrays) are JSON-encoded as strings — useful for passing structured config, but Terraform `TF_VAR_*` consumers may not parse them as expected.

### Example

Integration config:

```json
{
  "address": "https://vault.example.com",
  "role_id": "abc-123-...",
  "secret_id": "def-456-...",
  "mount": "secret",
  "path": "myapp/prod/config"
}
```

If `secret/data/myapp/prod/config` contains:

```json
{
  "DATABASE_URL": "postgres://...",
  "API_KEY": "..."
}
```

The runner sees `DATABASE_URL` and `API_KEY` as env vars.

---

## Bitwarden Secrets Manager (`bitwarden_sm`)

Fetches all secrets in a project (or all org secrets) via a machine-account access token.

### Config fields

| Field | Required | Notes |
| --- | --- | --- |
| `access_token` | yes | Machine account token in format `0.<serviceAccountId>.<clientSecret>.<base64EncKey>` |
| `project_id` | one of | Limits fetch to a specific SM project |
| `org_id` | one of | Fetches all secrets in the org (when no `project_id`) |
| `api_url` | optional | Override for self-hosted Bitwarden API (`https://vault.example.com/api`) |
| `identity_url` | optional | Override for self-hosted Bitwarden identity (`https://vault.example.com/identity`) |

You must set either `project_id` or `org_id` — not both, and not neither.

The access token bakes the encryption key into its final segment. Crucible parses it locally; the encryption key never leaves the Crucible host except over TLS to Bitwarden for the API session.

### Key handling

Each secret's **name** becomes the env var key (upper-cased). The **value** field becomes the env var value. Bitwarden secret notes are ignored.

### Example

Integration config:

```json
{
  "access_token": "0.abc-123.def-456.<base64key>",
  "project_id": "<uuid>"
}
```

If the project contains secrets named `database_url`, `api_key`, and `webhook_secret`, the runner sees `DATABASE_URL`, `API_KEY`, `WEBHOOK_SECRET`.

---

## Vaultwarden / self-hosted Bitwarden (`vaultwarden`)

Fetches SecureNote items from a Vaultwarden or vanilla self-hosted Bitwarden vault. Each note's **name** becomes the env var key and its **notes** field becomes the value.

This provider is intended for homelab / small-team setups where you already run Vaultwarden. For production teams, prefer Bitwarden Secrets Manager or HashiCorp Vault.

### Config fields

| Field | Required | Notes |
| --- | --- | --- |
| `url` | yes | Vaultwarden base URL (`https://vault.example.com`) |
| `client_id` | yes | API client ID — `user.{uuid}` from Account Settings → Security → Keys → API key |
| `client_secret` | yes | API client secret from the same screen |
| `email` | yes | Account email (used in key derivation) |
| `master_password` | yes | Account master password (decrypts the vault's symmetric key) |
| `folder_name` | optional | Restrict to SecureNotes in a specific folder |

The master password is required because Vaultwarden / Bitwarden encrypt vault contents client-side. There is no server-side decryption; Crucible performs the PBKDF2 → vault key → AES-CBC decryption locally for each secret.

### Setup in Vaultwarden

1. **Account settings → Security → Keys → API key** — generate a new API key. Note the `client_id` (`user.{uuid}`) and `client_secret`.
2. **Create a folder** (optional) — e.g. "crucible-stacks". You'll reference this name in the integration config.
3. **Create SecureNote items** in that folder. Set the item **Name** to the env var key (`DATABASE_URL`, `API_KEY`) and put the value in the **Notes** field.
4. Add the integration in Crucible with the values from step 1 plus your master password.

The master password is stored encrypted in Crucible's vault (HKDF-derived or BYOK-wrapped). It is only decrypted in memory at run time.

### Example

Folder `crucible-prod` in Vaultwarden contains:

- SecureNote `DATABASE_URL` with notes field `postgres://...`
- SecureNote `STRIPE_KEY` with notes field `sk_live_...`

Integration config:

```json
{
  "url": "https://vault.example.com",
  "client_id": "user.abc-123-...",
  "client_secret": "def456...",
  "email": "ops@example.com",
  "master_password": "...",
  "folder_name": "crucible-prod"
}
```

Runner sees `DATABASE_URL` and `STRIPE_KEY`.

---

## Choosing a provider

| You already use… | Recommended provider |
| --- | --- |
| AWS in production, want managed | `aws_sm` |
| HashiCorp Vault, on-prem or HCP | `hc_vault` |
| Bitwarden Cloud for the team | `bitwarden_sm` |
| Self-hosted Vaultwarden in a homelab | `vaultwarden` |
| None of the above, just want it to work | [Variable Sets](variable-sets.md) — store in Crucible directly |

For new deployments without an existing secret store, start with Variable Sets. Adopt an external store later when you have multiple consumers of the same secrets (e.g. CI plus Crucible plus deployed apps).

---

## Auditing

Every integration lifecycle action is recorded:

- `integration.created` / `integration.updated` / `integration.deleted`
- `stack.secret_integration_set` / `stack.secret_integration_cleared`

Per-run secret fetches are not audited individually (would be noisy). The integration's last-error is shown on the integration detail page if a run fails to fetch.

---

## Common errors

### "credentials required" (AWS)

You set neither static credentials in the integration config nor `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` in the Crucible host environment. Either set static creds (not recommended), or attach an IAM instance/task role to the Crucible host.

### "secret not found at mount/path" (Vault)

The path doesn't exist or the auth method's policy doesn't grant `read`. Test with the Vault CLI using the same token/role:

```bash
vault kv get -mount=secret myapp/prod/config
```

### "approle login returned 400" (Vault)

The `role_id` or `secret_id` is wrong, or the AppRole has been deleted/expired. Generate a new `secret_id`:

```bash
vault write -f auth/approle/role/crucible/secret-id
```

### "invalid access token" (Bitwarden SM)

The access token format must be `0.<id>.<secret>.<key>` with four dot-separated parts. Re-generate the machine account token in Bitwarden SM.

### "decryption failed" (Vaultwarden)

The master password is wrong, or the user's KDF settings have changed since you saved the integration. Re-enter the master password.

---

## What's next

- [BYOK](../operator-guide.md#byok--customer-managed-master-key) — wrap Crucible's vault master key with your own KMS for additional defense in depth.
- [SIEM streaming](../operator-guide.md#siem-audit-log-streaming) — forward integration audit events to your SIEM.
- [Variable Sets](variable-sets.md) — pair external secrets with sets for static values (regions, tags) that don't belong in a secret store.
