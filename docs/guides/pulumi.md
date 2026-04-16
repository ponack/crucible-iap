# Guide: Running Pulumi Programs with Crucible IAP

This guide walks through setting up a GitOps workflow for Pulumi using Crucible IAP. By the end you will have a stack that runs `pulumi preview` on every push, requires manual confirmation before `pulumi up`, and stores state in Crucible's built-in MinIO backend — no Pulumi Cloud account needed.

The example provisions a random name and port (no cloud credentials required) so you can learn the workflow before connecting real infrastructure.

---

## How Pulumi runs map to Crucible's lifecycle

| Concept | OpenTofu | Pulumi |
| --- | --- | --- |
| Plan phase | `tofu plan` | `pulumi preview --diff` |
| Apply phase | `tofu apply` | `pulumi up --yes` |
| Destroy | `tofu plan -destroy` → `tofu apply` | `pulumi preview --destroy` → `pulumi up --yes --destroy` (actually `pulumi destroy --yes`) |
| State | Crucible HTTP backend | Crucible MinIO (S3-compatible DIY backend) |
| Change summary | structured JSON plan | parsed from `pulumi preview --json` |

Crucible captures the `pulumi preview --diff` output as the plan artifact. The change summary (`create` / `update` / `delete` counts) is parsed from the JSON preview and shown in the run summary and PR comments.

---

## Prerequisites

- Crucible IAP v0.4.9+ running (see [operator-guide.md](../operator-guide.md))
- A Git repository (GitHub, GitLab, or Gitea) you can push to
- Node.js installed locally (for `pulumi new` and local testing)
- Pulumi CLI installed locally for project initialization

---

## 1. Create a Pulumi project

Install the Pulumi CLI locally if you haven't already:

```bash
curl -fsSL https://get.pulumi.com | sh
```

Create a new repository named `crucible-pulumi-demo` and initialize a TypeScript project:

```bash
mkdir crucible-pulumi-demo && cd crucible-pulumi-demo
git init
pulumi new typescript --name crucible-demo --stack dev --yes
```

This creates:

```text
crucible-pulumi-demo/
├── index.ts
├── package.json
├── tsconfig.json
├── Pulumi.yaml
└── Pulumi.dev.yaml
```

> **Stack name:** Crucible uses the stack name `crucible-<stack-id>` by default. Override with `CRUCIBLE_PULUMI_STACK` on the stack if you want a specific name (e.g. `production`). The `dev` stack you created locally is for local testing only — Crucible creates its own stack in MinIO.

### `index.ts`

Replace the contents with a simple program that requires no cloud credentials:

```typescript
import * as random from "@pulumi/random";

const name = new random.RandomPet("service-name", {
    length: 2,
    separator: "-",
});

const port = new random.RandomInteger("service-port", {
    min: 1024,
    max: 9999,
});

export const serviceName = name.id;
export const servicePort = port.result;
```

Install the random provider:

```bash
npm install @pulumi/random
```

### `Pulumi.yaml`

Crucible auto-detects this file in the project root. The runtime must be declared:

```yaml
name: crucible-demo
runtime: nodejs
description: Crucible IAP Pulumi quick-start
```

Commit and push both files to `main`.

---

## 2. Create the stack in Crucible

**Stacks → New Stack**:

| Field | Value |
| --- | --- |
| Name | `pulumi-demo` |
| Tool | `pulumi` |
| Repo URL | your repository URL |
| Branch | `main` |
| Working directory | `/` |
| Auto-apply | off |

---

## 3. Add required environment variables

Stack detail → **Environment Variables**:

| Key | Value | Secret |
| --- | --- | --- |
| `PULUMI_CONFIG_PASSPHRASE` | any strong passphrase | **yes** |

`PULUMI_CONFIG_PASSPHRASE` is required — Pulumi uses it to encrypt stack config and secrets at rest. Crucible stores it encrypted in its own vault; it is injected into the runner container at job time and never logged.

> **Keep this passphrase safe.** If you lose it, the stack state stored in MinIO becomes inaccessible. Write it down or store it in your password manager.

The MinIO backend credentials are injected automatically — you do not need to set `AWS_ACCESS_KEY_ID`, `PULUMI_BACKEND_URL`, or similar variables.

---

## 4. Connect the webhook (optional)

Stack detail page → copy **Webhook URL** and **Webhook Secret**.

**GitHub**: Repository → Settings → Webhooks → Add webhook
- Content type: `application/json`
- Events: **Pushes** and **Pull requests**

Without a webhook, use **Trigger proposed run** / **Trigger tracked run** manually from the stack detail page.

---

## 5. Trigger your first run

Stack detail → **Trigger tracked run**.

The runner executes `pulumi install` (installs npm dependencies) then `pulumi preview --diff`. A first run on a clean stack looks like:

