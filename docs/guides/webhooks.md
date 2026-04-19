# Webhooks Guide

Crucible integrates with your Git forge via webhooks. Push events and pull/merge request events trigger runs automatically. Every delivery is logged with its full payload, outcome, and the run it created — giving you a complete audit trail of every automation trigger.

## Contents

1. [Supported forges](#supported-forges)
2. [Setting up a webhook](#setting-up-a-webhook)
3. [What events trigger what](#what-events-trigger-what)
4. [Delivery log and payload viewer](#delivery-log-and-payload-viewer)
5. [Re-delivering a webhook](#re-delivering-a-webhook)
6. [Rotating the webhook secret](#rotating-the-webhook-secret)
7. [Troubleshooting](#troubleshooting)

---

## Supported forges

| Forge | Push events | PR / MR events |
| --- | --- | --- |
| GitHub | Yes | Yes |
| GitLab | Yes | Yes |
| Gitea | Yes | Yes |
| Gogs | Yes | Yes |

Crucible auto-detects the forge from request headers — no configuration needed on the Crucible side.

---

## Setting up a webhook

**Step 1 — Copy the webhook URL and secret**

Open **Stacks** → *stack name*. The webhook URL and webhook secret are shown in the **Webhook** section of the stack detail page.

> **Note on secret rotation:** The secret is only displayed once after generation or rotation. Copy it before navigating away. If you missed it, rotate the secret and copy the new value.

**Step 2 — Add the webhook in your forge**

### GitHub

Repository → **Settings** → **Webhooks** → **Add webhook**:

| Field | Value |
| --- | --- |
| Payload URL | The webhook URL from Crucible |
| Content type | `application/json` |
| Secret | The webhook secret from Crucible |
| Events | Select **Pushes** and **Pull requests** |

### GitLab

Project → **Settings** → **Webhooks** → **Add new webhook**:

| Field | Value |
| --- | --- |
| URL | The webhook URL from Crucible |
| Secret token | The webhook secret from Crucible |
| Trigger | **Push events** and **Merge request events** |

### Gitea / Gogs

Repository → **Settings** → **Webhooks** → **Add Webhook** → Gitea (or Gogs):

| Field | Value |
| --- | --- |
| Target URL | The webhook URL from Crucible |
| Secret | The webhook secret from Crucible |
| Trigger On | **Push Events** and **Pull Request Events** |

---

## What events trigger what

| Event | Run type | Auto-apply? |
| --- | --- | --- |
| Push to tracked branch | `tracked` | Depends on stack auto-apply setting |
| Pull / merge request opened or updated | `proposed` | Never (plan only) |
| Tag push matching semver (`vX.Y.Z`) | `tracked` | Depends on stack auto-apply setting |

A `tracked` run runs the full plan → apply lifecycle. A `proposed` run is plan-only — it posts a summary comment on the PR and sets a commit status check, but never applies.

Pushes to branches other than the stack's tracked branch are silently skipped (recorded as `branch_mismatch` in the delivery log).

---

## Delivery log and payload viewer

Every webhook delivery — including skipped and rejected ones — is recorded. Open **Stacks** → *stack name* → **Webhooks** to see the delivery history.

Each row shows:

| Column | Description |
| --- | --- |
| Time | When the delivery arrived |
| Event | The forge event type (e.g. `push`, `pull_request`) |
| Outcome | `triggered`, `skipped`, or `rejected` |
| Skip reason | Why the delivery was skipped (if applicable) |
| Run | Link to the run that was created (if `triggered`) |

**Click any row** to expand the raw JSON payload that Crucible received from the forge. This is the exact body that was sent, stored for debugging and auditing. The payload viewer lazy-loads on first click and is cached for the session.

### Outcome reference

| Outcome | Reason | Meaning |
| --- | --- | --- |
| `triggered` | — | A run was enqueued successfully |
| `skipped` | `branch_mismatch` | Push was to a non-tracked branch |
| `skipped` | `unknown_event` | Event type not recognised (e.g. `star`, `fork`) |
| `skipped` | `stack_disabled` | The stack is disabled |
| `skipped` | `no_module_config` | Tag push but stack has no module configuration |
| `skipped` | `tag_not_semver` | Tag push but tag does not match `vX.Y.Z` format |
| `skipped` | `enqueue_failed` | Run was created but could not be queued — check worker logs |
| `rejected` | `no_secret` | Stack has no webhook secret configured |
| `rejected` | `bad_signature` | HMAC signature did not match — wrong secret or payload tampered |

---

## Re-delivering a webhook

If a delivery was skipped due to a transient error (e.g. `enqueue_failed`), you can re-trigger it without pushing to the repo.

On the delivery row, click **Re-deliver**. Crucible replays the stored payload through the same processing pipeline — no new network request to the forge is made. The signature is not re-verified (the payload was already verified on first delivery).

Re-delivery creates a new run just like a fresh webhook event would.

---

## Rotating the webhook secret

Open **Stacks** → *stack name* → **Webhooks** → **Rotate secret**. Crucible generates a new secret immediately.

**Important:** Update the secret in your forge's webhook settings before the next push event. Deliveries signed with the old secret will be rejected with `bad_signature`. The new secret is shown once — copy it and update your forge webhook before closing the dialog.

The new secret is temporarily stored in your browser's `sessionStorage` so it persists across navigations in the same tab. It is cleared when you click **Dismiss** or close the tab.

---

## Troubleshooting

### Pushes are not triggering runs

1. Open the delivery log — if the delivery appears there, Crucible received it. Check the outcome and skip reason.
2. If the delivery does not appear at all, the forge is not reaching Crucible. Check:
   - The webhook URL is correct and reachable from the forge
   - Caddy / reverse proxy is forwarding `POST /api/v1/stacks/<id>/webhook` correctly
   - No firewall is blocking the forge's IP range

### All deliveries show `rejected: bad_signature`

The webhook secret in the forge does not match the secret stored in Crucible. Rotate the secret in Crucible (**Webhooks → Rotate secret**) and update the forge webhook with the new value.

### Pushes to a feature branch trigger runs

The stack's tracked branch is set to a branch that matches your feature branch name, or the tracked branch is blank. Open **Stacks** → *stack name* → **Edit** and verify the **Branch** field is set to the correct protected branch (e.g. `main`).

### PR comments are not appearing

Crucible posts PR comments using the forge API. Check:
- The webhook is configured with pull/merge request events enabled
- The runner image can reach the forge API (not blocked by network policy)
- Any forge API rate limits (GitHub: 5000 req/hr per token; GitLab: 2000 req/min)

Check worker logs for errors:

```bash
docker compose logs crucible-worker | grep -i "pr comment\|merge request\|pull request"
```
