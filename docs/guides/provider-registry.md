# Private Provider Registry

Crucible IAP includes a built-in Terraform/OpenTofu provider registry so you can host, version, and distribute internal providers without depending on the public Terraform Registry.

## Contents

1. [Overview](#overview)
2. [Publishing a provider binary](#publishing-a-provider-binary)
3. [Terraform configuration](#terraform-configuration)
4. [GPG signing (optional)](#gpg-signing-optional)
5. [Day-to-day workflow](#day-to-day-workflow)
6. [Terraform lock file](#terraform-lock-file)

---

## Overview

The private provider registry implements the [Terraform Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol) so that `terraform init` and `tofu init` work without any plugin changes. Crucible acts as the registry endpoint — providers are stored in MinIO and served over the same base URL as the rest of the application.

**Use the private registry when:**

- Your environment is air-gapped and cannot reach `registry.terraform.io`
- You have internal providers that must not be published publicly
- Your organisation requires full control over which provider versions are available to stacks
- You want download counts and a central audit trail for provider consumption

The discovery endpoint at `GET /.well-known/terraform.json` advertises the registry base path. Terraform resolves provider sources of the form `<host>/<namespace>/<type>` against this endpoint automatically.

---

## Publishing a provider binary

### File format

Terraform expects providers packaged as a zip archive named with this exact convention:

```
terraform-provider-<type>_<version>_<os>_<arch>.zip
```

For example:

```
terraform-provider-myprovider_1.2.0_linux_amd64.zip
terraform-provider-myprovider_1.2.0_darwin_arm64.zip
terraform-provider-myprovider_1.2.0_windows_amd64.zip
```

The zip must contain the provider binary at its root (no sub-directories). The binary itself must be named `terraform-provider-<type>_v<version>` (with a `v` prefix on the version inside the archive).

For instructions on building a provider from source, see the [HashiCorp plugin development documentation](https://developer.hashicorp.com/terraform/plugin/best-practices/distributing).

### Upload via the Crucible UI

1. Open **Providers** in the left sidebar
2. Click **Publish provider**
3. Fill in the fields:

| Field | Description |
| --- | --- |
| Namespace | Logical group for the provider, e.g. `myorg` |
| Type | Provider name without the `terraform-provider-` prefix, e.g. `myprovider` |
| Version | Semantic version string, e.g. `1.2.0` |
| OS | Target operating system: `linux`, `darwin`, or `windows` |
| Arch | Target architecture: `amd64`, `arm64`, `386` |
| File | The `.zip` archive matching the naming convention above |

4. Click **Publish**. Crucible computes the SHA-256 checksum automatically — no manual step is needed.

### Upload via API

Send a `multipart/form-data` POST request to the registry endpoint. Authenticate with a service account token (format `ciap_<id>_<secret>`, 81 characters total).

```bash
curl -X POST https://crucible.example.com/api/v1/registry/providers \
  -H "Authorization: Bearer ciap_your_service_account_token" \
  -F "namespace=myorg" \
  -F "type=myprovider" \
  -F "version=1.2.0" \
  -F "os=linux" \
  -F "arch=amd64" \
  -F "provider=@terraform-provider-myprovider_1.2.0_linux_amd64.zip"
```

| Form field | Value |
| --- | --- |
| `namespace` | Provider namespace, e.g. `myorg` |
| `type` | Provider type, e.g. `myprovider` |
| `version` | Semantic version, e.g. `1.2.0` |
| `os` | Target OS: `linux`, `darwin`, `windows` |
| `arch` | Target architecture: `amd64`, `arm64`, `386` |
| `provider` | Path to the `.zip` file |

SHA-256 is computed server-side at upload time.

### Multi-platform releases

Upload once per OS/arch combination using the same `namespace`, `type`, and `version` values. All platforms for a version are grouped together on the provider detail page and served correctly to each `terraform init` caller based on the requesting platform.

---

## Terraform configuration

### Step 1 — Create a service account token

Open **Settings** → **API Tokens** and create a token scoped to your organisation. Copy the full token value — it is only shown once.

### Step 2 — Add credentials to `.terraformrc`

Terraform reads credentials from `~/.terraformrc` (Linux/macOS) or `%APPDATA%\terraform.rc` (Windows). Add a `credentials` block for your Crucible hostname:

```hcl
credentials "crucible.example.com" {
  token = "ciap_your_service_account_token"
}
```

Replace `crucible.example.com` with the hostname from your `CRUCIBLE_BASE_URL` environment variable. Terraform passes this token as a `Bearer` header on all registry requests, including download — the JWT authentication requirement is handled transparently.

### Step 3 — Reference the provider in your Terraform code

```hcl
terraform {
  required_providers {
    myprovider = {
      source  = "crucible.example.com/myorg/myprovider"
      version = "~> 1.0"
    }
  }
}
```

The source address follows the standard `<host>/<namespace>/<type>` format. Version constraints use the same syntax as the public registry.

### Step 4 — Initialise

```bash
terraform init
```

Terraform calls `GET /.well-known/terraform.json` on your Crucible host to discover the registry path, then resolves the provider version and downloads the correct binary for the current platform. No plugins, mirrors, or `filesystem_mirror` blocks are needed.

---

## GPG signing (optional)

GPG signatures allow `terraform providers lock` to verify provider binaries cryptographically. Without a registered key, Terraform logs a warning but still installs the provider.

### Why bother

- `terraform providers lock` populates `.terraform.lock.hcl` with checksums and signing key fingerprints
- CI pipelines can enforce that the lock file was generated against a trusted key
- Provides an end-to-end chain: you sign the release, Crucible serves the signature, Terraform verifies it

### Add a GPG key via the UI

1. Open **Providers** in the left sidebar
2. Click **GPG keys**
3. Click **Add key** and fill in the fields:

| Field | Description |
| --- | --- |
| Namespace | The namespace this key covers, e.g. `myorg` |
| Key ID | The 16-character hex key ID, e.g. `ABC1234567890DEF` |
| ASCII-armored public key | The full `-----BEGIN PGP PUBLIC KEY BLOCK-----` block |

### Add a GPG key via API

```bash
curl -X POST https://crucible.example.com/api/v1/registry/provider-gpg-keys \
  -H "Authorization: Bearer ciap_your_service_account_token" \
  -H "Content-Type: application/json" \
  -d '{
    "namespace": "myorg",
    "key_id": "ABC1234567890DEF",
    "ascii_armor": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n...\n-----END PGP PUBLIC KEY BLOCK-----"
  }'
```

### Signing a release before upload

Crucible does not sign binaries for you — signing must happen before upload. The standard approach is to sign the SHA-256 checksums file:

```bash
# Generate checksums for all platform zips
sha256sum terraform-provider-myprovider_1.2.0_*.zip \
  > terraform-provider-myprovider_1.2.0_SHA256SUMS

# Sign the checksums file with your release key
gpg --detach-sign --armor \
  --local-user ABC1234567890DEF \
  terraform-provider-myprovider_1.2.0_SHA256SUMS
# produces terraform-provider-myprovider_1.2.0_SHA256SUMS.sig
```

Upload the individual `.zip` files through the UI or API as normal. The registry serves the registered public key in the `signing_keys` field of download responses so that Terraform can verify checksums during `providers lock`.

---

## Day-to-day workflow

### Add a new platform binary for an existing version

Just publish again with the same `namespace`, `type`, and `version` but a different `os`/`arch`. The existing version entry is updated in place — no need to remove and re-add anything.

### Yank a bad version

Yanking marks a version as deprecated without deleting it. Terraform will not select a yanked version during `init` unless the version is pinned exactly in the configuration.

**Via the UI:** Open the provider detail page, find the version row, and click **Yank**.

**Via the API:**

```bash
curl -X DELETE https://crucible.example.com/api/v1/registry/providers/:id \
  -H "Authorization: Bearer ciap_your_service_account_token"
```

Replace `:id` with the provider binary record ID shown on the detail page.

### Download counts

Each binary tracks a download counter. Open **Providers** → provider detail page to see per-binary download counts broken down by version and platform.

---

## Terraform lock file

After the first `terraform init`, Terraform writes `.terraform.lock.hcl` with the provider version, platform checksums, and (if GPG keys are registered) signing key fingerprints. Commit this file to git so that all team members and CI runs use the same provider version.

### Pre-populate for multiple platforms

If your team develops on different operating systems, generate lock file entries for all platforms at once so that `terraform init` on any platform succeeds without network access to the registry:

```bash
terraform providers lock \
  -platform=linux_amd64 \
  -platform=linux_arm64 \
  -platform=darwin_arm64 \
  -platform=windows_amd64
```

Run this command after publishing a new provider version and commit the updated `.terraform.lock.hcl`. Team members pull the updated lock file and `terraform init` resolves the cached checksums without re-querying the registry.

> **Note:** The `-platform` flag requires that binaries for each listed platform have been published to the registry. If a platform binary is missing, `terraform providers lock` will fail with a `404` from the registry.
