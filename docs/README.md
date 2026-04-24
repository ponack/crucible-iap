# Crucible IAP — Documentation

Start here if you're setting up Crucible or integrating it with your stack.

## Getting started

- [**Quickstart**](quickstart.md) — 10-minute local walkthrough (HTTP, local auth, a `random_pet` Terraform stack). The fastest way to see Crucible run.
- [**Operator guide**](operator-guide.md) — production deployment, TLS, OIDC SSO, cloud workload identity federation, reverse proxy setups.
- [**Architecture**](architecture.md) — services, data flow, and how API / Worker / runner containers interact.
- [**Security**](security.md) — threat model, encryption at rest, secret handling, hardening notes.
- [**Roadmap**](roadmap.md) — what's shipped, what's next.

## Guides (integrations)

- [AWS](guides/aws.md) · [GCP](guides/gcp.md) · [Azure](guides/azure.md) · [Proxmox](guides/proxmox.md) — cloud / hypervisor setup
- [Ansible](guides/ansible.md) · [Pulumi](guides/pulumi.md) — non-Terraform tooling
- [Webhooks](guides/webhooks.md) — GitHub / GitLab / Gitea / Gogs
- [Remote state](guides/remote-state.md) — built-in HTTP backend and S3/GCS/Azure overrides
- [Drift detection](guides/drift-detection.md) · [Run hooks](guides/run-hooks.md) · [Stack dependencies](guides/stack-dependencies.md)
- [Team setup](guides/team-setup.md) — org, roles, invites, per-stack membership

## Policy

- [Policy reference](policies.md) — OPA/Rego hooks, inputs, outputs
- [Example policies](policies/README.md) — ready-to-use `.rego` samples (approval gates, blast-radius guards, tag requirements, region allowlists, destroy protection)
