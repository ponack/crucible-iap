# AWS Guide

This guide covers the common patterns for running OpenTofu with AWS inside Crucible IAP: credential injection, remote state in S3, and a minimal IAM role.

## Contents

1. [Providing AWS credentials](#providing-aws-credentials)
2. [S3 remote state backend](#s3-remote-state-backend)
3. [Minimal IAM role](#minimal-iam-role)
4. [Recommended policies for AWS stacks](#recommended-policies-for-aws-stacks)

---

## Providing AWS credentials

Crucible does not manage cloud credentials directly — it passes environment variables into the OpenTofu process via stack environment variables. Set these on the stack in **Settings** → **Environment variables** (mark them as **secret** so they are stored encrypted and never shown in logs).

### Option 1 — Static IAM access keys (simple, not recommended for production)

| Variable | Value |
| --- | --- |
| `AWS_ACCESS_KEY_ID` | Your IAM user access key ID |
| `AWS_SECRET_ACCESS_KEY` | Your IAM user secret access key |
| `AWS_DEFAULT_REGION` | e.g. `eu-west-1` |

### Option 2 — IAM role assumption from instance profile

If your Crucible instance runs on AWS (EC2, ECS, or EKS), you can attach an IAM role directly to the compute and use role assumption — no long-lived keys needed.

1. Create an IAM role (see [Minimal IAM role](#minimal-iam-role)) with a trust policy that allows your instance profile to assume it
2. Set `AWS_DEFAULT_REGION` on the stack
3. Set `AWS_ROLE_ARN` to the role ARN
4. Add the following to your OpenTofu provider configuration:

```hcl
provider "aws" {
  region = var.aws_region

  assume_role {
    role_arn = var.role_arn
  }
}
```

Then in your stack's environment variables:

| Variable | Value |
| --- | --- |
| `TF_VAR_aws_region` | e.g. `eu-west-1` |
| `TF_VAR_role_arn` | `arn:aws:iam::123456789012:role/crucible-deploy` |

OpenTofu reads `TF_VAR_*` variables automatically, so no extra wiring is needed.

### Option 3 — OIDC federation via Crucible (recommended, keyless)

Crucible acts as an OIDC identity provider. The worker mints a short-lived OIDC token for each run and presents it to AWS STS `AssumeRoleWithWebIdentity` — no static credentials or instance profile required. This works regardless of where Crucible is hosted.

#### Per-stack configuration

Open **Stacks** → *stack name* → **Edit** → **Cloud OIDC** and set:

| Field | Value |
| --- | --- |
| Provider | `aws` |
| Role ARN | `arn:aws:iam::123456789012:role/crucible-deploy` |
| Session duration | e.g. `3600` (seconds) |

The worker injects `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN` for the assumed role. No provider changes needed in your Terraform code.

#### Org-level default

If all (or most) stacks deploy to the same AWS account, configure the role once in **Settings → General → Cloud OIDC Default** instead of repeating it per stack. Stacks without a per-stack Cloud OIDC config inherit the org default automatically. Per-stack always wins if both are set.

#### IAM trust policy for OIDC federation

Create an IAM OIDC provider for your Crucible instance, then use this trust policy on the role:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::123456789012:oidc-provider/crucible.example.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "crucible.example.com:aud": "sts.amazonaws.com"
        }
      }
    }
  ]
}
```

Replace `crucible.example.com` with your `CRUCIBLE_BASE_URL` hostname and `123456789012` with your AWS account ID.

---

## S3 remote state backend

Store OpenTofu state in S3 so it is shared across runs and survives worker restarts.

### Create the backend resources

```hcl
# backend-bootstrap/main.tf — run once, outside of Crucible
resource "aws_s3_bucket" "state" {
  bucket = "my-org-tofu-state"
}

resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_dynamodb_table" "lock" {
  name         = "my-org-tofu-locks"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }
}
```

### Configure the backend

In your stack's root module:

```hcl
terraform {
  backend "s3" {
    bucket         = "my-org-tofu-state"
    key            = "stacks/my-stack/terraform.tfstate"
    region         = "eu-west-1"
    dynamodb_table = "my-org-tofu-locks"
    encrypt        = true
  }
}
```

The `key` path is the object path within the bucket. Use a consistent naming convention — `stacks/<stack-name>/terraform.tfstate` works well.

> **Note:** Set `AWS_DEFAULT_REGION` (or `AWS_REGION`) on the stack environment so the S3 backend can authenticate. The backend uses the same credentials as the provider.

---

## Minimal IAM role

This policy grants the permissions needed for typical infrastructure management. Scope it down to only the services your stacks actually use.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "StateBackend",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket",
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:DeleteItem"
      ],
      "Resource": [
        "arn:aws:s3:::my-org-tofu-state",
        "arn:aws:s3:::my-org-tofu-state/*",
        "arn:aws:dynamodb:eu-west-1:*:table/my-org-tofu-locks"
      ]
    },
    {
      "Sid": "DeployResources",
      "Effect": "Allow",
      "Action": [
        "ec2:*",
        "iam:*",
        "s3:*",
        "rds:*",
        "elasticloadbalancing:*",
        "cloudwatch:*",
        "logs:*"
      ],
      "Resource": "*"
    }
  ]
}
```

Replace the `DeployResources` action list with only the services you need. For least-privilege IAM roles, generate the policy from a real plan using `aws iam simulate-principal-policy` or a tool like [iamlive](https://github.com/iann0036/iamlive).

### Trust policy (for assume-role from an EC2 instance profile)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

---

## Recommended policies for AWS stacks

These templates from [docs/policies/](../policies/) are particularly useful for AWS workloads.

| Policy | Type | What it does |
| --- | --- | --- |
| [instance-type-allowlist.rego](../policies/instance-type-allowlist.rego) | `post_plan` | Blocks EC2 instance types outside an approved list; warns on caution-tier types |
| [no-public-access.rego](../policies/no-public-access.rego) | `post_plan` | Blocks S3 buckets without public-access-block and security groups open to `0.0.0.0/0` |
| [restrict-regions.rego](../policies/restrict-regions.rego) | `post_plan` | Blocks AWS provider configs targeting disallowed regions |
| [require-tags.rego](../policies/require-tags.rego) | `post_plan` | Enforces `owner`, `environment`, and `cost-centre` tags on all resources |
| [no-destroy.rego](../policies/no-destroy.rego) | `post_plan` | Hard-blocks any plan that would delete resources (production stacks) |
| [approval-for-destroy.rego](../policies/approval-for-destroy.rego) | `approval` | Requires human approval before applying any plan with deletes |

Attach these to your AWS stacks in **Stacks** → *stack name* → **Policies**, or mark them as org defaults to apply them everywhere automatically.
