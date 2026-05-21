# Guide: Kubernetes & Helm with Crucible IAP

Managing Kubernetes with Infrastructure as Code involves two distinct layers:

1. **The cluster itself** — provisioning an EKS / GKE / AKS / DOKS / Hetzner / on-prem cluster.
2. **Resources inside the cluster** — namespaces, deployments, ingresses, secrets, Helm releases.

Both can run from a Crucible stack. This guide covers both, with a focus on common pitfalls when managing in-cluster resources.

---

## Decision: one stack or two?

Two stacks is almost always the right answer:

```text
┌─────────────────────┐         ┌─────────────────────────┐
│ Stack: cluster-prod │ ──────► │ Stack: workloads-prod   │
│                     │ depends │                          │
│  - VPC              │   on    │  - namespaces            │
│  - EKS cluster      │         │  - Helm releases         │
│  - Node groups      │         │  - cluster-wide CRDs     │
└─────────────────────┘         └─────────────────────────┘
```

Why split:

- The cluster changes rarely; workloads change daily. Independent blast radius.
- Cluster destruction wipes state of in-cluster resources too — the workload stack's state stays clean.
- Different teams often own them (SRE owns the cluster, app teams own workloads).

Use [stack dependencies](stack-dependencies.md) so a successful apply of the cluster stack triggers the workload stack.

If you do put both in one stack, accept that `terraform destroy` becomes a "scorched earth" button — use carefully.

---

## Authenticating to the cluster

Crucible's runner has no kubeconfig by default. You provide credentials one of three ways:

### Option A — Cluster outputs feed the workload stack

The cluster stack outputs everything the workload stack needs to authenticate. The workload stack reads them via `terraform_remote_state`:

```hcl
# Workload stack
data "terraform_remote_state" "cluster" {
  backend = "http"
  config = {
    address  = "https://crucible.example.com/api/v1/state/<cluster-stack-id>"
    username = "<cluster-stack-slug>"
    password = "<cluster-stack-token>"
  }
}

provider "kubernetes" {
  host                   = data.terraform_remote_state.cluster.outputs.cluster_endpoint
  cluster_ca_certificate = base64decode(data.terraform_remote_state.cluster.outputs.cluster_ca)
  token                  = data.terraform_remote_state.cluster.outputs.cluster_token
}
```

This works but needs a per-cloud auth flow to produce the token. For EKS it's IAM auth; for GKE it's gcloud-style impersonation; for self-hosted it's a service account token.

### Option B — Kubeconfig in a stack env var

Encode your kubeconfig as base64 and store as a **Secret** env var:

```bash
cat ~/.kube/config | base64 -w0
```

Stack env var:

| Name | Value | Secret |
| --- | --- | --- |
| `KUBECONFIG_B64` | base64-encoded kubeconfig | yes |

In a [pre-plan run hook](run-hooks.md):

```bash
echo "$KUBECONFIG_B64" | base64 -d > /tmp/kubeconfig
export KUBECONFIG=/tmp/kubeconfig
```

Then in your provider:

```hcl
provider "kubernetes" {
  config_path = "/tmp/kubeconfig"
}
```

Simple, but rotates poorly — the embedded credentials in the kubeconfig age out and need refreshing.

### Option C — Cloud OIDC federation (recommended for managed clusters)

Crucible mints a short-lived OIDC token; AWS/GCP/Azure exchange it for a cloud credential, and the cloud SDK then talks to the EKS/GKE/AKS auth endpoint to get a Kubernetes token.

```hcl
provider "kubernetes" {
  host                   = aws_eks_cluster.this.endpoint
  cluster_ca_certificate = base64decode(aws_eks_cluster.this.certificate_authority[0].data)
  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = ["eks", "get-token", "--cluster-name", aws_eks_cluster.this.name]
  }
}
```

