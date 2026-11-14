# One Kargo control plane, one Argo CD + Kargo shard per region

The demo mirrors the target setup: one Kargo instance and one Argo CD per region. Each region runs a Kargo controller (a **shard**) next to its Argo CD, and it connects back to the central Kargo control plane. The control plane has no privileged access to the region clusters ([Kargo: distributed configuration](https://docs.kargo.io/operator-guide/architecture#distributed-configuration)).

Locally, these are kind clusters on the docker network `gitops-cd`:

- **Hub** `gitops-cd`: the Kargo control plane (API, management controller, webhooks, and the default controller, which runs Warehouses, control-flow Stages and non-K8s target Stages), Keycloak, Traefik (the only cluster bound to host 80/443), cert-manager and break-glass. Its Argo CD manages hub add-ons only, no workloads.
- **Regions** `region-<r>` (`us-east4`, `dc1`; from the cluster registry in `system/config.go`): their own self-managed Argo CD, a Kargo controller with `shardName: <r>`, and the workloads.

How it fits together (deploy wiring: ADR 0017):

- **Renderer**: each cluster has a `region`. K8S target Stages get `spec.shard: <region>`. Argo CD params and AppProjects go under `rendered/argocd/{deploys,projects}/<region>/`.
- **Shard identity**: on the hub, a second Kargo chart release per region (`dataPlane.install: false`, or it re-renders the main release's Roles) creates only that controller's ServiceAccount, RBAC and token Secret. In the region, a controller-only release reads the hub through `kubeconfigSecrets.kargo`. It also needs a Role for `events` in `argocd`, which the chart doesn't grant.
- **Token handoff**: `gcd kargo-shards` runs on the host with every kube context and copies the hub token into the region's `kargo/kargo-hub-kubeconfig` (server `https://gitops-cd-control-plane:6443`). The hub never holds region credentials.
- **UIs**: each regional Argo CD is at `https://argocd-<r>.localhost`, through hub Traefik to an ExternalName Service `region-<r>-control-plane:30081` (plain HTTP proxying, no credentials). Login goes through Keycloak with a public PKCE client `argocd-<r>`. Regions reach the issuer `http://keycloak.localhost` through a CoreDNS rewrite (`gcd region-dns`, re-run by every `infra`). Kargo `api.argocd.urls` maps each shard to its regional UI.

## Considered Options

- **A single Argo CD managing every cluster (hub-and-spoke)**: the simplest, but it doesn't match the target setup, and the central controller would hold credentials to every cluster.
- **Hub Argo CD installs the regional add-ons**: the hub would need region credentials, which goes against Kargo's model.
- **Hand-written hub-side shard manifests**: they drift from the chart.

## Consequences

- `gcd up` builds three clusters, takes about 8–9 min and uses about 7 GiB.
- A new region is a cluster entry in `config.go`, then `gcd regions`, a merge and a clean `gcd up`.
- Shard controllers run without Argo Rollouts (`rollouts.integrationEnabled: false`), so Stage verification with AnalysisTemplates would need Rollouts in each region.
- After a docker restart, the hub IP behind the regional Keycloak rewrite can change. `gcd infra` or `region-dns` fixes it.
- `argocd-update` must pin `desiredRevision` to a commit SHA. A branch name leaves the Promotion Running forever.
