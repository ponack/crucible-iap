# Guide: Projects

Projects are a hierarchical layer between organizations and stacks. They let you group related stacks together and give different teams access to different groups within a single Crucible deployment.

```text
Organization
├── Project: platform-infra        ← platform team owns
│   ├── Stack: prod-network
│   ├── Stack: prod-kubernetes
│   └── Stack: prod-vault
├── Project: marketing-site        ← marketing team owns
│   ├── Stack: marketing-dev
│   └── Stack: marketing-prod
└── Project: data-platform         ← data team owns
    ├── Stack: airflow-prod
    └── Stack: warehouse-prod
```

Projects are optional. Small teams can put every stack at the org root and ignore the project layer entirely. Larger orgs use projects to enforce who can see, run, and configure each group of stacks.

---

## When to use projects

Use projects when:

- **Multiple teams share one Crucible deployment** — give each team admin over their own project without giving them admin over the whole org.
- **You want clean separation between environments managed by different people** — e.g. SRE owns infra, app teams own service stacks.
- **You're an MSP / consultancy serving multiple clients in one org** — one project per client.

Skip projects when:

- You're a single small team where everyone has full access.
- All stacks are owned by the same group of people.
- You're just getting started — add projects later when the need is concrete.

---

## How project RBAC works

Project membership has three roles:

| Project role | Can do within the project |
| --- | --- |
| `viewer` | Read stacks, runs, plans, audit log |
| `member` | All viewer + create stacks, trigger runs, manage env vars |
| `admin` | All member + delete stacks, edit project settings, manage project members |

**Important:** project roles add to org roles, they don't replace them. An org `admin` can do everything everywhere regardless of project membership. Project membership is the way to grant *additional* access to users who are otherwise `viewer` at the org level.

A typical setup:

- All employees are org `viewer` (read-only visibility into everything).
- Each team gets `member` or `admin` on their own project.
- A few platform engineers are org `admin` for break-glass scenarios.

---

## Creating a project

In the UI: **Projects → New project**.

| Field | Required | Notes |
| --- | --- | --- |
| Name | yes | Human-readable, shown in lists |
| Slug | yes | URL-safe identifier, lowercase alphanumeric + hyphens |
| Description | no | Optional explanation of what the project covers |

Via the API:

```bash
curl -X POST https://crucible.example.com/api/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "data-platform",
    "name": "Data Platform",
    "description": "Airflow, warehouses, ETL pipelines"
  }'
```

---

## Assigning stacks to a project

A stack belongs to at most one project (or none). Assign on the stack's **Settings** page → **Project** dropdown.

When you delete a project, contained stacks are not deleted — their `project_id` is set to NULL and they revert to the org root. Use this if you want to retire a project without losing its stacks.

---

## Adding members

Project detail page → **Members** tab → **Add member**.

Pick a user from the org (they must already be an org member — projects don't invite new users), assign their role, and save. Changes take effect on their next API call; no re-login needed.

Bulk-mapping from SSO groups is on the roadmap. For now, add members manually after they join the org.

---

## Project-scoped CLI

The `crucible` CLI accepts a `--project` filter on stack listing:

```bash
crucible stacks list --project <project-id>
```

Combine with `--json` and `jq` for scripted filtering:

```bash
crucible stacks list --project <project-id> --json | \
  jq -r '.[] | select(.last_run_status == "failed") | .name'
```

---

## Typical multi-project layout

A realistic example for a 50-person company:

```text
Organization: acme-corp
│
├── Project: shared-infra          (admins: SRE)
│   ├── prod-vpc
│   ├── prod-dns
│   ├── prod-iam-baseline
│   └── prod-kubernetes
│
├── Project: app-checkout          (admins: checkout-team)
│   ├── checkout-dev
│   ├── checkout-staging
│   └── checkout-prod
│
├── Project: app-search            (admins: search-team)
│   ├── search-dev
│   ├── search-staging
│   └── search-prod
│
└── Project: data                  (admins: data-platform-team)
    ├── data-prod-redshift
    └── data-prod-airflow
```

- SRE has `admin` on `shared-infra`, `viewer` on the others.
- Each app team has `admin` on their own project, `viewer` on `shared-infra` and `data`.
- All employees are org `viewer` — they can see everything but only act inside their own projects.

This pattern lets every team move independently without stepping on each other, while keeping a unified audit log and policy framework across the org.

---

## Renaming and archiving

Renaming a project preserves its ID; URLs and stack assignments continue to work.

There is no archive flag yet — to take a project offline, remove all members and set the org `viewer` role to not include it in the project list (handled automatically when there are no project members).

To fully delete a project: **Project detail → Settings → Delete project**. Contained stacks revert to the org root.

---

## Comparison to other tools

- **Spacelift:** "Spaces" are roughly equivalent to Crucible projects. Spaces also nest; Crucible projects are flat (one level deep).
- **Terraform Cloud:** "Projects" are nearly identical in concept and name.
- **env0:** "Projects" again — same pattern.

If you're migrating from Spacelift Spaces with nesting, flatten the tree: each leaf space becomes a Crucible project. Crucible doesn't currently support hierarchical projects.

---

## What's next

- [Team setup](team-setup.md) — full RBAC model, approval policies, recommended starter set.
- [Blueprints](blueprints.md) — combine with projects to give each team self-service stack creation within their own boundary.
- [Operator guide: multi-org](../operator-guide.md#multi-org-administration) — for when one Crucible needs to host completely separate tenants (different companies, different MSP customers).
