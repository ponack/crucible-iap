# Crucible IAP — Operator Guide

## Contents

1. [Requirements](#requirements)
2. [First-time deployment](#first-time-deployment)
3. [Configuration reference](#configuration-reference)
4. [Upgrading](#upgrading)
5. [Backup and restore](#backup-and-restore)
6. [Monitoring](#monitoring)
7. [Troubleshooting](#troubleshooting)

---

## Requirements

| Component      | Minimum                                          |
| -------------- | ------------------------------------------------ |
| Docker Engine  | 24+                                              |
| Docker Compose | v2.20+                                           |
| RAM            | 2 GB                                             |
| Disk           | 20 GB (state + plan artifacts grow over time)    |
| CPU            | 2 cores                                          |

Crucible runs entirely inside Docker. No other runtime dependencies are needed on the host.

---

## First-time deployment

### 1. Clone and configure

```bash
git clone https://github.com/ponack/crucible-iap.git
cd crucible-iap
cp .env.example .env
```

Edit `.env`. At minimum set:

```env
CRUCIBLE_BASE_URL=https://crucible.example.com
CRUCIBLE_SECRET_KEY=<random 32+ char string>
POSTGRES_PASSWORD=<strong password>
MINIO_SECRET_KEY=<strong password>
```

Generate a secret key:

```bash
openssl rand -hex 32
```

### 2. Choose an authentication method

**Option A — Local auth** (no IdP, single operator account):

```env
LOCAL_AUTH_ENABLED=true
LOCAL_AUTH_EMAIL=admin@example.com
LOCAL_AUTH_PASSWORD=<strong password>
```

**Option B — OIDC** (Authentik, Keycloak, Okta, GitHub, etc.):

```env
OIDC_ISSUER_URL=https://authentik.example.com/application/o/crucible/
OIDC_CLIENT_ID=crucible
OIDC_CLIENT_SECRET=<from your IdP>
OIDC_REDIRECT_URL=https://crucible.example.com/auth/callback
```

Both can be enabled simultaneously.

### 3. Start

```bash
docker compose up -d
```

Crucible will:
- Run database migrations automatically on first start
- Provision TLS via Let's Encrypt (set `CADDY_ACME_EMAIL` for production)
- Be available at `https://crucible.example.com`
- Expose Grafana at `https://crucible.example.com/grafana`

Set `GRAFANA_ADMIN_PASSWORD` before exposing Grafana publicly.

### 4. External reverse proxy

If you use your own Caddy, nginx, or Traefik:

```bash
docker compose --profile external-proxy up -d
```

See [`deploy/proxy-examples/`](../deploy/proxy-examples/) for ready-to-use configs including OPNsense Caddy plugin notes.

---

## Configuration reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `CRUCIBLE_BASE_URL` | yes | — | Public URL (e.g. `https://crucible.example.com`) |
| `CRUCIBLE_SECRET_KEY` | yes | — | JWT signing key — minimum 32 characters |
| `CRUCIBLE_ENV` | no | `production` | `production` or `development` |
| `POSTGRES_PASSWORD` | yes | — | PostgreSQL password |
| `MINIO_SECRET_KEY` | yes | — | MinIO root password |
| `LOCAL_AUTH_ENABLED` | no | `false` | Enable email/password login |
| `LOCAL_AUTH_EMAIL` | if local | — | Admin email for local auth |
| `LOCAL_AUTH_PASSWORD` | if local | — | Admin password for local auth |
| `OIDC_ISSUER_URL` | if OIDC | — | OIDC provider discovery URL |
| `OIDC_CLIENT_ID` | if OIDC | — | OIDC client ID |
| `OIDC_CLIENT_SECRET` | if OIDC | — | OIDC client secret |
| `OIDC_REDIRECT_URL` | if OIDC | — | Callback URL (`/auth/callback`) |
| `RUNNER_MAX_CONCURRENT` | no | `5` | Max parallel job containers |
| `RUNNER_JOB_TIMEOUT_MINUTES` | no | `60` | Per-job hard timeout |
| `RUNNER_MEMORY_LIMIT` | no | `2g` | Memory cap per runner container |
| `RUNNER_CPU_LIMIT` | no | `1.0` | CPU cap per runner container |
| `GRAFANA_ADMIN_USER` | no | `admin` | Grafana admin username |
| `GRAFANA_ADMIN_PASSWORD` | no | `changeme` | **Change this** |
| `CADDY_ACME_EMAIL` | no | — | Let's Encrypt email; blank = self-signed |

### Runtime settings (UI-configurable)

The following are managed via **Settings → Runner** and **Settings → Retention** in the UI and stored in the database — no restart required:

| Setting                 | Default        | Description                                                                                                          |
| ----------------------- | -------------- | -------------------------------------------------------------------------------------------------------------------- |
| Runner default image    | runner default | Docker image used when a stack has no custom image set                                                               |
| Max concurrent runs     | `5`            | Hard cap on parallel runner containers                                                                               |
| Job timeout             | `60 min`       | Per-job hard timeout; containers are killed after this                                                               |
| Memory / CPU limit      | `2g` / `1.0`   | Resource caps per runner container                                                                                   |
| Artifact retention days | `0` (forever)  | Plan files and run logs older than this are deleted daily. Set a value (e.g. `90`) to prevent unbounded MinIO growth |

### Provider caching

Crucible caches OpenTofu/Terraform provider binaries in MinIO under the `provider-cache/` prefix. Before each run, the worker checks for a cached copy of each provider and restores it to the runner workspace. After a successful `init`, any newly downloaded providers are uploaded back to the cache.

This is transparent to stacks — no configuration is required. The first run for a given provider version downloads from the registry; subsequent runs skip the download entirely. On slow registries or air-gapped environments the speedup is significant.

Cached providers are keyed by `provider-cache/<hostname>/<namespace>/<type>/<version>/<os>_<arch>/`. Entries are never automatically evicted — delete objects from MinIO manually if you need to force a fresh download.

### Custom run hooks

Each stack supports four optional bash lifecycle hooks, configured in **Stacks** → *stack name* → **Edit** → **Run hooks**:

| Hook | Runs |
| --- | --- |
| Pre-plan | Before `tofu plan` |
| Post-plan | After plan, before the artifact is uploaded |
| Pre-apply | Before `tofu apply` (after user confirmation) |
| Post-apply | After `tofu apply` completes successfully |

Hooks are stored as text in the database and injected into the runner container as environment variables (`CRUCIBLE_HOOK_PRE_PLAN`, `CRUCIBLE_HOOK_POST_PLAN`, `CRUCIBLE_HOOK_PRE_APPLY`, `CRUCIBLE_HOOK_POST_APPLY`). The entrypoint executes them with `bash -c`. A non-zero exit code fails the run and logs the error.

Hooks run with the same environment as the rest of the run (all stack env vars, cloud credentials, OIDC tokens). Use them for things like notifying external systems, validating prerequisites, or running custom linters.

### Org-level Cloud OIDC default

**Settings → General → Cloud OIDC Default** lets you configure a single OIDC federation identity that all stacks inherit. Supported providers: AWS, GCP, Azure.

When a run starts, the worker checks for a per-stack Cloud OIDC configuration first. If none is set, it falls back to the org default. Per-stack configuration always takes precedence.

This is useful when all (or most) of your stacks deploy to the same cloud account — configure the role ARN once in Settings and skip per-stack OIDC setup entirely. Stacks that need a different role can override it with their own configuration.

---

## Upgrading

Crucible runs database migrations automatically on startup. Upgrading is:

```bash
docker compose pull
docker compose up -d
```

Pull the new image first — the migration files are embedded in the binary, so the new image must be running before migrations are applied. Zero-downtime rolling upgrades are not yet supported — expect a few seconds of downtime during restart.

**Before upgrading:** read the release notes for breaking changes to `.env` variables.

### v0.2.x → v0.3.0

No `.env` changes required. The following runner-layer bugs are fixed in this release — all changes are in the ephemeral job container, not the API.

- **State backend empty-state handling (v0.2.7)** — fresh stacks with no prior state returned HTTP 500 instead of 204. Fixed by buffering the MinIO read before committing the response status. If you saw `giving up after 3 attempt(s)` in runner logs on a first run, upgrade resolves it.
- **Runner /tmp read-only (v0.2.8)** — provider plugins are extracted to `/tmp` at init time; the runner container's root filesystem is read-only. Fixed by mounting a 256 MB tmpfs at `/tmp` (in addition to the existing `/workspace` mount). Symptoms: `open /tmp/terraform-provider-*: read-only file system` in runner logs.
- **Runner tmpfs noexec (v0.2.9)** — the `/workspace` and `/tmp` tmpfs mounts were previously created with Docker's `TmpfsOptions` struct which does not expose mount flags, causing Docker to apply the default `noexec` flag. Provider binaries cannot execute under `noexec`. Fixed by switching to the `HostConfig.Tmpfs` string-option form with an explicit `exec` flag. Symptoms: `fork/exec /tmp/terraform-provider-*: permission denied` in runner logs.
- **Plan download auth (v0.3.0)** — the apply phase downloads the saved plan from the API using the runner's job JWT. The endpoint was previously only exposed under user auth (`/api/v1/runs/`). The runner JWT (audience `runner`) is not accepted there, so the download returned 401 and the apply failed. Fixed by adding `GET /api/v1/internal/runs/:id/plan` under the runner-auth group. Symptoms: `failed to download plan artifact` in runner logs immediately after confirming a run.

### v0.1.5 → v0.2.0

No `.env` changes required. Migrations 017 and 018 run automatically on startup:

- **017** — adds `is_secret` column to `stack_env_vars` (existing env vars default to `secret = true`)
- **018** — adds the missing `river_job_state_in_bitmask` function required by the River job queue. If you upgraded to v0.1.5 and runs were failing with `SQLSTATE 42883`, this migration fixes it.

New in v0.2.0:

- **Destroy runs** — trigger a `tofu destroy` from the stack detail page. The full plan is shown before anything is deleted; a name-confirmation modal and explicit approval gate are required.
- **Env var secret flag** — each stack environment variable can be marked as `Secret` (value masked in the UI, default) or `Plain` (value visible). Toggle the flag when adding or updating a variable.

---

## Backup and restore

### What to back up

| Data | Location | Method |
|------|----------|--------|
| All application state | PostgreSQL `crucible` database | `pg_dump` |
| Terraform state files | MinIO `crucible-state` bucket | MinIO client or S3 sync |
| Plan artifacts + logs | MinIO `crucible-artifacts` bucket | MinIO client or S3 sync |
| Configuration | `.env` file | Copy to secure location |

### PostgreSQL backup

```bash
docker compose exec postgres pg_dump -U crucible crucible | gzip > crucible_$(date +%Y%m%d).sql.gz
```

### PostgreSQL restore

```bash
gunzip -c crucible_20260101.sql.gz | docker compose exec -T postgres psql -U crucible crucible
```

### MinIO backup

Using the MinIO client (`mc`):

```bash
mc alias set crucible http://localhost:9000 minioadmin <MINIO_SECRET_KEY>
mc mirror crucible/crucible-state    ./backup/state
mc mirror crucible/crucible-artifacts ./backup/artifacts
```

### Full restore procedure

1. Stop the stack: `docker compose down`
2. Restore PostgreSQL data into the `postgres_data` volume
3. Restore MinIO data into the `minio_data` volume (or replay `mc mirror` in reverse)
4. Restore `.env`
5. Start: `docker compose up -d`

---

## Monitoring

Crucible ships Prometheus and Grafana. Access Grafana at `/grafana` (default credentials in `.env`).

### Key metrics

| Metric | Description |
|--------|-------------|
| `crucible_http_requests_total` | Request count by method, path, status |
| `crucible_http_request_duration_seconds` | Latency histogram |
| `crucible_runs_total` | Run completions by status and type |
| `crucible_queue_depth` | Pending River jobs |

### Health check

```bash
curl https://crucible.example.com/health
# {"status":"ok","db":"ok","uptime":"2h30m","version":"1.0.0"}
```

A `status` of `"degraded"` means the database is unreachable.

---

## Troubleshooting

### API keeps restarting

Check logs:
```bash
docker compose logs crucible-api --tail 50
```

Common causes:
- `CRUCIBLE_SECRET_KEY` shorter than 32 characters → extend it
- `MINIO_ENDPOINT` not reachable → ensure MinIO is healthy: `docker compose ps`
- `POSTGRES_PASSWORD` mismatch → check `.env` matches the volume's initialised password

### Migrations fail on startup

```bash
docker compose run --rm crucible-api migrate
```

If a migration has partially applied, you may need to roll back manually:
```bash
docker compose run --rm crucible-api migrate --down
```

### Runner containers not starting

Verify the Docker socket is mounted on the worker (not the API):
```bash
docker compose exec crucible-worker ls /var/run/docker.sock
```

Ensure the `crucible-runner` network exists:
```bash
docker network ls | grep crucible-runner
```

### Grafana shows no data

Verify Prometheus is scraping:
- Open `https://crucible.example.com/grafana`
- Go to **Explore** → select **Prometheus** datasource → query `up`
- If no data, check: `docker compose logs prometheus`

### Runner: "giving up after N attempt(s)" on state backend

The runner cannot reach the state backend before handing off to OpenTofu/Terraform. The pre-flight connectivity check in the runner reports the real HTTP status before the go-retryablehttp opaque error.

Common causes:

- **401/403** — `CRUCIBLE_SECRET_KEY` in the API does not match the key used when the runner JWT was minted. Redeploy API and runner from the same image/config.
- **000 / connection refused** — The `CRUCIBLE_API_URL` env var (injected by the dispatcher) is not reachable from inside the runner container. The runner container runs on the `crucible-runner` Docker network; the API must be reachable by its Docker service name (e.g. `http://crucible-api:8080`), not `localhost`. Verify: `docker compose exec crucible-api curl http://crucible-api:8080/health`.
- **500 on first run** — fixed in v0.2.7. A fresh stack with no prior state returned 500 instead of 204. Upgrade resolves it.

### Runner: "read-only file system" during provider install

Fixed in v0.2.8. OpenTofu/Terraform extracts provider binaries to `/tmp`. The runner container uses a read-only root filesystem; without an explicit `/tmp` tmpfs mount, writes fail. If you are running a pre-v0.2.8 image, upgrade.

Verify the mount is present in a running runner container:

```bash
docker inspect <runner-container-id> | grep -A5 Tmpfs
```

You should see `/tmp` and `/workspace` both listed.

### Runner: "permission denied" executing provider binary

Fixed in v0.2.9. Docker's `TmpfsOptions` struct does not expose mount flags, so the created tmpfs received the kernel default `noexec`. Provider binaries cannot be executed under `noexec`. Upgrade to v0.2.9+ resolves it; no config change needed.

Symptom: `fork/exec /tmp/terraform-provider-*: permission denied` appearing immediately after the provider is downloaded successfully.

### Runner: "failed to download plan artifact" on apply

Fixed in v0.3.0. The runner fetches the saved plan in the apply phase using its job JWT (audience `runner`). Before v0.3.0, the plan download endpoint was only exposed under user auth, which rejects the runner JWT. The run would succeed planning but fail immediately on confirm.

Ensure you are running v0.3.0+. The endpoint `GET /api/v1/internal/runs/:id/plan` was added in this release specifically for the runner apply phase.

### Run detail page buttons don't update after a run ends

Fixed in v0.3.0. The SSE log stream channel was never closed when the worker goroutine finished, so the browser kept a hung connection and never saw the terminal `[DONE]` event. Buttons (Cancel → Delete, plan → confirm/discard) stayed in their mid-run state until page reload.

Upgrade to v0.3.0+; no config change needed.
