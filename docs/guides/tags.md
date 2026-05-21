# Guide: Tags

Tags are color-coded labels you attach to stacks for filtering, grouping, and at-a-glance visual organisation. They have no functional effect on runs — they're a purely organisational layer for humans.

```text
prod-network    [env:prod] [team:sre] [aws]
staging-app     [env:staging] [team:web] [aws]
dev-sandbox     [env:dev] [team:web] [aws]
homelab-vault   [env:prod] [team:sre] [proxmox]
```

A typical org accumulates a handful of tags within the first week. Beyond ~30 tags, consider [projects](projects.md) for hierarchical grouping instead.

---

## When to use tags vs projects vs naming conventions

| Approach | Strength | Weakness |
| --- | --- | --- |
| Tags | Multi-dimensional (one stack can have many tags) | No permissions tied to tags — purely visual |
| [Projects](projects.md) | Permission boundary (per-project RBAC) | One stack belongs to exactly one project |
| Stack name conventions (e.g. `prod-aws-network`) | Visible everywhere including CLI/API | Rigid — can't filter by a single dimension easily |

The three are complementary. A common setup: stacks belong to a project (for permissions), are tagged for filtering, and follow a naming convention for legibility in logs and CLI output.

---

## Creating tags

In the UI: **Settings → Tags → New tag**.

| Field | Required | Notes |
| --- | --- | --- |
| Name | yes | Unique within the org; case-sensitive. Use `key:value` if you want structure (`env:prod`) or single labels (`critical`). |
| Color | no | Hex code (default `#6B7280`, neutral grey). Pick something that pops against the UI background. |

Tag names are flat strings — Crucible doesn't enforce `key:value` semantics. The colon is a visual convention, not a data structure.

Recommended starter set:

| Tag | Color | Used for |
| --- | --- | --- |
| `env:prod` | red | Production stacks |
| `env:staging` | yellow | Staging stacks |
| `env:dev` | green | Development stacks |
| `critical` | red | Stacks where breakage = outage |
| `team:<name>` | each team's brand | Ownership |
| `cloud:aws` / `cloud:gcp` / `cloud:azure` | provider colors | Target platform |

---

## Attaching tags to a stack

Stack detail → **Settings → Tags** → click tags to toggle on or off, then **Save**.

Or via API:

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tag_ids":["<tag-id-1>","<tag-id-2>"]}' \
  https://crucible.example.com/api/v1/stacks/$STACK_ID/tags
```

`PUT` replaces the entire tag set on the stack. To add a tag without losing existing ones, fetch first and merge:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://crucible.example.com/api/v1/stacks/$STACK_ID/tags
```

---

## Filtering by tag

The **Stacks** list page has a tag filter at the top. Click any tag chip to filter; click again to remove. Combine multiple tags for an AND filter (e.g. `env:prod` + `cloud:aws` shows only prod AWS stacks).

The same filter works on the runs list and on dashboards.

---

## Editing and deleting tags

**Edit** (rename or recolor): **Settings → Tags → click the tag**.

Renaming is allowed and propagates everywhere — the change is purely cosmetic; the underlying tag ID is unchanged.

**Delete**: requires `admin` role. Deleting a tag removes it from all attached stacks (`ON DELETE CASCADE`). There's no archive or soft-delete — deletion is final.

---

## Audit events

Every tag change is recorded:

- `tag.created` / `tag.updated` / `tag.deleted`
- `stack.tags_set` — the full new tag list, with diff against previous

Stream to your SIEM via [SIEM streaming](../operator-guide.md#siem-audit-log-streaming) if you use tags for compliance grouping.

---

## Patterns

### Map tags to your blast-radius policy

If your approval policy is "any stack tagged `critical` needs two approvers", encode the tag name in the policy:

```rego
package crucible

approval := {"required_approvers": 2} if {
  "critical" in input.stack.tags
}

approval := {"required_approvers": 1} if {
  not "critical" in input.stack.tags
}
```

Now anyone tagging a new stack as `critical` automatically inherits the stronger approval gate.

### Tag-driven cost rollups

Pair tags with the Infracost integration (Settings → Integrations → Infracost API key). The **Analytics → Costs** page lets you filter cost charts by tag, giving you per-environment or per-team cost views without standing up a separate billing system.

### Tag promotion as part of release

When you promote a stack from staging to prod (i.e. point it at a new branch or repo), update the tag in the same PR/release. Use it as a visual confirmation that the promotion has happened — "the stack is now red, we're live."

---

## What's next

- [Projects](projects.md) — when tagging alone isn't enough to manage who can do what.
- [Policies](../policies.md) — read tags inside Rego to drive approval / blast-radius decisions.
- [Variable Sets](variable-sets.md) — pair env-overlay sets with `env:*` tags so a stack's environment is visible in two places.
