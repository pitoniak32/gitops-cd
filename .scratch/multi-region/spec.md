# Multi-region: one Kargo, one Argo CD per region

Replicate the target setup locally: a single Kargo control plane driving several Argo CD instances, one per region. Regions in the demo: `us`, `onprem`. The eu targets are dropped.

Reference: Kargo distributed configuration, https://docs.kargo.io/operator-guide/architecture#distributed-configuration
- Controllers are 1:1 with Argo CD control planes. Each controller is a **shard** and "phones home" to the Kargo control plane.
- The control plane has **no privileged access** to the clusters where controllers run.
- Per controller: a ServiceAccount in the control plane, with its token distributed to the controller's cluster.

## Topology (kind, docker network `kargo-demo`)

| kind cluster | Runs |
|---|---|
| `kargo-demo` (hub) | Kargo control plane: API, management controller, webhooks, default controller (gates + Warehouses). Keycloak, Traefik, cert-manager, break-glass. Platform Argo CD for **hub add-ons only**. |
| `region-us` | Argo CD (self-managed), Kargo controller `shardName: us`, us workloads |
| `region-onprem` | Argo CD (self-managed), Kargo controller `shardName: onprem`, on-prem workloads |

The hub never gets credentials to the regions. `mise` runs on the host with every kube context and:
- bootstraps each region's Argo CD;
- copies each shard's hub token into its region (Secret `kargo/kargo-hub-kubeconfig`, server `https://kargo-demo-control-plane:6443`).

## Spike results (2026-11-08, hub + region-us, Kargo 1.12.5)

Proven end to end. A Stage with `spec.shard: us` on the hub was promoted by the controller in region-us, whose `argocd-update` synced an Application in region-us's Argo CD. The Stage went Healthy and verified.

Findings:
- **Shard field:** Stage shard is `spec.shard`. The `kargo.akuity.io/shard` label is derived from it by a webhook; setting only the label does nothing.
- **Hub side:** a second Kargo chart release per shard (`kargo-ctrl-<shard>`, ns `kargo`) renders only the shard's identity and RBAC, with no collisions with the main release:
  ```yaml
  workloads: {install: false}
  dataPlane: {install: false}   # shared data plane (CRDs, Roles, webhooks) belongs to the main release
  argocd: {dataPlane: {install: false, kargoController: {install: false}}}  # Argo CD lives in the region
  controller: {id: <shard>}
  extraObjects: [<Secret kargo-controller-<shard>-token, type service-account-token>]
  ```
  It renders: SA, ClusterRoleBindings → `kargo-controller`/`kargo-controller-rollouts`, RoleBinding → Role `kargo-controller`, and shared/system-resources reader RoleBindings (git creds). The first spike attempt kept `dataPlane.install: true`, which also renders the shared Roles; that caused the collision.
- **Region side:** the chart works with these values:
  ```yaml
  dataPlane: {install: false, controller: {install: false}}
  argocd: {dataPlane: {install: true}}   # local ClusterRole/Binding for Applications
  api: {enabled: false}
  externalWebhooksServer: {enabled: false}
  garbageCollector: {enabled: false}
  webhooksServer: {enabled: false}
  managementController: {enabled: false}
  kubeconfigSecrets: {kargo: kargo-hub-kubeconfig}
  controller: {id: <shard>, shardName: <shard>, rollouts: {integrationEnabled: false}}
  # argocd-update records events on the Application; chart Argo CD RBAC lacks them
  extraObjects: [Role + RoleBinding: events create/patch in ns argocd]
  ```
- **Memory:** hub 4.9 GiB, region 1.6 GiB (minimal Argo CD: no dex/notifications/appset). That's about 8 GiB of the 11.7 GiB Docker has, for hub + 2 regions.

## Design

- **Cluster registry:** `clusters.cue` swaps `in-cluster` for `us` and `onprem`. Each entry has `region` (= Kargo shard = Argo CD instance). Targets pick a cluster as today.
- **Renderer:**
  - Target Stages get `spec.shard: <region>`. Gates and Warehouses stay on the default (hub) controller.
  - Applications move to `rendered/argocd/<region>/<cap>.yaml`, destination `in-cluster` of that region's Argo CD.
  - One AppProject per region per capability.
  - The `authorized-stage` annotation is unchanged.
- **Bootstrap:** one root Application per region syncing `rendered/argocd/<region>/` from GitHub `main`. The hub keeps `rendered/kargo/`.
- **Regional add-ons:** `platform/infrastructure/` gets region add-ons: argocd-region and kargo-shard. The `infra` task applies them per context, in the same inline-from-checkout style.
- **Hub shard identities:** generated from a shard list, so adding a region means adding one list entry.
- **UIs** (after the core works; login comes later):
  - Regional Argo CD UIs are proxied by hub Traefik at `argocd-us.localhost` and `argocd-onprem.localhost`.
  - Each gets a Keycloak client and the email-based AppProject viewer roles (ADR 0011).
  - Kargo `api.argocd.urls` maps each shard to its UI for deep links.
- **Workloads:** none on the hub; every target lives in a region. This is the main goal.
- **Demo targets:** reshape freely to show the structure with few targets. Every stage gets a target in both regions, so each stage fans out across shards in parallel:
  - payments: `us-dev`, `onprem-dev`, `us-stg`, `onprem-stg`, `us-perf`, `us-prod`, `onprem-prod`
  - search: `us-dev`, `us-prod`, `onprem-prod`
  - all eu targets removed

## Decisions (2026-11-08)
- No workloads on the hub (main goal).
- Regional Argo CD UI login comes later (ticket 05 after the core works).
- Targets can be reduced or renamed for the demo.
- Hub-side shard identity via a chart release, not hand-written manifests.

## Risks / open

- **Keycloak from regions:** regional pods need `http://keycloak.localhost` to resolve to hub Traefik, which is hub container `:30080` on the docker network, not `:80`. The likely fix is a CoreDNS rewrite plus a selector-less Service whose Endpoints are set by `mise` to the hub container IP.
- **Docker IP changes:** container IPs can change across docker restarts, so the `infra` task must re-resolve them (idempotent).
- **Rollouts verification (open):** AnalysisRuns for Stage verification are created by the shard controller. Should regions get argo-rollouts, or stay `integrationEnabled: false` until verification is used?
- **Startup time:** a clean `mise run up` takes longer (3 clusters). The regions could be created in parallel.

## Tickets

`issues/01`–`06`.
