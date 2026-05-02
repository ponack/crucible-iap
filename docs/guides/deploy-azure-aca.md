# Deploy Crucible IAP on Azure Container Apps

This guide walks through deploying Crucible IAP on Azure using managed services to replace each component of the default Docker Compose stack. The API runs as a Container App with managed HTTPS and auto-scaling. The worker runs on a plain VM because it needs Docker socket access to spawn IaC runs.

## Contents

1. [Architecture overview](#architecture-overview)
2. [Prerequisites](#prerequisites)
3. [Azure Database for PostgreSQL](#azure-database-for-postgresql)
4. [Azure Blob Storage](#azure-blob-storage)
5. [Azure Key Vault](#azure-key-vault)
6. [Container Apps — Crucible API](#container-apps--crucible-api)
7. [Azure VM — Crucible worker](#azure-vm--crucible-worker)
8. [Custom domain](#custom-domain)
9. [Verification](#verification)
10. [Scaling notes](#scaling-notes)

---

## Architecture overview

| Component | Azure service | Replaces | Notes |
| --- | --- | --- | --- |
| PostgreSQL | Azure Database for PostgreSQL Flexible Server | `postgres` container | Private VNet access, SSL required |
| Object storage | Azure Blob Storage (S3-compatible API) | MinIO container | Same MinIO client, different endpoint |
| API process | Azure Container Apps | `crucible-api` container | Managed HTTPS, auto-scale, no Docker socket |
| Worker process | Azure VM (Ubuntu 22.04) | `crucible-worker` container | Docker socket required for run execution |
| Secrets | Azure Key Vault | `.env` file | Managed identity for Container Apps |

**Why the worker cannot run in Container Apps:** The `crucible-worker` process spawns Docker containers to execute Terraform, Ansible, and Pulumi runs. This requires access to a Docker socket (`/var/run/docker.sock`). Azure Container Apps does not expose the Docker socket to containers, so the worker runs on a VM that has Docker installed directly. The API is fully stateless and has no such requirement.

**Azure Blob S3 compatibility:** Azure Blob Storage exposes an S3-compatible API when enabled on the storage account. Crucible's MinIO client works against this endpoint without any code changes — point `MINIO_ENDPOINT` at `<account>.blob.core.windows.net` and supply the storage account name and key as the access credentials.

---

## Prerequisites

- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) installed and logged in (`az login`)
- An active Azure subscription
- The following resource providers registered in your subscription:

```bash
az provider register --namespace Microsoft.App
az provider register --namespace Microsoft.OperationalInsights
az provider register --namespace Microsoft.DBforPostgreSQL
az provider register --namespace Microsoft.KeyVault
```

Set variables used throughout this guide:

```bash
RG=crucible-rg
LOCATION=westeurope
```

Create the resource group:

```bash
az group create --name $RG --location $LOCATION
```

---

## Azure Database for PostgreSQL

### Create the server

For development use Burstable B1ms (1 vCPU, 2 GiB). For production use General Purpose D2s_v3 (2 vCPU, 8 GiB) or larger.

```bash
az postgres flexible-server create \
  --name crucible-pg \
  --resource-group $RG \
  --location $LOCATION \
  --sku-name Standard_B1ms \
  --tier Burstable \
  --version 16 \
  --admin-user crucibleadmin \
  --admin-password '<strong-admin-password>' \
  --storage-size 32 \
  --public-access Disabled
```

`--public-access Disabled` puts the server on a managed VNet with no public endpoint. The worker VM will be placed on the same VNet (see [Azure VM — Crucible worker](#azure-vm--crucible-worker)).

For production, use `--sku-name Standard_D2s_v3 --tier GeneralPurpose`.

### Create the database and application user

Connect via the Azure Cloud Shell or `psql` from a machine with VNet access:

```sql
CREATE DATABASE crucible;
CREATE USER crucible WITH PASSWORD '<crucible-db-password>';
GRANT ALL PRIVILEGES ON DATABASE crucible TO crucible;
\c crucible
GRANT ALL ON SCHEMA public TO crucible;
```

### Note the connection string

```
postgres://crucible:<crucible-db-password>@crucible-pg.postgres.database.azure.com:5432/crucible?sslmode=require
```

The Flexible Server hostname follows the pattern `<server-name>.postgres.database.azure.com`. SSL is required — the `sslmode=require` parameter in the connection string is mandatory.

---

## Azure Blob Storage

### Create the storage account

Storage account names must be globally unique, 3–24 lowercase alphanumeric characters:

```bash
az storage account create \
  --name cruciblestore \
  --resource-group $RG \
  --location $LOCATION \
  --sku Standard_LRS \
  --min-tls-version TLS1_2 \
  --allow-blob-public-access false
```

### Enable S3-compatible API

S3 compatibility is enabled at the storage account level. Enable it via the portal (**Storage account** → **Settings** → **S3 compatible API** → toggle on) or with the CLI (preview extension):

```bash
az storage account update \
  --name cruciblestore \
  --resource-group $RG \
  --enable-hierarchical-namespace false
```

> **Note:** The S3-compatible API is available on storage accounts with Standard_LRS, Standard_ZRS, Standard_GRS, and Standard_RAGRS skus. Hierarchical namespace (ADLS Gen2) must be off.

Once enabled, the S3-compatible endpoint for your account is:

```
https://cruciblestore.blob.core.windows.net
```

### Create the blob containers

```bash
ACCOUNT=cruciblestore
KEY=$(az storage account keys list \
  --account-name $ACCOUNT \
  --resource-group $RG \
  --query '[0].value' -o tsv)

for CONTAINER in crucible-runs crucible-state crucible-registry; do
  az storage container create \
    --name $CONTAINER \
    --account-name $ACCOUNT \
    --account-key $KEY
done
```

### Get the storage account key

```bash
az storage account keys list \
  --account-name cruciblestore \
  --resource-group $RG \
  --query '[0].value' -o tsv
```

Save this value — it becomes `MINIO_SECRET_KEY` in the next step.

---

## Azure Key Vault

### Create the vault

```bash
az keyvault create \
  --name crucible-kv \
  --resource-group $RG \
  --location $LOCATION \
  --sku standard \
  --enable-rbac-authorization true
```

### Store secrets

```bash
KV=crucible-kv

# Database
az keyvault secret set --vault-name $KV --name database-url \
  --value 'postgres://crucible:<crucible-db-password>@crucible-pg.postgres.database.azure.com:5432/crucible?sslmode=require'

# Application
az keyvault secret set --vault-name $KV --name secret-key \
  --value '<64-random-chars>'

# Azure Blob / MinIO
az keyvault secret set --vault-name $KV --name minio-secret-key \
  --value '<storage-account-key>'
```

Generate the `SECRET_KEY` value:

```bash
openssl rand -hex 32
```

### Create a managed identity for Container Apps

```bash
az identity create \
  --name crucible-api-identity \
  --resource-group $RG
```

Get the principal ID:

```bash
IDENTITY_PRINCIPAL=$(az identity show \
  --name crucible-api-identity \
  --resource-group $RG \
  --query principalId -o tsv)

KV_ID=$(az keyvault show \
  --name crucible-kv \
  --resource-group $RG \
  --query id -o tsv)
```

Assign the `Key Vault Secrets User` role so the identity can read secrets:

```bash
az role assignment create \
  --assignee $IDENTITY_PRINCIPAL \
  --role "Key Vault Secrets User" \
  --scope $KV_ID
```

---

## Container Apps — Crucible API

### Create the Container Apps Environment

```bash
az containerapp env create \
  --name crucible-env \
  --resource-group $RG \
  --location $LOCATION
```

### Get the managed identity resource ID

```bash
IDENTITY_ID=$(az identity show \
  --name crucible-api-identity \
  --resource-group $RG \
  --query id -o tsv)
```

### Retrieve Key Vault secret URIs

```bash
DB_URL_URI=$(az keyvault secret show \
  --vault-name crucible-kv --name database-url \
  --query id -o tsv)

SECRET_KEY_URI=$(az keyvault secret show \
  --vault-name crucible-kv --name secret-key \
  --query id -o tsv)

MINIO_SECRET_URI=$(az keyvault secret show \
  --vault-name crucible-kv --name minio-secret-key \
  --query id -o tsv)
```

### Create the Container App

```bash
az containerapp create \
  --name crucible-api \
  --resource-group $RG \
  --environment crucible-env \
  --image ghcr.io/ponack/crucible-iap:latest \
  --user-assigned $IDENTITY_ID \
  --min-replicas 1 \
  --max-replicas 5 \
  --cpu 0.5 \
  --memory 1Gi \
  --target-port 8080 \
  --ingress external \
  --secrets \
    "database-url=keyvaultref:${DB_URL_URI},identityref:${IDENTITY_ID}" \
    "secret-key=keyvaultref:${SECRET_KEY_URI},identityref:${IDENTITY_ID}" \
    "minio-secret-key=keyvaultref:${MINIO_SECRET_URI},identityref:${IDENTITY_ID}" \
  --env-vars \
    "DATABASE_URL=secretref:database-url" \
    "SECRET_KEY=secretref:secret-key" \
    "CRUCIBLE_ENV=production" \
    "CRUCIBLE_BASE_URL=https://<container-app-fqdn>" \
    "MINIO_ENDPOINT=cruciblestore.blob.core.windows.net" \
    "MINIO_ACCESS_KEY=cruciblestore" \
    "MINIO_SECRET_KEY=secretref:minio-secret-key" \
    "MINIO_USE_SSL=true" \
    "MINIO_BUCKET_RUNS=crucible-runs" \
    "MINIO_BUCKET_STATE=crucible-state" \
    "MINIO_BUCKET_REGISTRY=crucible-registry"
```

### Get the Container App FQDN

```bash
az containerapp show \
  --name crucible-api \
  --resource-group $RG \
  --query properties.configuration.ingress.fqdn -o tsv
```

Update `CRUCIBLE_BASE_URL` with the real FQDN before deploying the worker:

```bash
az containerapp update \
  --name crucible-api \
  --resource-group $RG \
  --set-env-vars "CRUCIBLE_BASE_URL=https://<actual-fqdn>"
```

Container Apps provides a managed TLS certificate and HTTPS termination for external ingress automatically — no Application Gateway or certificate management needed for a basic setup.

---

## Azure VM — Crucible worker

The worker must run on a VM because it spawns Docker containers to execute IaC runs.

### Create the VM

Place the VM in the same VNet as the PostgreSQL Flexible Server so the worker can reach the database on its private endpoint:

```bash
# Get the VNet created by the Flexible Server
VNET_ID=$(az postgres flexible-server show \
  --name crucible-pg \
  --resource-group $RG \
  --query network.delegatedSubnetResourceId -o tsv | sed 's|/subnets/.*||')

SUBNET_ID=$(az postgres flexible-server show \
  --name crucible-pg \
  --resource-group $RG \
  --query network.delegatedSubnetResourceId -o tsv)

# Create a separate subnet for the worker VM
az network vnet subnet create \
  --name worker-subnet \
  --vnet-name $(az network vnet show --ids $VNET_ID --query name -o tsv) \
  --resource-group $RG \
  --address-prefix 10.0.2.0/24

WORKER_SUBNET=$(az network vnet subnet show \
  --vnet-name $(az network vnet show --ids $VNET_ID --query name -o tsv) \
  --resource-group $RG \
  --name worker-subnet \
  --query id -o tsv)
```

```bash
az vm create \
  --name crucible-worker-vm \
  --resource-group $RG \
  --location $LOCATION \
  --image Ubuntu2204 \
  --size Standard_B2s \
  --admin-username azureuser \
  --ssh-key-values ~/.ssh/id_rsa.pub \
  --subnet $WORKER_SUBNET \
  --public-ip-sku Standard
```

Standard_B2s (2 vCPU, 4 GiB) is sufficient for low to moderate run throughput. Use Standard_D4s_v3 or larger for heavy concurrent runs.

### Install Docker

SSH into the VM and install Docker:

```bash
ssh azureuser@<vm-public-ip>
```

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker azureuser
# Re-login or run: newgrp docker
```

### Pull the image

```bash
docker pull ghcr.io/ponack/crucible-iap:latest
```

### Create the systemd service

Create `/etc/systemd/system/crucible-worker.service`:

```ini
[Unit]
Description=Crucible IAP Worker
After=docker.service
Requires=docker.service

[Service]
Restart=always
RestartSec=10
ExecStartPre=-/usr/bin/docker rm -f crucible-worker
ExecStart=/usr/bin/docker run --rm \
  --name crucible-worker \
  --entrypoint crucible-worker \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e DATABASE_URL="postgres://crucible:<crucible-db-password>@crucible-pg.postgres.database.azure.com:5432/crucible?sslmode=require" \
  -e SECRET_KEY="<64-random-chars>" \
  -e CRUCIBLE_ENV=production \
  -e RUNNER_API_URL="https://<container-app-fqdn>" \
  -e MINIO_ENDPOINT="cruciblestore.blob.core.windows.net" \
  -e MINIO_ACCESS_KEY="cruciblestore" \
  -e MINIO_SECRET_KEY="<storage-account-key>" \
  -e MINIO_USE_SSL="true" \
  -e MINIO_BUCKET_RUNS="crucible-runs" \
  -e MINIO_BUCKET_STATE="crucible-state" \
  -e MINIO_BUCKET_REGISTRY="crucible-registry" \
  ghcr.io/ponack/crucible-iap:latest
ExecStop=/usr/bin/docker stop crucible-worker

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable crucible-worker
sudo systemctl start crucible-worker
sudo systemctl status crucible-worker
```

### Network access from the worker

The worker VM needs outbound access to:

| Destination | Why |
| --- | --- |
| PostgreSQL private endpoint (port 5432) | Job queue and application data |
| `https://<container-app-fqdn>` | Worker polls the API for queued runs |
| `https://cruciblestore.blob.core.windows.net` | Run artifacts and state |
| `https://ghcr.io` | Image pulls |

If the VM has a Network Security Group, ensure the outbound rules permit HTTPS (443) and PostgreSQL (5432).

### Add a PostgreSQL firewall rule for the worker

The Flexible Server private-access mode uses VNet integration rather than a firewall allowlist. If you used public access instead, add a rule for the worker's private IP:

```bash
az postgres flexible-server firewall-rule create \
  --name allow-worker \
  --resource-group $RG \
  --server-name crucible-pg \
  --start-ip-address <worker-private-ip> \
  --end-ip-address <worker-private-ip>
```

---

## Custom domain

Container Apps supports custom domains with managed certificates (no Application Gateway required).

### Add the custom domain

```bash
az containerapp hostname add \
  --name crucible-api \
  --resource-group $RG \
  --hostname crucible.example.com
```

Follow the DNS validation instructions in the output — you will be asked to create a CNAME and a TXT record at your DNS provider. Once DNS propagates, bind the managed certificate:

```bash
az containerapp hostname bind \
  --name crucible-api \
  --resource-group $RG \
  --hostname crucible.example.com \
  --certificate-type managed
```

Azure provisions a free managed certificate and handles renewal automatically. Update `CRUCIBLE_BASE_URL` to the custom domain:

```bash
az containerapp update \
  --name crucible-api \
  --resource-group $RG \
  --set-env-vars "CRUCIBLE_BASE_URL=https://crucible.example.com"
```

Also update `RUNNER_API_URL` in the worker's systemd unit file and restart the service:

```bash
sudo systemctl restart crucible-worker
```

---

## Verification

### Check Container App logs

```bash
az containerapp logs show \
  --name crucible-api \
  --resource-group $RG \
  --follow
```

### Health check

```bash
FQDN=$(az containerapp show \
  --name crucible-api \
  --resource-group $RG \
  --query properties.configuration.ingress.fqdn -o tsv)

curl -sf https://$FQDN/health
```

A healthy response returns HTTP 200 with `{"status":"ok"}`.

### Check worker service

On the VM:

```bash
sudo systemctl status crucible-worker
sudo journalctl -u crucible-worker -f
```

The worker logs should show it connecting to the API and polling for queued runs.

### Test a run

1. Log in to the Crucible UI at `https://<fqdn>`
2. Create a stack pointing at a simple Terraform module (e.g. a `null_resource`)
3. Trigger a run manually
4. Confirm the run completes and logs appear

If the run stays queued, the worker cannot reach the API — double-check `RUNNER_API_URL` and NSG outbound rules.

---

## Scaling notes

### Container Apps auto-scaling

Add an HTTP concurrency scaling rule to handle traffic spikes:

```bash
az containerapp update \
  --name crucible-api \
  --resource-group $RG \
  --scale-rule-name http-concurrency \
  --scale-rule-type http \
  --scale-rule-http-concurrency 50 \
  --min-replicas 1 \
  --max-replicas 10
```

This adds a replica for every 50 concurrent HTTP connections up to the maximum. The API is stateless so replicas are interchangeable.

### Worker throughput

Each worker VM processes one run at a time per CPU-bound capacity. To increase parallel run throughput:

- Provision additional VMs with the same `crucible-worker.service` configuration
- Each VM connects to the same PostgreSQL database; River's job queue handles deduplication and locking automatically
- Size VMs to match your typical run workload — Terraform runs are I/O-bound and fit well on Standard_B2s; Ansible runs against large inventories benefit from Standard_D4s_v3 or more memory

### PostgreSQL connection limits

Azure Database for PostgreSQL Flexible Server enforces connection limits per SKU:

| SKU | Max connections |
| --- | --- |
| Standard_B1ms | 50 |
| Standard_B2s | 100 |
| Standard_D2s_v3 | 200 |
| Standard_D4s_v3 | 400 |

Each Container Apps replica holds a small connection pool. If you scale to many replicas or worker VMs, use PgBouncer in front of the database or upgrade to a larger SKU. Set `max_connections` on the server if the default is too low for your replica count.
