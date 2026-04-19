# GCP Guide

This guide covers running OpenTofu with Google Cloud Platform inside Crucible IAP: credential injection, GCS remote state, and keyless Workload Identity Federation.

## Contents

1. [Providing GCP credentials](#providing-gcp-credentials)
2. [GCS remote state backend](#gcs-remote-state-backend)
3. [Minimal IAM permissions](#minimal-iam-permissions)
4. [Recommended policies for GCP stacks](#recommended-policies-for-gcp-stacks)

---

## Providing GCP credentials

### Option 1 — Service account key (simple, not recommended for production)

Generate a JSON key for a service account, then add it as a stack environment variable:

| Variable | Value |
| --- | --- |
| `GOOGLE_CREDENTIALS` | Contents of the service account JSON key file |
| `GOOGLE_PROJECT` | Your GCP project ID (e.g. `my-project-123456`) |

Mark `GOOGLE_CREDENTIALS` as **secret** so the value is stored encrypted and never shown in logs.

In your OpenTofu provider:

```hcl
provider "google" {
  project = var.gcp_project
  region  = var.gcp_region
}
```

OpenTofu picks up `GOOGLE_CREDENTIALS` automatically — no explicit credentials block needed.

### Option 2 — Workload Identity Federation via Crucible (recommended, keyless)

Crucible acts as an OIDC identity provider. The worker mints a short-lived OIDC token per run and exchanges it for a GCP access token via Workload Identity Federation — no service account key files, no long-lived credentials.

#### Per-stack configuration

Open **Stacks** → *stack name* → **Edit** → **Cloud OIDC** and set:

| Field | Value |
| --- | --- |
| Provider | `gcp` |
| Workload Identity Audience | The audience string for your WIF pool (e.g. `//iam.googleapis.com/projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL_ID/providers/PROVIDER_ID`) |
| Service account email | `crucible-runner@my-project-123456.iam.gserviceaccount.com` |

Crucible automatically sets `GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp-credentials.json` and writes an Application Default Credentials JSON file configured for Workload Identity Federation. No provider changes needed in your Terraform code.

#### Org-level default

To use the same service account across all stacks, configure it once in **Settings → General → Cloud OIDC Default** instead of repeating it per stack. Stacks with a per-stack Cloud OIDC config override the org default.

#### Setting up the Workload Identity Pool

In your GCP project, create a Workload Identity Pool and OIDC provider:

```bash
# Create the pool
gcloud iam workload-identity-pools create crucible-pool \
  --location="global" \
  --description="Crucible IAP runner" \
  --display-name="Crucible IAP"

# Create the OIDC provider
gcloud iam workload-identity-pools providers create-oidc crucible-provider \
  --location="global" \
  --workload-identity-pool="crucible-pool" \
  --issuer-uri="https://crucible.example.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.stack_slug=assertion.stack_slug" \
  --attribute-condition="attribute.stack_slug != ''"
```

Replace `https://crucible.example.com` with your `CRUCIBLE_BASE_URL`.

#### Bind the service account

```bash
PROJECT_NUMBER=$(gcloud projects describe my-project-123456 --format="value(projectNumber)")
POOL_ID="crucible-pool"

gcloud iam service-accounts add-iam-policy-binding \
  crucible-runner@my-project-123456.iam.gserviceaccount.com \
  --role="roles/iam.workloadIdentityUser" \
  --member="principalSet://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/${POOL_ID}/attribute.stack_slug/*"
```

This allows any Crucible stack to impersonate the service account. To restrict to a specific stack slug, replace the wildcard:

```
attribute.stack_slug/my-stack-slug
```

---

## GCS remote state backend

Store OpenTofu state in GCS for durability and cross-run sharing.

### Create the bucket

```bash
gcloud storage buckets create gs://my-org-tofu-state \
  --location=EU \
  --uniform-bucket-level-access
```

Enable versioning:

```bash
gcloud storage buckets update gs://my-org-tofu-state --versioning
```

### Configure the backend

```hcl
terraform {
  backend "gcs" {
    bucket = "my-org-tofu-state"
    prefix = "stacks/my-stack"
  }
}
```

The service account (or WIF token) used by the runner needs `roles/storage.objectAdmin` on the state bucket.

> **Note:** If you use Crucible's built-in HTTP state backend (the default), you do not need GCS at all — Crucible manages state in MinIO. Use GCS only when sharing state with tools outside of Crucible.

---

## Minimal IAM permissions

For a service account used by the Crucible runner, grant the minimum roles needed:

```bash
# State bucket access
gcloud storage buckets add-iam-policy-binding gs://my-org-tofu-state \
  --role="roles/storage.objectAdmin" \
  --member="serviceAccount:crucible-runner@my-project-123456.iam.gserviceaccount.com"

# Resource deployment (scope down to services you actually use)
gcloud projects add-iam-policy-binding my-project-123456 \
  --role="roles/editor" \
  --member="serviceAccount:crucible-runner@my-project-123456.iam.gserviceaccount.com"
```

Prefer custom roles with only the specific permissions your stacks need over the broad `roles/editor` — use `gcloud iam list-testable-permissions` or the IAM policy simulator to scope roles down after an initial deployment.

---

## Recommended policies for GCP stacks

| Policy | Type | What it does |
| --- | --- | --- |
| `require-tags.rego` | `post_plan` | Enforces required labels on GCP resources |
| `restrict-regions.rego` | `post_plan` | Blocks provider configs targeting disallowed regions |
| `no-destroy.rego` | `post_plan` | Hard-blocks any plan that would delete resources (production stacks) |
| `approval-for-destroy.rego` | `approval` | Requires human approval before applying any plan with deletes |

Attach these via **Stacks** → *stack name* → **Policies**, or mark them as org defaults to apply everywhere automatically.
