# Run Hooks Guide

Run hooks are optional bash scripts that execute at specific points in the run lifecycle. They run inside the same ephemeral runner container as the OpenTofu/Terraform/Ansible process, with full access to all stack environment variables, OIDC tokens, and cloud credentials.

## Contents

1. [Hook lifecycle](#hook-lifecycle)
2. [Configuring hooks](#configuring-hooks)
3. [Examples](#examples)
4. [Security considerations](#security-considerations)
5. [Troubleshooting](#troubleshooting)

---

## Hook lifecycle

| Hook | Runs | Applies to |
| --- | --- | --- |
| Pre-plan | Before `tofu plan` / `ansible-playbook --check` | All run types |
| Post-plan | After plan, before the artifact is uploaded | All run types |
| Pre-apply | Before `tofu apply` / `ansible-playbook` | Apply phase only |
| Post-apply | After apply completes successfully | Apply phase only |

A non-zero exit from any hook immediately fails the run. The run log captures hook stdout and stderr, so errors are visible in the UI.

Hooks are injected as environment variables (`CRUCIBLE_HOOK_PRE_PLAN`, `CRUCIBLE_HOOK_POST_PLAN`, `CRUCIBLE_HOOK_PRE_APPLY`, `CRUCIBLE_HOOK_POST_APPLY`) and executed with `bash -c`.

---

## Configuring hooks

Open **Stacks** → *stack name* → **Edit** → **Run hooks**. Each hook is a free-text bash script field. Leave a field blank to skip that hook.

Scripts have access to all stack environment variables and any secrets set on the stack, including cloud credentials and OIDC tokens.

---

## Examples

### Slack notification on apply start

Post a message to Slack before every apply so the team knows a change is in flight:

```bash
# Pre-apply hook
curl -s -X POST "$SLACK_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d "{\"text\": \"Apply started on stack \`${CRUCIBLE_STACK_SLUG}\` by ${CRUCIBLE_TRIGGERED_BY:-automation}\"}"
```

Set `SLACK_WEBHOOK_URL` as a secret environment variable on the stack. `CRUCIBLE_STACK_SLUG` and `CRUCIBLE_TRIGGERED_BY` are injected by the runner automatically.

### Slack notification on apply complete

```bash
# Post-apply hook
curl -s -X POST "$SLACK_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d "{\"text\": \":white_check_mark: Apply completed on stack \`${CRUCIBLE_STACK_SLUG}\`\"}"
```

### Pre-flight connectivity check

Fail fast before planning if a required service is unreachable — avoids wasting time on a plan that will fail at apply:

```bash
# Pre-plan hook
echo "checking database endpoint..."
curl -sf --max-time 5 "https://db.internal/health" || {
  echo "ERROR: database health check failed — aborting run"
  exit 1
}
echo "connectivity OK"
```

### Enforce required variables

Validate that all required env vars are set before the plan starts:

```bash
# Pre-plan hook
required_vars="DB_PASSWORD API_SECRET REGION"
missing=""
for var in $required_vars; do
  if [[ -z "${!var:-}" ]]; then
    missing="$missing $var"
  fi
done
if [[ -n "$missing" ]]; then
  echo "ERROR: required variables not set:$missing"
  exit 1
fi
```

### Notify PagerDuty on failure (post-apply only runs on success)

Since hooks only run on success, use a pre-apply hook to register a "run started" marker, and a separate webhook or monitoring tool to detect missing "run completed" signals. For failure alerting, use outgoing webhooks instead — they fire on every run state change including failures.

### Invalidate a CDN cache after apply

```bash
# Post-apply hook
aws cloudfront create-invalidation \
  --distribution-id "$CLOUDFRONT_DISTRIBUTION_ID" \
  --paths "/*" \
  --region us-east-1
```

Requires `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` (or OIDC federation) on the stack with `cloudfront:CreateInvalidation` permission.

### Run a custom linter before planning

```bash
# Pre-plan hook
if command -v tflint &>/dev/null; then
  echo "running tflint..."
  tflint --recursive
else
  echo "tflint not found in runner image, skipping"
fi
```

To always have linting tools available, build a custom runner image:

```dockerfile
FROM ghcr.io/ponack/crucible-runner:latest
RUN curl -L "https://github.com/terraform-linters/tflint/releases/latest/download/tflint_linux_amd64.zip" \
      -o /tmp/tflint.zip && \
    unzip /tmp/tflint.zip -d /usr/local/bin/ && \
    rm /tmp/tflint.zip
```

Set the custom image in **Settings → Runner → Runner default image** or override it per-stack.

---

## Security considerations

- Hooks run with the same privileges as the main tofu/ansible process — full access to all env vars, secrets, and cloud credentials.
- Hook scripts are stored as plaintext in the database. Do not embed secrets directly in the script text; use stack environment variables instead.
- The runner container is destroyed after every run, so any files written during hooks do not persist.
- Hooks are scoped per-stack. An admin must have edit access to the stack to configure hooks.

---

## Troubleshooting

### Hook fails immediately

Check the run log in the UI — hook stdout and stderr are captured and shown inline. A common cause is a missing environment variable referenced in the hook script.

### Hook command not found

The runner image ships with common tools (`curl`, `jq`, `aws`, `bash`). If you need additional tools, build a custom runner image and set it on the stack.

### Hook runs but does nothing visible

Hooks that produce no output and exit 0 are silent — this is expected. Add `echo` statements for visibility:

```bash
echo "pre-plan hook: starting check..."
# your commands
echo "pre-plan hook: done"
```

### Post-apply hook never runs

Post-apply hooks only run when the apply phase succeeds. If the apply fails (or never starts because the run was discarded), the post-apply hook is skipped. Check the run status and apply log for errors.
