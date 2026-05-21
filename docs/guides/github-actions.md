# Guide: Triggering Crucible from GitHub Actions

Crucible's native GitOps trigger is a VCS webhook — a push to `main` or a PR creates a run automatically without any CI involvement. That's the recommended path for *most* changes.

But sometimes you want to trigger a Crucible run from inside a GitHub Actions workflow:

- After a build job produces a new container image that needs deploying.
- As part of a chained pipeline ("run integration tests, then promote infra").
- For scheduled or manual `workflow_dispatch` deploys.
- To run a destroy on schedule for ephemeral PR environments.

This guide shows the patterns. It assumes the [CLI guide](cli.md) has been read.

---

## When to use webhooks vs Actions

| Trigger | Mechanism | Use when |
| --- | --- | --- |
| Push / PR on the stack's repo | Crucible webhook (built-in) | The default — code change drives infra change |
| Code change on a different repo | GitHub Actions → Crucible API | App repo decides when infra changes |
| Schedule / manual / matrix builds | GitHub Actions → Crucible API | Anything that's not a simple "code change" |
| Test passes + deploy infra in same pipeline | GitHub Actions → Crucible API | Coupling tests to infra rollout |

You can use both simultaneously: keep the webhook on for plan-on-push (proposed runs), and use Actions for explicit tracked/destroy runs.

---

## Step 1 — Create a Crucible API token

In Crucible: **Settings → API tokens → Generate new token**.

- Name: `github-actions-<repo>`
- Role: `member` is enough to trigger runs; `admin` only if you'll be modifying stacks from CI.
- Copy the token (shown once).

---

## Step 2 — Store the token in GitHub

Repo → **Settings → Secrets and variables → Actions → New repository secret**:

| Secret name | Value |
| --- | --- |
| `CRUCIBLE_URL` | `https://crucible.example.com` |
| `CRUCIBLE_TOKEN` | the token from Step 1 |

For org-wide use across many repos, use an organisation secret instead.

---

## Step 3 — Pattern A: Trigger a run after a build

The most common pattern. App workflow builds a Docker image, pushes it, then asks Crucible to deploy:

```yaml
# .github/workflows/build-and-deploy.yml
name: Build and deploy

on:
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      image_tag: ${{ steps.meta.outputs.tag }}
    steps:
      - uses: actions/checkout@v4

      - name: Build and push image
        id: meta
        run: |
          TAG="${GITHUB_SHA::8}"
          docker build -t ghcr.io/myorg/myapp:$TAG .
          docker push ghcr.io/myorg/myapp:$TAG
          echo "tag=$TAG" >> $GITHUB_OUTPUT

  deploy:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - name: Install Crucible CLI
        run: |
          curl -sSLO https://github.com/ponack/crucible-iap/releases/latest/download/crucible-linux-amd64
          chmod +x crucible-linux-amd64
          sudo mv crucible-linux-amd64 /usr/local/bin/crucible

      - name: Trigger tracked run
        env:
          CRUCIBLE_URL: ${{ secrets.CRUCIBLE_URL }}
          CRUCIBLE_TOKEN: ${{ secrets.CRUCIBLE_TOKEN }}
          IMAGE_TAG: ${{ needs.build.outputs.image_tag }}
          STACK_ID: 7c4e1a30-...        # your stack ID
        run: |
          # Update the image tag env var first (one-off; usually you'd commit
          # the tag to a config repo and let the webhook pick it up)
          curl -sf -X PUT \
            -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"value\":\"$IMAGE_TAG\",\"is_secret\":false}" \
            "$CRUCIBLE_URL/api/v1/stacks/$STACK_ID/env-vars/IMAGE_TAG"

          # Trigger the run and wait for it to finish
          RUN_ID=$(crucible runs trigger "$STACK_ID" --type tracked -q)
          echo "Triggered run $RUN_ID"
          crucible runs status "$RUN_ID" --watch
```

> **Note on the CLI binary URL:** Pre-built CLI binaries aren't published yet (the CLI ships as source in [`api/cmd/crucible/`](../../api/cmd/crucible)). Until pre-built releases are available, either build from source in the workflow or substitute the curl commands below with direct API calls. See the [CLI guide](cli.md#install) for build steps.

Pure curl version (no CLI install):

```yaml
- name: Trigger tracked run via API
  env:
    CRUCIBLE_URL: ${{ secrets.CRUCIBLE_URL }}
    CRUCIBLE_TOKEN: ${{ secrets.CRUCIBLE_TOKEN }}
    STACK_ID: 7c4e1a30-...
  run: |
    RUN_ID=$(curl -sf -X POST \
      -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"type":"tracked"}' \
      "$CRUCIBLE_URL/api/v1/stacks/$STACK_ID/runs" \
      | jq -r .id)

    # Poll until terminal
    while true; do
      STATUS=$(curl -sf -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
        "$CRUCIBLE_URL/api/v1/runs/$RUN_ID" | jq -r .status)
      echo "[$RUN_ID] status: $STATUS"
      case "$STATUS" in
        finished|failed|discarded|cancelled|error)
          [ "$STATUS" = "finished" ] && exit 0 || exit 1
          ;;
      esac
      sleep 10
    done
```