Pair with stack-level Cloud OIDC federation pointing at an IAM role that has `eks:DescribeCluster` and is mapped to a Kubernetes group via `aws-auth` ConfigMap. See [`operator-guide.md#cloud-oidc-workload-identity-federation`](../operator-guide.md#cloud-oidc-workload-identity-federation).

The runner image includes `aws`, `gcloud`, and `kubectl` CLIs for this pattern.

---

## Provider versions

```hcl
terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.32"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.16"
    }
  }
}
```

Both providers share the same auth config. If you set up the `kubernetes` provider, copy the same `host`/`cluster_ca_certificate`/`token-or-exec` block under `provider "helm" { kubernetes { ... } }`.

---

## Helm releases

```hcl
resource "helm_release" "nginx" {
  name       = "ingress-nginx"
  repository = "https://kubernetes.github.io/ingress-nginx"
  chart      = "ingress-nginx"
  version    = "4.11.3"
  namespace  = "ingress-nginx"

  create_namespace = true

  values = [yamlencode({
    controller = {
      replicaCount = 2
      resources = {
        requests = {
          cpu    = "100m"
          memory = "128Mi"
        }
      }
    }
  })]
}
```

Common pitfalls:

- **Don't use `set` for many values.** It's positional, hard to read, and easy to break. Use `values = [yamlencode({...})]` or `values = [file("values.yaml")]`.
- **Pin chart versions.** Without `version`, Helm picks the latest each plan and you'll see drift constantly.
- **Beware of timeouts on first install.** Charts that wait for resources to become ready (`atomic = true`, `wait = true`) can exceed the runner's job timeout. Set the stack's job timeout higher or use `wait = false` for first installs.

---

## Managing CRDs

CRDs are a common stumbling block.

- **The provider doesn't manage CRDs themselves.** Install them out-of-band (`kubectl apply -f`) or via a Helm chart that ships them, *then* let Terraform manage custom resources of that kind.
- **Use `kubernetes_manifest` for raw CRDs.** Works for arbitrary kinds but has stricter validation than `kubectl apply` — schemas must match exactly.

Pattern: cluster stack installs CRDs via Helm. Workload stack creates custom resources using `kubernetes_manifest`.

---

## Recommended OPA policies

### Block privileged containers

```rego
package crucible

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "kubernetes_deployment"
  c := resource.change.after.spec[_].template[_].spec[_].container[_]
  c.security_context[_].privileged == true
  msg := sprintf("container %q in %q sets privileged=true — not allowed", [c.name, resource.address])
}
```

### Require resource limits

```rego
package crucible

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "kubernetes_deployment"
  c := resource.change.after.spec[_].template[_].spec[_].container[_]
  not c.resources[_].limits.memory
  msg := sprintf("container %q in %q missing memory limit", [c.name, resource.address])
}
```

### Pin Helm chart versions

```rego
package crucible

deny[msg] {
  resource := input.plan.resource_changes[_]
  resource.type == "helm_release"
  not resource.change.after.version
  msg := sprintf("helm_release %q must pin a 'version' — unpinned charts cause drift", [resource.address])
}
```

---

## Common errors

### "Error: Get \"https://...\": no such host"

Cluster endpoint URL is wrong or the cluster doesn't exist yet. If using `data.terraform_remote_state`, confirm the cluster stack has applied successfully and exports the right output names.

### "Error: connection refused"

Cluster endpoint is reachable but the Kubernetes API server isn't responding. The cluster might be in the middle of creation or upgrade. Retry.

### "Error: Unauthorized" / "Error: forbidden"

The runner's credentials don't grant access. For EKS, confirm the IAM role is mapped in the `aws-auth` ConfigMap. For GKE, confirm the service account has `container.developer` or finer-grained roles. For self-hosted, confirm the kubeconfig user has the necessary RBAC bindings.

### "Error: namespace ... not found"

The Helm release / manifest references a namespace that doesn't exist yet. Either create it with `kubernetes_namespace` and add a `depends_on`, or set `create_namespace = true` on the helm_release.

### Helm release stuck "pending-install"

A previous install was interrupted. The Helm release object exists in `pending-install` state. Manually clean it:

```bash
helm uninstall --keep-history=false <release> -n <namespace>
```

Then re-trigger the run.

---

## What's next

- [stack-dependencies.md](stack-dependencies.md) — wire cluster stack outputs to the workload stack.
- [aws.md](aws.md) / [gcp.md](gcp.md) / [azure.md](azure.md) — for managed-cluster auth specifics on each cloud.
- [policies.md](../policies.md) — write Kubernetes-specific Rego.
- [drift-detection.md](drift-detection.md) — particularly useful for K8s; in-cluster changes happen often.
