# Azure Guide

This guide covers running OpenTofu with Microsoft Azure inside Crucible IAP: credential injection, remote state in Azure Blob Storage, and keyless federated identity.

## Contents

1. [Providing Azure credentials](#providing-azure-credentials)
2. [Azure Blob remote state backend](#azure-blob-remote-state-backend)
3. [Minimal role assignments](#minimal-role-assignments)
4. [Recommended policies for Azure stacks](#recommended-policies-for-azure-stacks)

---

## Providing Azure credentials

### Option 1 — Service principal client secret (simple, not recommended for production)

Create a service principal and add its credentials as stack environment variables:

```bash
az ad sp create-for-rbac --name crucible-runner --role Contributor \
  --scopes /subscriptions/<subscription-id>
```

This outputs a JSON block. Set the following on the stack (all as **secret**):

| Variable | Value |
| --- | --- |
| `ARM_CLIENT_ID` | `appId` from the output |
| `ARM_CLIENT_SECRET` | `password` from the output |
| `ARM_TENANT_ID` | `tenant` from the output |
| `ARM_SUBSCRIPTION_ID` | Your Azure subscription ID |

The AzureRM provider picks these up automatically.

### Option 2 — Federated identity via Crucible (recommended, keyless)

Crucible acts as an OIDC identity provider. The worker mints a short-lived OIDC token per run and presents it to Azure AD for a federated identity exchange — no client secrets, no certificate rotation.

#### Per-stack configuration

Open **Stacks** → *stack name* → **Edit** → **Cloud OIDC** and set:

| Field | Value |
| --- | --- |
| Provider | `azure` |
| Tenant ID | Your Azure AD tenant ID |
| Client ID | The app registration client ID (see setup below) |
| Subscription ID | Your Azure subscription ID |

Crucible automatically sets `AZURE_FEDERATED_TOKEN_FILE=/tmp/oidc-token`, `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_SUBSCRIPTION_ID`. The AzureRM provider reads these natively — no provider changes needed.

#### Org-level default

To share the same app registration across all stacks, configure it once in **Settings → General → Cloud OIDC Default** instead of repeating it per stack. Per-stack configuration always overrides the org default.

#### Setting up the app registration and federated credential

**1. Create the app registration:**

```bash
az ad app create --display-name "crucible-runner"
```

Note the `appId` (client ID) and `id` (object ID) from the output.

**2. Create a service principal:**

```bash
az ad sp create --id <app-object-id>
```

**3. Add a federated credential:**

```bash
az ad app federated-credential create \
  --id <app-object-id> \
  --parameters '{
    "name": "crucible-all-stacks",
    "issuer": "https://crucible.example.com",
    "subject": "stack:*",
    "audiences": ["api://AzureADTokenExchange"]
  }'
```

Replace `https://crucible.example.com` with your `CRUCIBLE_BASE_URL`. The `subject` field uses `stack:<slug>` — use `stack:*` to allow all stacks, or `stack:my-stack-slug` to restrict to a single stack.

**4. Assign a role to the service principal:**

```bash
az role assignment create \
  --assignee <app-object-id> \
  --role Contributor \
  --scope /subscriptions/<subscription-id>
```

Scope the role to a resource group for finer control:

```bash
--scope /subscriptions/<subscription-id>/resourceGroups/my-rg
```

---

## Azure Blob remote state backend

Store OpenTofu state in Azure Blob Storage for durability and cross-run sharing.

### Create the backend resources

```bash
# Resource group for Terraform state
az group create --name tofu-state-rg --location westeurope

# Storage account (name must be globally unique, 3-24 lowercase alphanumeric)
az storage account create \
  --name myorgtofulstate \
  --resource-group tofu-state-rg \
  --sku Standard_LRS \
  --encryption-services blob

# Container
az storage container create \
  --name tofu-state \
  --account-name myorgtofulstate
```

### Configure the backend

```hcl
terraform {
  backend "azurerm" {
    resource_group_name  = "tofu-state-rg"
    storage_account_name = "myorgtofulstate"
    container_name       = "tofu-state"
    key                  = "stacks/my-stack/terraform.tfstate"
  }
}
```

The service principal needs `Storage Blob Data Contributor` on the storage account:

```bash
az role assignment create \
  --assignee <app-object-id> \
  --role "Storage Blob Data Contributor" \
  --scope /subscriptions/<subscription-id>/resourceGroups/tofu-state-rg/providers/Microsoft.Storage/storageAccounts/myorgtofulstate
```

> **Note:** If you use Crucible's built-in HTTP state backend (the default), you do not need Azure Blob at all — Crucible manages state in MinIO. Use Azure Blob only when sharing state with tools outside of Crucible.

---

## Minimal role assignments

Scope the Contributor role to only the resource groups your stacks manage, rather than the full subscription. For least-privilege deployments, create a custom role:

```json
{
  "Name": "Crucible Runner",
  "IsCustom": true,
  "Description": "Deploy resources via Crucible IAP",
  "Actions": [
    "Microsoft.Resources/subscriptions/resourceGroups/read",
    "Microsoft.Compute/*",
    "Microsoft.Network/*",
    "Microsoft.Storage/*"
  ],
  "NotActions": [],
  "AssignableScopes": ["/subscriptions/<subscription-id>"]
}
```

Save as `crucible-role.json` and create:

```bash
az role definition create --role-definition crucible-role.json
```

---

## Recommended policies for Azure stacks

| Policy | Type | What it does |
| --- | --- | --- |
| `require-tags.rego` | `post_plan` | Enforces required tags on Azure resources |
| `restrict-regions.rego` | `post_plan` | Blocks provider configs targeting disallowed Azure regions |
| `no-destroy.rego` | `post_plan` | Hard-blocks any plan that would delete resources (production stacks) |
| `approval-for-destroy.rego` | `approval` | Requires human approval before applying any plan with deletes |

Attach these via **Stacks** → *stack name* → **Policies**, or mark them as org defaults to apply everywhere automatically.