---

## Pattern B: Schedule a destroy for ephemeral PR environments

Pair with PR preview environments. When a PR closes, a scheduled job destroys its stack:

```yaml
# .github/workflows/cleanup-pr-envs.yml
name: Clean up closed PR environments

on:
  pull_request:
    types: [closed]

jobs:
  destroy:
    runs-on: ubuntu-latest
    steps:
      - name: Destroy the PR's stack
        env:
          CRUCIBLE_URL: ${{ secrets.CRUCIBLE_URL }}
          CRUCIBLE_TOKEN: ${{ secrets.CRUCIBLE_TOKEN }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
        run: |
          # Look up the stack by name convention
          STACK_ID=$(curl -sf \
            -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
            "$CRUCIBLE_URL/api/v1/stacks?name=pr-$PR_NUMBER" \
            | jq -r '.[0].id // empty')

          if [ -z "$STACK_ID" ]; then
            echo "No stack found for PR #$PR_NUMBER — nothing to clean up."
            exit 0
          fi

          # Trigger destroy and wait
          RUN_ID=$(curl -sf -X POST \
            -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
            -H "Content-Type: application/json" \
            -d '{"type":"destroy"}' \
            "$CRUCIBLE_URL/api/v1/stacks/$STACK_ID/runs" \
            | jq -r .id)

          # ... poll as in Pattern A
```

For PR preview environments created automatically on PR open, see [`blueprints.md`](blueprints.md) — Crucible's blueprint feature pairs naturally with this cleanup workflow.

---

## Pattern C: `workflow_dispatch` for manual ops

A button in the GitHub UI that triggers a Crucible run:

```yaml
# .github/workflows/manual-deploy.yml
name: Manual deploy

on:
  workflow_dispatch:
    inputs:
      stack_id:
        description: Crucible stack ID
        required: true
        type: string
      run_type:
        description: Run type
        required: true
        type: choice
        options:
          - proposed
          - tracked

jobs:
  trigger:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger run
        env:
          CRUCIBLE_URL: ${{ secrets.CRUCIBLE_URL }}
          CRUCIBLE_TOKEN: ${{ secrets.CRUCIBLE_TOKEN }}
        run: |
          curl -sf -X POST \
            -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"type\":\"${{ inputs.run_type }}\"}" \
            "$CRUCIBLE_URL/api/v1/stacks/${{ inputs.stack_id }}/runs"
```

---

## Pattern D: PR preview run (proposed) with PR comment

Skip this if you already have the Crucible webhook hooked up — the webhook does this automatically. But if Crucible isn't reachable from `github.com`'s servers (self-hosted, private network), use Actions as the bridge:

```yaml
name: Crucible PR preview

on:
  pull_request:

jobs:
  preview:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger proposed run
        env:
          CRUCIBLE_URL: ${{ secrets.CRUCIBLE_URL }}
          CRUCIBLE_TOKEN: ${{ secrets.CRUCIBLE_TOKEN }}
          STACK_ID: 7c4e1a30-...
        run: |
          curl -sf -X POST \
            -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"type\":\"proposed\",\"branch\":\"${{ github.head_ref }}\"}" \
            "$CRUCIBLE_URL/api/v1/stacks/$STACK_ID/runs"
```

---

## Authorisation tips

- **Use a scoped service-account token** (Settings → API tokens, role: `member`) rather than a personal user token. Personal tokens are revoked when the user leaves; service-account tokens survive.
- **Rotate tokens periodically.** Crucible doesn't expire API tokens by default — set a calendar reminder, or rotate quarterly.
- **One token per repository or one per workflow.** If a token is compromised, you can revoke without affecting unrelated workflows.

---

## Troubleshooting

### Workflow hangs on the polling step

Either the run is genuinely running for a long time, or your polling loop isn't matching the terminal status. Confirm with `curl` from your laptop:

```bash
curl -H "Authorization: Bearer $CRUCIBLE_TOKEN" \
  $CRUCIBLE_URL/api/v1/runs/<run-id> | jq .status
```

If status is one of `finished` / `failed` / `discarded` / `cancelled` / `error`, your loop's case statement is missing it.

### `401 Unauthorized`

Token is wrong or revoked. Generate a new one.

### `403 Forbidden`

Token is valid but the user doesn't have permission. Confirm the user has at least `member` role on the org and (if projects are in use) `member` on the relevant project.

### `Crucible URL is private — github.com can't reach it`

That's expected if you self-host on a private network. Two options:

1. **Use a self-hosted runner** for the workflow — runs on a machine inside your network with access to Crucible.
2. **Expose Crucible publicly** behind TLS — Crucible is hardened for public exposure (Caddy / external proxy support, rate limits, OIDC SSO).

---

## What's next

- [cli.md](cli.md) — the underlying CLI that the workflow snippets emulate.
- [blueprints.md](blueprints.md) — pair with PR-preview cleanup for full ephemeral env lifecycle.
- [stack-templates.md](stack-templates.md) — create stack templates that Actions can clone.
- [operator-guide.md#chatops-approvals](../operator-guide.md#chatops-approvals) — once a run needs human approval, ChatOps lets approvers confirm without leaving Slack/Teams.
