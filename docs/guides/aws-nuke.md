# AWS Account Nuke with Crucible IAP

This guide shows how to automate sandbox AWS account cleanup using [aws-nuke](https://github.com/ekristen/aws-nuke) orchestrated by Crucible IAP. Three stacks — `nuke`, `prep`, and `nuke-run` — form a self-resetting demo loop: nuke cleans the account, prep automatically re-provisions test resources afterwards.

A reference implementation is available at [ponack/homelab-aws](https://github.com/ponack/homelab-aws).

## Architecture

```text
nuke  (one-time setup, then lock)

prep ──► nuke-run
```

| Stack | Purpose |
| ----- | ------- |
| `nuke/` | Creates `aws-nuke-role` in the target account with `AdministratorAccess`. Apply once and lock. |
| `prep/` | Provisions test resources — one protected (survives nuke) and one target (gets deleted). |
| `nuke-run/` | Downloads aws-nuke and runs it. Downstream of `prep` so the account is re-provisioned automatically after each nuke. |

## Prerequisites

- Two AWS accounts: a **management** account (where Crucible runs) and a **target** sandbox account
- OIDC federation configured in Crucible for the management account (see the [AWS guide](aws.md))
- A role in the management account — e.g. `crucible-nuke-run` — that Crucible's OIDC provider can assume

## Cross-Account IAM Flow

```text
Crucible runner (management account)
  └─ OIDC JWT → crucible-nuke-run role
       └─ sts:AssumeRole ──► aws-nuke-role (target account)
                                 └─ AdministratorAccess on target account
```

The `nuke/` stack creates `aws-nuke-role` in the target account and sets its trust policy to allow assumption from your management account role.

### Why AdministratorAccess — and why that is appropriate here

aws-nuke must enumerate and delete every resource type across a whole AWS account. That means calling hundreds of service APIs (`ec2:Describe*`, `s3:DeleteBucket`, `iam:DeleteRole`, `rds:DeleteDBInstance`, …). A least-privilege policy that covered every API aws-nuke can invoke would be enormous, brittle (it would break every time aws-nuke adds support for a new resource type), and would still be semantically equivalent to "delete everything in this account." AWS themselves grant `AdministratorAccess` to automation roles of this kind — Control Tower's own `AWSControlTowerExecution` role is the canonical example.

The safety controls live in the *trust boundary*, not in the policy:

| Control | How it is enforced |
| ------- | ------------------ |
| **Account isolation** | `aws-nuke-role` only exists in the sandbox. Management and production accounts are permanently blocklisted in `nuke-config.yaml.tpl`. |
| **Trust policy principal** | Only the specific `crucible-nuke-run` role ARN in the management account can assume `aws-nuke-role`. No wildcard principals. |
| **OIDC chain** | Crucible mints a short-lived JWT per run; the management account role is only assumable by Crucible's OIDC provider — not by a human or an access key. |
| **Stack locked** | The `nuke/` stack is locked in Crucible after the first apply, preventing accidental changes to the trust policy or role name. |
| **Dry-run gate** | `TF_VAR_dry_run` defaults to `true`; changing it to `false` requires an explicit stack-config edit. |

### Keeping the trust policy tight

The `nuke/` stack trust policy should be as narrow as possible. Apply these constraints:

#### 1. Exact principal — no wildcards

```json
"Principal": {
  "AWS": "arn:aws:iam::<management-account-id>:role/crucible-nuke-run"
}
```

Never use `"AWS": "arn:aws:iam::<management-account-id>:root"` — that allows any principal in the management account to assume the role.

#### 2. Require an ExternalId

An `ExternalId` condition prevents confused-deputy attacks — if the management account role were ever compromised and used to call `sts:AssumeRole` on behalf of a third party, the third party would not know the `ExternalId` and the assume would fail.

```json
"Condition": {
  "StringEquals": {
    "sts:ExternalId": "<a-random-uuid-you-choose>"
  }
}
```

Store this value as a Secret variable in the `nuke-run/` stack and pass it to `aws-nuke` via `AWS_ASSUME_ROLE_EXTERNAL_ID`.

#### 3. Scope to your management account with SourceAccount

As defence-in-depth alongside the principal ARN:

```json
"Condition": {
  "StringEquals": {
    "aws:SourceAccount": "<management-account-id>",
    "sts:ExternalId": "<a-random-uuid-you-choose>"
  }
}
```

#### 4. Review the trust policy after every nuke stack apply

The apply output will show the full trust document. Confirm the `Principal` and `Condition` blocks match what you expect before locking the stack.

## Step 1 — Apply `nuke/` (once)

Create a Crucible stack pointing at `nuke/`, running **in the target account**.

| Variable | Value |
| -------- | ----- |
| `TF_VAR_account_id` | Target account ID |
| `TF_VAR_trusted_principal_arn` | ARN of the management account role (e.g. `arn:aws:iam::<mgmt-id>:role/crucible-nuke-run`) |

After the apply succeeds, **lock the stack** in Crucible (Settings → Lock stack). The role only needs to exist once.

## Step 2 — Apply `prep/`

Create a Crucible stack pointing at `prep/`, running in the target account. This creates:

- A **protected VPC** (tagged `crucible-nuke-protect=true`) containing `nuke-test-protected` — this instance and all its networking survive the nuke
- A **target VPC** (no protect tags) containing `nuke-test-target` — this gets deleted by the nuke

Keeping protected and target resources in separate VPCs is essential: a protected instance's ENI would permanently block deletion of a shared VPC.

## Step 3 — Configure `nuke-run/`

Create a Crucible stack pointing at `nuke-run/`, running **in the management account**.

Set these environment variables in Crucible (Stack → Settings → Environment Variables):

| Variable | Example | Notes |
| -------- | ------- | ----- |
| `TF_VAR_nuke_role_arn` | `arn:aws:iam::<target-id>:role/aws-nuke-role` | Created by the `nuke/` stack |
| `TF_VAR_management_account_id` | `<management-account-id>` | Permanently blocklisted — can never be nuked |
| `TF_VAR_dry_run` | `true` | Keep `true` until you've verified the dry-run output |
| `TF_VAR_key_pair_name` | `my-key` | EC2 key pair to preserve (leave empty to skip) |

Mark `TF_VAR_nuke_role_arn` and `TF_VAR_management_account_id` as **Secret** so the account IDs are never stored in git.

On the **Dependencies** tab, add `prep` as an upstream stack so `prep` auto-runs after each nuke completes.

## Step 4 — Verify the dry run

Trigger `nuke-run` with `TF_VAR_dry_run=true` (the default). The run log will list every resource that *would* be deleted.

Check the output for:

- `nuke-test-protected` → `filtered by config` ✓
- `nuke-test-target` → `would remove` ✓
- `StackSet-AWSControlTower*` CloudFormation stacks → `filtered by config` ✓
- `homelab-tfstate` S3 bucket → `filtered by config` ✓
- `aws-nuke-role`, `AWSControlTowerExecution`, `AWSReservedSSO_*` roles → `filtered by config` ✓

When the summary reads `0 nukeable`, proceed to the live run.

## Step 5 — Live run

Change `TF_VAR_dry_run` to `false` and trigger `nuke-run`. aws-nuke deletes everything unfiltered, then Crucible's dependency system automatically triggers `prep` to reprovision the test resources.

## The reset loop

Once set up, resetting the sandbox for a demo is a single action:

1. Trigger `nuke-run` (with `dry_run=false`)
2. Crucible auto-triggers `prep` after nuke completes
3. Account is clean and test resources are re-provisioned

## Troubleshooting

### Run times out after 60 minutes (dry run)

aws-nuke scans every AWS service by default, including hundreds of legacy/deprecated services. Use `resource-types: targets:` in `nuke-config.yaml.tpl` to limit scanning to types you actually use. Without this, scan time across multiple regions routinely exceeds 60 minutes before any deletion occurs.

### Live run times out in additional regions

Reduce `regions:` in the nuke config to only the regions where your stacks actually deploy resources, plus `global`. Fewer regions = fewer retry rounds = faster runs.

### VPC deletion stuck (in-use error)

A protected EC2 instance's ENI keeps the VPC in-use permanently. The solution is separate VPCs for protected and target resources — the `prep/` reference implementation does this.

### IAM Control Tower roles failing

aws-nuke v3 uses `type: glob` for pattern matching (not `type: regex`). Control Tower role filters must use glob syntax:

```yaml
IAMRole:
  - type: glob
    value: "aws-controltower-*"
  - type: glob
    value: "AWSReservedSSO_*"
```

### Default VPC in us-east-2 (or other regions) fails

Add `IsDefault`, `DefaultVPC`, and `DefaultForAz` property filters for all default VPC resource types:

```yaml
EC2VPC:
  - property: IsDefault
    value: "true"
EC2Subnet:
  - property: DefaultForAz
    value: "true"
EC2InternetGateway:
  - property: DefaultVPC
    value: "true"
```

## Customising what gets preserved

Edit `nuke-run/nuke-config.yaml.tpl`. Filters support exact name matches, glob patterns, regex, and property/tag matching.

Common additions:

```yaml
# Preserve by tag
RDSInstance:
  - property: tag:keep
    value: "true"

# Preserve by name prefix (glob)
SecretsManagerSecret:
  - type: glob
    value: "prod/*"

# Preserve a specific S3 bucket
S3Bucket:
  - "my-important-bucket"
S3Object:
  - property: Bucket
    value: "my-important-bucket"
```

After editing the template, re-run with `dry_run=true` to verify before going live.
