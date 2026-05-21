# Guide: Variable Sets

A **variable set** is a named, reusable bundle of environment variables that can be attached to many stacks at once. Update a value once in the set, and every attached stack picks it up on the next run.

Use variable sets when you find yourself copy-pasting the same env vars across stacks — cloud credentials shared by every stack in an account, common tags, registry tokens, monitoring endpoints.

---

## When to use a variable set vs per-stack env vars

| Situation | Use |
| --- | --- |
| Variable is unique to one stack (e.g. `STACK_SLUG=my-app`) | Per-stack env var |
| Same value across many stacks (e.g. `AWS_REGION=us-east-1`) | Variable set |
| Secret rotates regularly and must update everywhere (e.g. `DATADOG_API_KEY`) | Variable set |
| Stack-specific override of an org-wide default | Per-stack env var (attached *after* the varset; per-stack always wins) |

When a variable name appears in both a variable set and a per-stack env var, the **per-stack value wins**. Same goes for two variable sets attached to one stack with overlapping names — the most recently attached set takes precedence.

---

## Creating a variable set

In the UI: **Settings → Variable Sets → New variable set**.

| Field | Required | Notes |
| --- | --- | --- |
| Name | yes | Shown in dropdowns; pick something descriptive (`aws-prod-creds`, `monitoring-keys`) |
| Description | no | Free-form context for teammates |

Save. The set starts empty — add variables next.

---

## Adding variables

Open the set's detail page and click **Add variable**.

| Field | Required | Notes |
| --- | --- | --- |
| Name | yes | The env var name as runner containers will see it (`AWS_REGION`, `DATADOG_API_KEY`) |
| Value | yes | The value. Stored encrypted in Crucible's vault when marked Secret. |
| Secret | toggle | When on, the value is masked in the UI after save and never returned via the API |

Secret values are encrypted with the same HKDF-derived key as stack env vars. With [BYOK](../operator-guide.md#byok--customer-managed-master-key) enabled, the master key is wrapped by your own KMS.

Plain (non-secret) values are stored unencrypted and are visible in the UI. Use plain for things like `AWS_REGION` or `LOG_LEVEL` where there's no harm in disclosure.

---

## Attaching a set to a stack

Two ways:

- **From the stack:** Stack detail → **Settings** → **Variable sets** → **Attach**. Pick from the dropdown.
- **From the set:** Variable set detail → **Attached stacks** → **Attach**. Pick stacks.

Attached sets take effect on the next run. Already-queued or in-flight runs use whatever was attached when they started.

---

## Updating a variable

Open the set → click the variable row → **Edit**.

Updating a value applies to **every attached stack** on the next run. There's no per-stack override at the variable level; if one stack needs a different value for a shared variable, set a per-stack env var with the same name (it will win).

Renaming a variable is allowed — the new name takes effect immediately for all attached stacks.

---

## Detaching a set

Detach is permission-checked at `member` level. From either the stack or the set:

- Stack detail → **Settings → Variable sets** → **×** on the row.
- Set detail → **Attached stacks** → **×** on the row.

Detaching removes the env vars from the stack on the next run. Already-running jobs are unaffected.

Deleting a variable set requires `admin` and is blocked if any stacks are still attached — detach first.

---

## Typical patterns

### Cloud account credentials

```text
aws-prod-creds
├── AWS_ACCESS_KEY_ID      (secret)
├── AWS_SECRET_ACCESS_KEY  (secret)
└── AWS_REGION=us-east-1   (plain)
```

Attach to every stack that deploys into the production AWS account. Rotating the key updates everywhere at once. For OIDC workload identity federation (recommended for production), you do not need this set at all — Crucible mints federated tokens automatically.

### Monitoring tokens

```text
observability
├── DATADOG_API_KEY      (secret)
├── DD_SITE=datadoghq.eu (plain)
└── DD_ENV=prod          (plain)
```

Attach to every stack that ships metrics to Datadog. One set, many stacks, single point of rotation.

### Common tags

```text
common-tags
├── TF_VAR_owner_team=platform        (plain)
├── TF_VAR_cost_center=engineering    (plain)
└── TF_VAR_data_classification=public (plain)
```

Using Terraform's `TF_VAR_*` convention to pass values into module variables. Attach to any stack whose modules accept these inputs.

### Per-environment overlays

```text
env-dev          env-staging       env-prod
├── LOG_LEVEL=   ├── LOG_LEVEL=    ├── LOG_LEVEL=
│   debug        │   info          │   warn
└── REPLICAS=1   └── REPLICAS=2    └── REPLICAS=4
```

Three sets, one per environment. Each stack gets attached to exactly one. To promote a value across environments, edit each set in turn — or use a single set with overlay logic in your Terraform.

---

## API

The same CRUD is available via the API for scripting:

```bash
# List
curl -H "Authorization: Bearer $TOKEN" \
  https://crucible.example.com/api/v1/variable-sets

# Create
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"aws-prod-creds","description":"prod account credentials"}' \
  https://crucible.example.com/api/v1/variable-sets

# Add a variable (PUT is upsert)
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value":"us-east-1","is_secret":false}' \
  https://crucible.example.com/api/v1/variable-sets/$SET_ID/vars/AWS_REGION

# Attach to a stack
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  https://crucible.example.com/api/v1/stacks/$STACK_ID/variable-sets/$SET_ID

# Detach
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://crucible.example.com/api/v1/stacks/$STACK_ID/variable-sets/$SET_ID
```

Variable *values* are never returned by `GET` endpoints — only names and metadata. Even with admin permissions, you cannot read back a secret value via the API. Treat the API as write-only for secret values.

---

## Auditing

Every variable set action is recorded in the audit log:

- `variable_set.created`
- `variable_set.updated`
- `variable_set.deleted`
- `variable_set.var_upserted`
- `variable_set.var_deleted`
- `variable_set.attached`
- `variable_set.detached`

Stream these to your SIEM via [SIEM streaming](../operator-guide.md#siem-audit-log-streaming) for full traceability.

---

## What's next

- [Team setup](team-setup.md) — control who can attach/detach sets via RBAC.
- [External secrets](external-secrets.md) — when you'd rather pull from AWS Secrets Manager / Vault / Bitwarden instead of keeping values in Crucible.
- [Tags](tags.md) — pair with variable sets to filter stacks by env or team.
