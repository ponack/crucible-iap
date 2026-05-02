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
- [Cloudflare](guides/cloudflare.md) — bootstrap with cf-terraforming, Crucible stack setup, OPA policies
- [Ansible](guides/ansible.md) · [Pulumi](guides/pulumi.md) — non-Terraform tooling
- [Provider registry](guides/provider-registry.md) — private Terraform provider registry, GPG signing, air-gapped deployments
- [Webhooks](guides/webhooks.md) — GitHub / GitLab / Gitea / Gogs
- [Remote state](guides/remote-state.md) — built-in HTTP backend and S3/GCS/Azure overrides
- [Drift detection](guides/drift-detection.md) · [Run hooks](guides/run-hooks.md) · [Stack dependencies](guides/stack-dependencies.md)
- [Team setup](guides/team-setup.md) — org, roles, invites, per-stack membership
- [Stack templates](guides/stack-templates.md) · [Blueprints](guides/blueprints.md) — reusable configs and self-service deployment

## Migration

- [Spacelift → Crucible](guides/spacelift-migration.md) — concept mapping, state migration options, worked example

## Policy

- [Policy reference](policies.md) — OPA/Rego hooks, inputs, outputs
- [Policy GitOps](guides/policy-gitops.md) — sync `.rego` files from a git repo, webhook setup, mirror mode
- [Example policies](policies/README.md) — ready-to-use `.rego` samples (approval gates, blast-radius guards, tag requirements, region allowlists, destroy protection)
