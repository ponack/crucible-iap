# Policy-as-Code GitOps Guide

Store `.rego` policy files in a git repository and let Crucible sync them automatically on every push — policies reviewed in PRs, versioned with your infrastructure, no manual copy-paste into the UI.

## Contents

1. [Overview](#overview)
2. [Repository layout](#repository-layout)
3. [Adding a git source](#adding-a-git-source)
4. [Policy type inference](#policy-type-inference)
5. [Webhook setup](#webhook-setup)
6. [Mirror mode](#mirror-mode)
7. [Private repositories](#private-repositories)
8. [GitLab](#gitlab)
9. [Manual sync](#manual-sync)
10. [Combining git-managed and manual policies](#combining-git-managed-and-manual-policies)
11. [Best practices](#best-practices)

---

## Overview

A **policy git source** links a VCS repository to your Crucible org. When a push lands on the configured branch, the webhook triggers a background job that:

1. Fetches a `.tar.gz` archive of the repository from the VCS provider
2. Extracts every `.rego` file under the configured path
3. Upserts each file as a policy in the database (insert on first sync, update on subsequent syncs)
4. Reloads the policy engine so changes take effect immediately

Git-managed policies are tracked by `(source, file path)` — not by name — so renaming a file creates a new policy and the old one is left untouched (or deleted in mirror mode).

---

## Repository layout

Crucible infers the policy **type** from the directory structure. Place `.rego` files in a directory named after the policy type you want:

```
policies/
├── post_plan/
│   ├── no-destroy.rego
│   └── require-tags.rego
├── pre_plan/
│   └── validate-naming.rego
├── approval/
│   └── large-blast-radius.rego
└── login/
    └── allowed-groups.rego
```

Valid directory names: `pre_plan`, `post_plan`, `pre_apply`, `approval`, `trigger`, `login`.

You can also put all files at the root and annotate each one with a `# crucible:type` comment — see [Policy type inference](#policy-type-inference).

---

## Adding a git source

1. Navigate to **Policies** → **Git sources**
2. Click **Add source**
3. Fill in the form:

| Field | Description |
| --- | --- |
| Name | Display name for this source (e.g. `org-policies`) |
| Repository URL | Full HTTPS clone URL (e.g. `https://github.com/acme/policies.git`) |
| Branch | Branch to track (default: `main`) |
| Path | Subdirectory within the repo to scan (default: `.` — repo root) |
| VCS Integration | Optional — select a stored VCS credential for private repos |
| Mirror mode | When on, deletes policies from this source that no longer exist in the repo |

4. Click **Create**
5. Copy the **Webhook URL** shown on the source card — you'll register this in your VCS next

The first sync does not happen automatically on creation. Either push a commit or click **Sync now** to trigger the initial import.

---

## Policy type inference

Crucible determines a policy's type in this order:

1. **Parent directory name** — if the file lives in a directory whose name matches a valid policy type (`post_plan`, `pre_plan`, `pre_apply`, `approval`, `trigger`, `login`), that type is used
2. **`# crucible:type` comment** — if the file's first 20 lines contain `# crucible:type <type>`, that type is used
3. **Default** — `post_plan`

The `# crucible:type` comment is useful when all files live at the root or in a non-type directory:

```rego
# crucible:type approval
package crucible

approval := result if { ... }
```

The policy **name** is the filename without the `.rego` extension (e.g. `no-destroy.rego` → policy name `no-destroy`).

---

## Webhook setup

### GitHub

1. Open the repository → **Settings** → **Webhooks** → **Add webhook**
2. Paste the webhook URL from the Crucible git source card into **Payload URL**
3. Set **Content type** to `application/json`
4. Paste the **Webhook secret** into **Secret**
5. Choose **Just the push event**
6. Click **Add webhook**

Crucible verifies the `X-Hub-Signature-256` header on every delivery and rejects requests with an invalid signature. If no secret is configured on the git source, signature verification is skipped (not recommended for production).

### Gitea / Gogs

1. Repository → **Settings** → **Webhooks** → **Add Webhook** → **Gitea**
2. Paste the webhook URL into **Target URL**
3. Set **Secret** to match the webhook secret from the source card
4. Set **Content type** to `application/json`
5. Select **Push events**
6. Click **Add Webhook**

---

## Mirror mode

By default, syncing only **adds and updates** policies — it never deletes. This is safe: if you remove a file from the repo, its policy remains in Crucible until you delete it manually.

Enable **Mirror mode** on the git source to have Crucible automatically delete policies that belonged to this source but no longer exist in the latest sync:

- On each sync, Crucible compares the set of file paths in the archive against all policies with `git_source_id` matching this source
- Any policy whose path is absent from the archive is deleted
- Policies created manually (not via git sync) are never touched

Mirror mode is safe to enable at any time — it only affects policies that were created by this specific git source.

---

## Private repositories

For private repositories, create a VCS integration with a personal access token or deploy key token:

1. Navigate to **Settings** → **Integrations** → **Add integration**
2. Select the provider (GitHub, GitLab, etc.) and paste a token with `repo` (read) scope
3. Save the integration

Then select it in the **VCS Integration** dropdown when creating the git source. The token is stored encrypted and used only to fetch the archive on each sync.

**Minimum required permissions:**

| Provider | Scope |
| --- | --- |
| GitHub | `repo` (or `contents:read` for fine-grained tokens) |
| GitLab | `read_repository` |

---

## GitLab

GitLab is supported — Crucible uses the GitLab API v4 archive endpoint automatically when the repository URL contains a GitLab hostname.

**Self-hosted GitLab:** If your GitLab instance is not at `gitlab.com`, you need a VCS integration with the base URL set to your instance (e.g. `https://gitlab.internal`). Without a VCS integration the archive URL defaults to the public endpoint.

The webhook payload format is the same as GitHub (`after` field for the pushed SHA). Configure the GitLab webhook under **Repository Settings** → **Webhooks** with the **Push events** trigger and the secret token from the source card.

---

## Manual sync

You can trigger an ad-hoc sync from the **Policies → Git sources** page at any time:

1. Find the git source in the list
2. Click **Sync now**
3. The sync job is queued immediately — results appear in the policy list within a few seconds

Manual syncs always pull from the configured branch HEAD. They are useful after:
- Creating a new git source (initial import)
- Recovering from a webhook delivery failure
- Testing a new `.rego` file before pushing to the branch

---

## Combining git-managed and manual policies

Git-managed and manually-created policies coexist. Both appear in the **Policies** list and can be attached to stacks in the same way.

**What git sync can and cannot touch:**

| Operation | Git sync |
| --- | --- |
| Create new policy from `.rego` file | ✓ |
| Update body of a git-managed policy | ✓ |
| Delete git-managed policy (mirror mode only) | ✓ |
| Create or modify manually-created policies | ✗ |
| Delete manually-created policies | ✗ |

Git-managed policies can be edited in the UI — but the next sync will overwrite the body with the file content from the repo. Treat the repo as the source of truth for any git-managed policy.

---

## Best practices

**Use directory-based type organisation.** Placing files in `post_plan/`, `approval/`, etc. makes the policy type immediately obvious from the file tree. The `# crucible:type` comment works but requires opening the file to see the type.

**One repository per organisation.** A single `policies` repo with subdirectories per type is easier to review and audit than scattered policies across multiple repos. Use branch protection and require PR reviews before merging to `main`.

**Enable mirror mode on the primary source.** Mirror mode ensures the Crucible policy set always matches the repo. Without it, deleted policies linger silently. The only reason to leave it off is when you have policies in the same source that you want to preserve after removing the file (rare).

**Test before merging.** Use the [policy test playground](../policies.md#dry-run-sandbox) to validate new or changed policies against a real plan JSON before the PR lands. The `/policies/test` page accepts raw Rego — paste from your file and run it against a plan.

**Use separate sources for separate teams.** If multiple teams manage policies independently, give each team their own repo and git source. Mirror mode is then scoped per source — one team's deletions cannot affect another team's policies.

**Attach org-default policies via the UI after sync.** Git sync creates the policy but does not set it as an org default. After the initial sync, navigate to each policy that should apply globally and toggle **Set as org default**.