```text
Updating (crucible-<stack-id>):
     Type                       Name            Plan
 +   pulumi:pulumi:Stack        crucible-demo   create
 +   ├─ random:index:RandomPet  service-name    create
 +   └─ random:index:RandomInteger service-port create

Outputs:
  + serviceName: output<string>
  + servicePort: output<int>

Resources:
    + 2 to create
```

Status moves to `unconfirmed`. Review the preview in the **Plan** tab, then click **Confirm**. The runner applies:

```text
Outputs:
    serviceName: "happy-lemur"
    servicePort: 7341

Resources:
    + 2 created

Duration: 3s
```

State is stored in Crucible's MinIO bucket (`crucible-state`) under the Pulumi stack path.

---

## 6. Subsequent runs and drift

Push a change to `index.ts` — for example, change `separator` from `"-"` to `"_"`. Crucible plans the change automatically:

```text
~ random:index:RandomPet  service-name  update

Resources:
    ~ 1 to update
```

Confirm to apply. The resource is updated in place.

---

## Using a real cloud provider

Replace the `@pulumi/random` program with any Pulumi provider. The pattern is the same — add provider credentials as secret env vars on the stack.

### AWS example

Add stack env vars:

| Key | Value | Secret |
| --- | --- | --- |
| `AWS_ACCESS_KEY_ID` | your IAM access key | yes |
| `AWS_SECRET_ACCESS_KEY` | your IAM secret key | yes |
| `AWS_REGION` | `us-east-1` | no |

```typescript
import * as aws from "@pulumi/aws";

const bucket = new aws.s3.BucketV2("my-bucket", {
    bucket: "my-crucible-demo-bucket",
});

export const bucketName = bucket.id;
```

### GCP example

Add `GOOGLE_CREDENTIALS` (service account JSON key, base64-encoded) as a secret env var and use `@pulumi/gcp`.

### Azure example

Add `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID` as secret env vars and use `@pulumi/azure-native`.

---

## Advanced options

### Override the stack name

By default Crucible uses `crucible-<stack-id>` as the Pulumi stack name. To use a specific name (e.g. separate production and staging stacks with the same program):

| Variable | Value |
| --- | --- |
| `CRUCIBLE_PULUMI_STACK` | `production` |

### Use your own state backend

To use AWS S3, GCS, or another S3-compatible store instead of the built-in MinIO:

| Variable | Value |
| --- | --- |
| `PULUMI_BACKEND_URL` | `s3://my-bucket?region=us-east-1` |
| `AWS_ACCESS_KEY_ID` | your access key |
| `AWS_SECRET_ACCESS_KEY` | your secret key |

When `PULUMI_BACKEND_URL` is set on the stack, Crucible skips the automatic MinIO backend configuration entirely.

### Python programs

The runner image includes Python 3. Python Pulumi programs work without any additional setup:

```text
crucible-pulumi-demo/
├── __main__.py
├── requirements.txt
└── Pulumi.yaml
```

```yaml
# Pulumi.yaml
name: crucible-demo
runtime: python
```

```text
# requirements.txt
pulumi>=3.0.0,<4.0.0
pulumi-aws>=6.0.0,<7.0.0
```

### Go programs

Go Pulumi programs compile to a binary at run time. The runner image includes `go` via the standard Alpine packages — add `go` to your `Pulumi.yaml` runtime. The `pulumi install` step handles the build.

### Using Pulumi config values

Set config values as stack env vars using the `PULUMI_CONFIG_<KEY>` pattern, or encrypt them with `pulumi config set --secret` locally and commit the encrypted `Pulumi.<stack>.yaml`. The passphrase you set as `PULUMI_CONFIG_PASSPHRASE` decrypts them at run time.

---

## Troubleshooting

### "PULUMI_CONFIG_PASSPHRASE is required"

The passphrase is not set on the stack. Go to Stack detail → Environment Variables → add `PULUMI_CONFIG_PASSPHRASE` as a secret.

### "failed to open bucket" / S3 connection error

The runner could not reach MinIO. In the default Docker Compose setup, this is usually a network issue. Verify:
1. The `crucible-runner` Docker network exists: `docker network ls | grep crucible-runner`
2. MinIO is healthy: `docker compose ps minio`
3. The runner container can reach `minio:9000` — check that `minio` is attached to the runner network in `docker-compose.yml`

### "passphrase must be the same as the last time the stack was updated"

The `PULUMI_CONFIG_PASSPHRASE` value was changed after the stack was first created. The passphrase is baked into the state encryption — it cannot be rotated without re-creating the stack. If you need to change it, destroy the stack first, then recreate it with the new passphrase.

### "no Pulumi.yaml found"

Pulumi requires a `Pulumi.yaml` in the working directory. Check the **Working directory** on the stack matches where `Pulumi.yaml` lives in your repository. Leave it as `/` if the file is in the repo root.

### Preview succeeds but apply fails on first run

This typically means the program depends on a resource that was not available during preview (e.g. an async cloud resource that returns a different value on apply than preview showed). This is expected Pulumi behaviour and not specific to Crucible. Review the apply error in the run log.
