# Guide: The `crucible` CLI

Crucible ships with a command-line client for triggering runs, checking status, and approving from your terminal — without leaving the editor or opening the UI. Useful for:

- Scripting workflows ("trigger this stack on every Jenkins build")
- Quick approvals during incidents ("I'm on a phone; confirm that run for me")
- Listing recent runs and statuses without opening a browser
- Wiring Crucible into your own CI without writing HTTP code

---

## Install

The CLI source lives at [`api/cmd/crucible/`](../../api/cmd/crucible) in this repository. Build it yourself:

```bash
git clone https://github.com/ponack/crucible-iap.git
cd crucible-iap/api
go build -o crucible ./cmd/crucible
sudo mv crucible /usr/local/bin/
```

Verify:

```bash
crucible --version
```

Pre-built binaries for common platforms are on the roadmap — for now build from source. Go 1.25+ required.

---

## First-time setup

Configure your base URL and API token:

```bash
crucible configure
```

You'll be prompted for:

- **Base URL** — e.g. `https://crucible.example.com` (no trailing slash)
- **API token** — create one at **Settings → API tokens → Generate new token** in the Crucible UI

The config is saved to `~/.config/crucible/config.yaml` with `0600` permissions.

You can override these per-invocation with `--url` and `--token` flags, or via the `CRUCIBLE_URL` and `CRUCIBLE_TOKEN` environment variables (useful for CI).

---

## Common commands

### List stacks

```bash
crucible stacks list
```

```text
ID        NAME              TOOL       STATUS      HEALTH   LOCKED
ab12cd34  prod-network      opentofu   finished    good
ef56gh78  staging-app       terragrunt unconfirmed good
ij90kl12  dev-sandbox       pulumi     failed      poor     locked
```

Filter to a project:

```bash
crucible stacks list --project 7c4e1a30-...
```

### Show stack details

```bash
crucible stacks show <stack-id>
```

Returns a key-value summary: tool, repo, branch, last run, drift status.

### Trigger a run

```bash
# Default: proposed (plan-only)
crucible runs trigger <stack-id>

# Tracked (plan → confirm → apply)
crucible runs trigger <stack-id> --type tracked

# Destroy
crucible runs trigger <stack-id> --type destroy
```

The command returns immediately with the new run ID and status. Use `runs status --watch` to follow it.

### Check run status

```bash
crucible runs status <run-id>
```

Add `--watch` to poll every 5 seconds until the run reaches a terminal state (`finished`, `failed`, `discarded`, `cancelled`):

```bash
crucible runs status <run-id> --watch
```

### Confirm an unconfirmed run

```bash
crucible runs confirm <run-id>
```

### Approve a run pending approval

```bash
crucible runs approve <run-id>
```

(Note: `approve` is for runs in `pending_approval` state — those gated by an approval policy. `confirm` is for runs in `unconfirmed` state — the normal post-plan gate.)

### Discard a run

```bash
crucible runs discard <run-id>
```

### List recent runs

```bash
crucible runs list                          # 20 most recent across all stacks
crucible runs list --stack <stack-id>       # 20 most recent for one stack
```

---

## Scripting with the CLI

### Get just the ID (for piping)

The `--quiet` (`-q`) flag prints only the resource ID:

```bash
RUN_ID=$(crucible runs trigger <stack-id> --type tracked -q)
echo "Triggered $RUN_ID"
```

### Get raw JSON

The `--json` flag prints unfiltered API response:

```bash
crucible stacks show <stack-id> --json | jq .repo_branch
```

### Wait for completion in a CI script

```bash
RUN_ID=$(crucible runs trigger $STACK --type tracked -q)

# Watch until terminal state
crucible runs status $RUN_ID --watch

# Check exit status from final JSON
STATUS=$(crucible runs status $RUN_ID --json | jq -r .status)
if [ "$STATUS" != "finished" ]; then
  echo "Run failed: $STATUS" >&2
  exit 1
fi
```

### Confirm a run from Slack via API token

A simple bot pattern: post a message with the run ID, accept a slash command, then:

```bash
crucible runs confirm <run-id-from-slash-command>
```

For full ChatOps (HMAC-signed links, no token needed), see [`operator-guide.md#chatops-approvals`](../operator-guide.md#chatops-approvals).

---

## Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `CRUCIBLE_URL` | from config file | Override base URL (CI-friendly) |
| `CRUCIBLE_TOKEN` | from config file | Override token (CI-friendly) |

Precedence: explicit flags > env vars > config file.

---

## Exit codes

| Exit code | Meaning |
| --- | --- |
| `0` | Command succeeded |
| `1` | Command failed (HTTP error, no config, network problem, etc.) |

Stderr carries the error message. Stdout carries the result (table, JSON, or ID).

---

## What the CLI cannot do (yet)

The CLI is intentionally focused on the run lifecycle. For these, use the UI or the API directly:

- Creating, editing, or deleting stacks
- Managing policies (write, attach, detach)
- Managing org members, invites, RBAC
- Reading audit logs
- Variable set / blueprint / template management

See the API endpoints in [`api/internal/server/server.go`](../../api/internal/server/server.go) for the full surface; everything is HTTP+JSON with the same bearer token as the CLI.

---

## Troubleshooting

### `no base URL configured`

You haven't run `crucible configure` yet, or the config file isn't readable. Run `crucible configure` or set `CRUCIBLE_URL` in your shell.

### `401 Unauthorized`

Your API token is invalid or revoked. Generate a new one in **Settings → API tokens** and re-run `crucible configure`.

### `403 Forbidden`

Your token's user lacks permission for the action — e.g. you have `viewer` and tried to trigger a run. Get an upgrade to `member` or `admin`.

### `dial tcp: lookup ...: no such host`

Crucible's hostname doesn't resolve from where you're running the CLI. Try a direct `curl` to the same URL to confirm reachability.
