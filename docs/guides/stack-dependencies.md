# Stack Dependencies Guide

Stack dependencies let you chain infrastructure stacks so that a successful apply on one stack automatically triggers a run on another. This is the primary way to build multi-layer infrastructure pipelines in Crucible — for example, applying a networking stack before a compute stack, or deploying a database before an application.

## Contents

1. [How dependencies work](#how-dependencies-work)
2. [Setting up dependencies](#setting-up-dependencies)
3. [Common pipeline patterns](#common-pipeline-patterns)
4. [Cycle detection](#cycle-detection)
5. [Monitoring a dependency chain](#monitoring-a-dependency-chain)
6. [Removing a dependency](#removing-a-dependency)

---

## How dependencies work

When an upstream stack completes a successful apply, Crucible automatically enqueues a `tracked` run on every downstream stack linked to it. The downstream run goes through the normal plan → confirm → apply lifecycle (or applies automatically if the downstream stack has auto-apply enabled).

Key behaviours:

- **Only successful applies trigger downstream runs** — a failed apply, a discarded run, or a proposed (plan-only) run does not trigger downstream stacks.
- **Downstream runs are independent** — each downstream stack goes through its own plan, and each plan must be confirmed separately (unless auto-apply is on). The upstream run is not held waiting for downstream runs to complete.
- **One upstream can have many downstreams** — the trigger fans out in parallel to all configured downstream stacks.
- **Chains work transitively** — if A triggers B and B triggers C, a successful apply on A will eventually trigger C (after B applies successfully).

---

## Setting up dependencies

### Add a downstream stack

Open the **upstream** stack (the one that should trigger another stack after it applies):

1. Scroll to the **Dependencies** section on the stack detail page
2. Click **Add downstream**
3. Select the stack that should be triggered from the dropdown
4. Click **Add**

The downstream stack now appears in the **Downstream stacks** list. After the next successful apply on this stack, the downstream will receive a new run automatically.

### View the dependency graph

Both upstream and downstream relationships are shown on each stack's detail page:

- **Upstream stacks** — stacks that must apply before this one can be triggered
- **Downstream stacks** — stacks that will be triggered after this one applies

---

## Common pipeline patterns

### Networking → Compute

A typical three-layer setup where each layer depends on outputs from the layer above:

```
networking-stack
    └── compute-stack
            └── app-stack
```

1. `networking-stack` manages VPCs, subnets, and security groups. It exports outputs (subnet IDs, VPC ID).
2. `compute-stack` reads those outputs via `terraform_remote_state` (see [remote-state.md](remote-state.md)) and manages EC2 instances, EKS clusters, etc.
3. `app-stack` reads compute outputs and manages application-layer resources (load balancers, DNS, etc.).

Set up the dependency chain so that a push to the networking repo automatically flows through all three layers.

### Shared infra → Multiple consumers

A shared infrastructure stack that multiple independent application stacks depend on:

```
shared-infra-stack
    ├── app-a-stack
    ├── app-b-stack
    └── app-c-stack
```

When the shared infra applies (e.g. a new IAM role or updated security group), all three consumer stacks are triggered simultaneously to pick up the change.

### Database → Application

```
postgres-stack
    └── api-stack
```

After the PostgreSQL stack applies a schema migration, the API stack is triggered to pick up any environment changes (e.g. a new connection string output).

---

## Cycle detection

Crucible prevents dependency cycles at the API level. If adding a dependency would create a cycle (A → B → A), the request is rejected with a `409 Conflict` response and the cycle is not created.

The cycle check is transitive — Crucible walks the full reachability graph of the target downstream stack before allowing the new link.

If you need a bidirectional relationship (uncommon in practice), model it as two independent stacks with separate concerns rather than circular triggers.

---

## Monitoring a dependency chain

Each triggered downstream run shows `trigger=auto_trigger` in the run history, making it easy to trace which upstream apply caused it.

To see the full chain for a given upstream run:

1. Open the upstream stack → find the run that applied
2. Note the run completion time
3. Open each downstream stack and look for runs created around the same time with `trigger=auto_trigger`

For complex pipelines, attach a `trigger` OPA policy to the upstream stack to record chain events in the audit log or send notifications. See [policies.md](../policies.md) for the `trigger` hook syntax.

### OPA trigger policy example

Use a policy on the upstream stack to post a Slack message when the chain kicks off:

```rego
package crucible

plan := result if {
  result := {
    "deny":    [],
    "warn":    [],
    "trigger": [],
  }
}
```

Stack dependencies are configured directly in the UI and don't require a policy — the above is only needed if you want side effects (notifications, audit records) beyond the automatic run trigger.

---

## Removing a dependency

Open the upstream stack → **Dependencies** → find the downstream stack in the list → click **Remove**.

Removing a dependency does not affect any currently running or pending runs. It only prevents future automatic triggers.
