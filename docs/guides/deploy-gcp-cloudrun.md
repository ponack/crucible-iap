# Deploy Crucible IAP on GCP (Cloud Run + Compute Engine)

This guide walks through deploying Crucible IAP to Google Cloud Platform using managed services in place of the default Docker Compose stack. The API runs on Cloud Run (fully managed, automatic TLS); the worker runs on a Compute Engine VM because it needs Docker socket access to spawn IaC containers.

## Contents

1. [Architecture overview](#architecture-overview)
2. [Prerequisites](#prerequisites)
3. [Cloud SQL (PostgreSQL)](#cloud-sql-postgresql)
4. [GCS buckets and HMAC credentials](#gcs-buckets-and-hmac-credentials)
5. [Secret Manager](#secret-manager)
6. [Cloud Run — API](#cloud-run--api)
7. [Compute Engine VM — worker](#compute-engine-vm--worker)
8. [Custom domain](#custom-domain)
9. [Verification](#verification)
10. [Scaling notes](#scaling-notes)

---

## Architecture overview

| Component | GCP service | Why |
| --- | --- | --- |
| Crucible API | Cloud Run | Stateless HTTP server; Cloud Run handles TLS, auto-scaling, and zero-ops networking |
| Crucible worker | Compute Engine VM | Needs Docker socket to spawn runner containers — Cloud Run has no Docker socket |
| PostgreSQL | Cloud SQL (PostgreSQL 15) | Managed, private IP only, automated backups |
| Object storage | GCS (S3-compatible HMAC API) | Replaces MinIO; Crucible's MinIO client works against GCS with HMAC credentials |
| TLS | Cloud Run built-in | No separate load balancer or cert manager needed for the API |
| Secrets | Secret Manager | Centralised secret storage; mounted as env vars in Cloud Run |

The worker communicates back to the API over the public Cloud Run URL (HTTPS). The API and worker both reach Cloud SQL over private IP via a Serverless VPC Connector (Cloud Run) and direct private routing (GCE).

---

## Prerequisites

### Tools

- `gcloud` CLI — authenticated and pointed at your project: `gcloud auth login && gcloud config set project PROJECT_ID`
- Docker — for the initial image pull/verify step (optional)

### GCP APIs to enable

```bash
gcloud services enable \
  run.googleapis.com \
  sqladmin.googleapis.com \
  secretmanager.googleapis.com \
  compute.googleapis.com \
  vpcaccess.googleapis.com \
  servicenetworking.googleapis.com \
  storage.googleapis.com
```

### Project-level variables

Set these once in your shell — the commands throughout this guide reference them:

```bash
export PROJECT_ID=my-project-123456
export REGION=europe-west1
export ZONE=europe-west1-b
export INSTANCE_NAME=crucible-api
export WORKER_VM=crucible-worker
export SQL_INSTANCE=crucible-db
export VPC_NETWORK=default
```

---

## Cloud SQL (PostgreSQL)

### Create the instance

For development (cheapest option):

```bash
gcloud sql instances create $SQL_INSTANCE \
  --database-version=POSTGRES_15 \
  --tier=db-f1-micro \
  --region=$REGION \
  --no-assign-ip \
  --network=projects/$PROJECT_ID/global/networks/$VPC_NETWORK \
  --availability-type=zonal
```

For production, swap `db-f1-micro` for `db-n1-standard-1` (or larger) and add `--availability-type=regional` for high availability:

```bash
gcloud sql instances create $SQL_INSTANCE \
  --database-version=POSTGRES_15 \
  --tier=db-n1-standard-1 \
  --region=$REGION \
  --no-assign-ip \
  --network=projects/$PROJECT_ID/global/networks/$VPC_NETWORK \
  --availability-type=regional
```

`--no-assign-ip` disables the public IP. The instance is only reachable from within your VPC.

### Note the private IP

```bash
gcloud sql instances describe $SQL_INSTANCE \
  --format="value(ipAddresses[0].ipAddress)"
```

Save this as `CLOUD_SQL_IP` — you will need it for `DATABASE_URL`.

### Create the database and user

```bash
gcloud sql connect $SQL_INSTANCE --user=postgres
```

Inside the psql session:

```sql
CREATE DATABASE crucible;
CREATE USER crucible WITH PASSWORD 'your-strong-password';
GRANT ALL PRIVILEGES ON DATABASE crucible TO crucible;
\q
```

### Authorized network for the worker VM

The GCE worker connects to Cloud SQL over private IP within the same VPC — no authorized network entry is needed as long as both are on the same VPC. If the worker is on a different network, add a VPC peering or authorized network:

```bash
gcloud sql instances patch $SQL_INSTANCE \
  --authorized-networks=WORKER_EXTERNAL_IP/32
```

---

## GCS buckets and HMAC credentials

Crucible uses three buckets. Create them with uniform bucket-level access:

```bash
for BUCKET in crucible-runs crucible-state crucible-registry; do
  gcloud storage buckets create gs://${PROJECT_ID}-${BUCKET} \
    --location=$REGION \
    --uniform-bucket-level-access
done
```

> The bucket names above are prefixed with your project ID to ensure global uniqueness. Set `MINIO_BUCKET_RUNS`, `MINIO_BUCKET_STATE`, and `MINIO_BUCKET_REGISTRY` to match exactly.

### Create a service account for storage

```bash
gcloud iam service-accounts create crucible-storage \
  --display-name="Crucible IAP storage access"
```

### Grant storage access

```bash
for BUCKET in crucible-runs crucible-state crucible-registry; do
  gcloud storage buckets add-iam-policy-binding gs://${PROJECT_ID}-${BUCKET} \
    --role=roles/storage.objectAdmin \
    --member="serviceAccount:crucible-storage@${PROJECT_ID}.iam.gserviceaccount.com"
done
```

### Generate an HMAC key

HMAC keys allow the S3-compatible API. You must enable the HMAC key via the service account:

```bash
gcloud storage hmac create crucible-storage@${PROJECT_ID}.iam.gserviceaccount.com
```

The output includes an `accessId` and `secret`. Save them — the secret is shown only once.

```
accessId: GOOGXXXXXXXXXXXXXXXXXX
secret:   xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

These map to `MINIO_ACCESS_KEY` and `MINIO_SECRET_KEY` respectively.

---

## Secret Manager

Store all sensitive values in Secret Manager. Cloud Run can mount these as environment variables at deploy time without baking secrets into container images or gcloud commands.

### Create secrets

```bash
# Database URL
echo -n "postgres://crucible:your-strong-password@CLOUD_SQL_IP:5432/crucible?sslmode=require" \
  | gcloud secrets create crucible-database-url --data-file=-

# Secret key (generate 64 random chars)
openssl rand -hex 32 \
  | gcloud secrets create crucible-secret-key --data-file=-

# GCS HMAC credentials
echo -n "GOOGXXXXXXXXXXXXXXXXXX" \
  | gcloud secrets create crucible-minio-access-key --data-file=-

echo -n "your-hmac-secret" \
  | gcloud secrets create crucible-minio-secret-key --data-file=-
```

### Grant Cloud Run access to the secrets

Cloud Run runs as its own service account (create one now):

```bash
gcloud iam service-accounts create crucible-api-sa \
  --display-name="Crucible API Cloud Run"
```

Grant secret access:

```bash
for SECRET in crucible-database-url crucible-secret-key \
              crucible-minio-access-key crucible-minio-secret-key; do
  gcloud secrets add-iam-policy-binding $SECRET \
    --role=roles/secretmanager.secretAccessor \
    --member="serviceAccount:crucible-api-sa@${PROJECT_ID}.iam.gserviceaccount.com"
done
```

---

## Cloud Run — API

### Create a Serverless VPC Connector

The Cloud Run service needs to reach Cloud SQL at its private IP. Create a VPC connector in the same region:

```bash
gcloud compute networks vpc-access connectors create crucible-connector \
  --region=$REGION \
  --subnet=default \
  --subnet-project=$PROJECT_ID \
  --min-instances=2 \
  --max-instances=3
```

If your `default` subnet already has an IP range conflict, specify a dedicated `/28` CIDR instead:

```bash
gcloud compute networks vpc-access connectors create crucible-connector \
  --region=$REGION \
  --network=$VPC_NETWORK \
  --range=10.8.0.0/28 \
  --min-instances=2 \
  --max-instances=3
```

### Deploy the API

```bash
gcloud run deploy $INSTANCE_NAME \
  --image=ghcr.io/ponack/crucible-iap:latest \
  --region=$REGION \
  --port=8080 \
  --service-account=crucible-api-sa@${PROJECT_ID}.iam.gserviceaccount.com \
  --vpc-connector=crucible-connector \
  --vpc-egress=private-ranges-only \
  --min-instances=1 \
  --max-instances=10 \
  --memory=512Mi \
  --cpu=1 \
  --allow-unauthenticated \
  --set-env-vars="CRUCIBLE_ENV=production,MINIO_ENDPOINT=storage.googleapis.com,MINIO_USE_SSL=true,MINIO_BUCKET_RUNS=${PROJECT_ID}-crucible-runs,MINIO_BUCKET_STATE=${PROJECT_ID}-crucible-state,MINIO_BUCKET_REGISTRY=${PROJECT_ID}-crucible-registry" \
  --set-secrets="DATABASE_URL=crucible-database-url:latest,SECRET_KEY=crucible-secret-key:latest,MINIO_ACCESS_KEY=crucible-minio-access-key:latest,MINIO_SECRET_KEY=crucible-minio-secret-key:latest"
```

`--allow-unauthenticated` lets the public internet reach the API — authentication is handled by Crucible's own login system, not by Cloud Run IAM.

`--min-instances=1` keeps one instance warm so the first request after idle does not have a cold start. Remove it (or set to 0) for dev deployments where cost matters more than latency.

### Note the Cloud Run URL

```bash
gcloud run services describe $INSTANCE_NAME \
  --region=$REGION \
  --format="value(status.url)"
```

Set `CRUCIBLE_BASE_URL` to this value, then update the deployment:

```bash
gcloud run services update $INSTANCE_NAME \
  --region=$REGION \
  --set-env-vars="CRUCIBLE_BASE_URL=https://crucible-api-xxxxxxxxxx-ew.a.run.app"
```

---

## Compute Engine VM — worker

The worker must run on a VM because it spawns Docker containers to execute IaC runs. Cloud Run has no Docker socket.

### Create the VM

```bash
gcloud compute instances create $WORKER_VM \
  --zone=$ZONE \
  --machine-type=e2-standard-2 \
  --image-family=debian-12 \
  --image-project=debian-cloud \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-balanced \
  --network=$VPC_NETWORK \
  --scopes=cloud-platform \
  --service-account=crucible-storage@${PROJECT_ID}.iam.gserviceaccount.com \
  --tags=crucible-worker
```

`e2-standard-2` (2 vCPU, 8 GB) is a reasonable starting point. Each parallel IaC run gets its own container — size up if you want more concurrency on a single VM.

### Install Docker

```bash
gcloud compute ssh $WORKER_VM --zone=$ZONE -- "
  set -e
  sudo apt-get update -q
  sudo apt-get install -y ca-certificates curl gnupg
  sudo install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/debian/gpg \
    | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  echo \"deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
    https://download.docker.com/linux/debian \$(. /etc/os-release && echo \"\$VERSION_CODENAME\") stable\" \
    | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
  sudo apt-get update -q
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io
  sudo systemctl enable --now docker
"
```

### Pull the Crucible image

```bash
gcloud compute ssh $WORKER_VM --zone=$ZONE -- "
  sudo docker pull ghcr.io/ponack/crucible-iap:latest
"
```

### Create the environment file

SSH into the VM and create `/etc/crucible-worker.env`:

```bash
gcloud compute ssh $WORKER_VM --zone=$ZONE
```

Inside the VM:

```bash
sudo tee /etc/crucible-worker.env > /dev/null <<EOF
CRUCIBLE_ENV=production
DATABASE_URL=postgres://crucible:your-strong-password@CLOUD_SQL_IP:5432/crucible?sslmode=require
SECRET_KEY=your-64-char-secret-key
RUNNER_API_URL=https://crucible-api-xxxxxxxxxx-ew.a.run.app
MINIO_ENDPOINT=storage.googleapis.com
MINIO_ACCESS_KEY=GOOGXXXXXXXXXXXXXXXXXX
MINIO_SECRET_KEY=your-hmac-secret
MINIO_USE_SSL=true
MINIO_BUCKET_RUNS=my-project-123456-crucible-runs
MINIO_BUCKET_STATE=my-project-123456-crucible-state
MINIO_BUCKET_REGISTRY=my-project-123456-crucible-registry
EOF
sudo chmod 600 /etc/crucible-worker.env
```

Replace all placeholder values with your actual project IDs, IP addresses, and credentials.

### Create a systemd unit

```bash
sudo tee /etc/systemd/system/crucible-worker.service > /dev/null <<'EOF'
[Unit]
Description=Crucible IAP worker
After=docker.service
Requires=docker.service

[Service]
Restart=always
RestartSec=10
ExecStartPre=-/usr/bin/docker rm -f crucible-worker
ExecStart=/usr/bin/docker run \
  --name crucible-worker \
  --rm \
  --env-file /etc/crucible-worker.env \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --entrypoint crucible-worker \
  ghcr.io/ponack/crucible-iap:latest
ExecStop=/usr/bin/docker stop crucible-worker

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable crucible-worker
sudo systemctl start crucible-worker
```

The `-v /var/run/docker.sock:/var/run/docker.sock` mount gives the worker the Docker socket it needs to spawn runner containers.

### Verify the worker is running

```bash
sudo systemctl status crucible-worker
sudo journalctl -u crucible-worker -f
```

---

## Custom domain

Cloud Run provides a `*.run.app` URL automatically. To use your own domain (`crucible.example.com`), verify the domain and create a domain mapping:

### Verify domain ownership

```bash
gcloud domains verify crucible.example.com
```

Follow the DNS TXT record instructions. Once verified, the domain appears in your project's verified domains.

### Create the domain mapping

```bash
gcloud run domain-mappings create \
  --service=$INSTANCE_NAME \
  --domain=crucible.example.com \
  --region=$REGION
```

### Update DNS

```bash
gcloud run domain-mappings describe \
  --domain=crucible.example.com \
  --region=$REGION
```

The output shows the CNAME or A/AAAA records to add at your DNS provider. Cloud Run provisions a managed TLS certificate automatically once DNS propagates.

### Update CRUCIBLE_BASE_URL

```bash
gcloud run services update $INSTANCE_NAME \
  --region=$REGION \
  --set-env-vars="CRUCIBLE_BASE_URL=https://crucible.example.com"
```

And update `RUNNER_API_URL` in `/etc/crucible-worker.env` on the GCE VM, then restart:

```bash
sudo systemctl restart crucible-worker
```

---

## Verification

### Health check

```bash
curl -s https://crucible.example.com/health | jq .
```

Expected response:

```json
{"status": "ok"}
```

### API logs (Cloud Run)

```bash
gcloud run services logs read $INSTANCE_NAME \
  --region=$REGION \
  --limit=50
```

Or stream live:

```bash
gcloud beta run services logs tail $INSTANCE_NAME \
  --region=$REGION
```

### Worker logs (Compute Engine)

```bash
gcloud compute ssh $WORKER_VM --zone=$ZONE -- \
  "sudo journalctl -u crucible-worker --since '10 min ago'"
```

### Test a run

1. Open `https://crucible.example.com` in a browser and complete initial setup
2. Create a stack pointing to a simple OpenTofu repo (e.g. the [crucible-quickstart](https://github.com/ponack/crucible-quickstart) template)
3. Trigger a plan — the worker picks up the job, spawns a runner container, and streams logs back to the UI
4. Confirm the run completes and logs appear in the **Runs** view

---

## Scaling notes

### API (Cloud Run)

Cloud Run scales the API automatically — from the `--min-instances` floor up to `--max-instances`. Each Cloud Run instance is stateless, so horizontal scaling requires no extra configuration. Monitor **Cloud Run metrics** in the GCP console to tune concurrency and instance counts.

### Worker (Compute Engine)

Each GCE worker runs jobs serially by default (one runner container at a time). To increase parallel run capacity:

- **Scale up** — use a larger machine type (`e2-standard-4`, `e2-standard-8`) and set a higher concurrency limit in Crucible's worker config
- **Scale out** — provision additional GCE VMs using the same systemd unit and environment file. All workers read from the same River job queue in Cloud SQL and pick up jobs independently. No load balancer or coordination layer is needed.

A managed instance group (MIG) with a custom autoscaler works well for variable workloads: scale on Cloud SQL queue depth using a custom metric.

### Database connection pooling

Each Cloud Run instance and each worker maintains its own connection pool. At high Cloud Run instance counts, the total open connections can exceed Cloud SQL limits.

Two options:

| Option | How | Trade-off |
| --- | --- | --- |
| Cloud SQL Proxy sidecar | Add `cloud-sql-proxy` as a Cloud Run sidecar container (Cloud Run sidecar feature in GA) | Zero-ops, native GCP integration |
| PgBouncer on the worker VM | Run PgBouncer in a Docker container on the GCE VM, point both the worker and API (via an internal load balancer or private IP) at it | More control; single point of failure unless replicated |

For most deployments, start without pooling and add it only when Cloud SQL reports connection saturation. `db-n1-standard-1` supports up to 200 concurrent connections — sufficient for moderate API traffic and several workers.
