# One Kargo control plane, one Argo CD + Kargo shard per region

Status: accepted. Supplements ADR 0012: one Kargo Stage per target, gates unchanged.

The demo now mirrors the target setup: a single Kargo instance and several Argo CD instances, one per region. Kargo controllers pair 1:1 with Argo CD control planes. A controller (a **shard**) runs next to its Argo CD and connects back to the central Kargo control plane. The control plane has no privileged access to the shard clusters ([Kargo: distributed configuration](https://docs.kargo.io/operator-guide/architecture#distributed-configuration)).

Locally, all clusters are kind clusters on the docker network `kargo-demo`:

- **Hub** `kargo-demo`:
  - Kargo control plane: API, management controller, webhooks, and the default controller, which runs gates and Warehouses.
  - Keycloak, Traefik (the only cluster bound to host 80/443), cert-manager, break-glass.
  - A platform Argo CD for hub add-ons only. No workloads.
- **Regions** `region-<r>` (list: `platform/cluster/regions`; today `us` and `onprem`):
  - Their own self-managed Argo CD.
  - A Kargo controller with `shardName: <r>`.
  - The workloads.

Wiring:

- **Renderer.** Each `#Cluster` has a required `region`.
  - Target Stages get `spec.shard: <region>`. Gates and Warehouses get no shard and run on the hub controller.
  - Argo CD output goes to `rendered/argocd/<region>/<cap>.yaml`. Each file has one AppProject (that region's namespaces only) and the region's Applications, with destination `in-cluster`.
- **Bootstrap.**
  - The hub syncs only `rendered/kargo`.
  - Each region's Argo CD has a root app for `rendered/argocd/<region>`.
- **Shard identity.**
  - On the hub, a second Kargo chart release per region creates only that controller's ServiceAccount, RBAC and token Secret. It must set `dataPlane.install: false`, or it re-renders the main release's Roles.
  - In the region, a controller-only release reads the hub through `kubeconfigSecrets.kargo`. It also needs a Role for `events` in `argocd`, which the chart doesn't grant.
- **Token handoff.**
  - `mise run kargo-shards` runs on the host with every kube context.
  - It copies the hub token into the region's `kargo/kargo-hub-kubeconfig`, with server `https://kargo-demo-control-plane:6443`.
  - The hub never holds region credentials.
- **UIs.**
  - Each regional Argo CD is reached at `https://argocd-<r>.localhost` through hub Traefik: an IngressRoute to an ExternalName Service `region-<r>-control-plane:30081` (needs Traefik `allowExternalNameServices`). That is plain HTTP proxying, not credentials.
  - Login uses Keycloak with a public PKCE client `argocd-<r>` per region, generated from the regions list. RBAC is email-only (ADR 0011) with the admin emails (ADR 0009).
  - Regions reach the issuer `http://keycloak.localhost` through a CoreDNS rewrite to a selectorless Service, which points at the hub container's Traefik NodePort. `mise run region-dns` re-resolves it on every `infra`.
  - Kargo `api.argocd.urls` maps each shard to its regional UI. The default shard has no Argo CD link.
- **Demo targets.** One per region per stage, with eu removed, so every stage fans out across both shards.

## Considered Options

- **One Kargo Stage per (app, stage) promoting all targets.** This gives a cleaner Kargo graph. It isn't possible here: a Stage runs on one shard, and a shard talks to one Argo CD, so a stage spanning regions needs one Stage per region. One per target is the same thing when each region has one target per stage.
- **Single Argo CD managing every cluster (hub-and-spoke).** Simplest, but it doesn't match the target setup, and the central controller would hold credentials to every cluster.
- **Hub Argo CD installs the regional add-ons.** One bootstrap path, but the hub would need region credentials, which goes against Kargo's model. Rejected: each region bootstraps itself from the host.
- **Hand-written hub-side shard manifests.** They worked in the spike but drift from the chart. Rejected once the `dataPlane.install: false` split was found.

## Consequences

- `mise run up` builds three clusters, takes about 8–9 min, and uses about 7 GiB (hub ≈3.3–4.6 GiB, region ≈1.6–1.8 GiB). Adding a region is one line in `platform/cluster/regions` plus a cluster entry in `platform/render/clusters.cue`.
- The Kargo pipeline view still shows gate + target nodes per stage. The clutter comes with the per-region model.
- Shard controllers run without Argo Rollouts (`rollouts.integrationEnabled: false`). Stage verification with AnalysisTemplates would need Argo Rollouts in each region.
- After a docker restart the hub IP behind the regional Keycloak rewrite can change. `mise run infra` (or `region-dns`) fixes it.
- `argocd-update` must pin `desiredRevision` to a commit SHA. A branch name leaves the Promotion Running forever.
- On the first push after this change, the regional root apps show ComparisonError until CI renders the new layout. The old hub workload apps are deleted by `infra`. Old `env/*` branches for removed or renamed targets are orphaned and need deleting by hand.
