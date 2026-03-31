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

| Component | Minimum |
|-----------|---------|
| Docker Engine | 24+ |
| Docker Compose | v2.20+ |
| RAM | 2 GB |
| Disk | 20 GB (state + plan artifacts grow over time) |
| CPU | 2 cores |

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

---

## Upgrading

Crucible uses automatic migrations. Upgrading is:

```bash
docker compose pull
docker compose up -d
```

The API container runs `migrate up` on startup before accepting traffic. Zero-downtime rolling upgrades are not yet supported — expect a few seconds of downtime during restart.

**Before upgrading:** read the release notes for breaking changes to `.env` variables.

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

Verify the Docker socket is mounted:
```bash
docker compose exec crucible-api ls /var/run/docker.sock
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
