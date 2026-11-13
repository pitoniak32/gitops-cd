# ApplicationSets + cluster generator for add-ons and workloads

Date: 2026-11-08. Status: approved design, pre-plan.
Ref: https://argo-cd.readthedocs.io/en/stable/operator-manual/cluster-bootstrapping/

## Goal

Every Argo CD (hub + each region) runs the same root "app of ApplicationSets". ApplicationSets with the
cluster generator decide what each Argo CD deploys, from labels on its own `in-cluster` cluster Secret.
Covers both infrastructure add-ons and system workloads.

## Decisions (from brainstorming)

- Scope: add-ons **and** workloads.
- Topology: **keep per-region** Argo CD (ADR 0015). Hub holds no region credentials. Each Argo CD's
  cluster generator sees only its own labeled `in-cluster`.
- Add-on config: **git-sourced from `main`** (chart + `$values` ref + manifests path). No more inlining.
- Workloads: **matrix(cluster gen × git files gen)** over renderer-emitted params files.
- Migration: **clean `mise run up` only**; no in-place upgrade path.
- No `REVISION` override for testing unpushed infra changes.

## Cluster identity

- Each Argo CD declares a labeled `in-cluster` Secret (`argocd.argoproj.io/secret-type: cluster`,
  server `https://kubernetes.default.svc`) via argo-cd chart `configs.clusterCredentials`
  (or `extraObjects` if the chart can't label in-cluster).
  - Hub: `example.com/role: hub`.
  - Region: `example.com/role: region`, `example.com/region: <r>`.
- Regional Argo CD: enable ApplicationSet controller (today `applicationSet.replicas: 0`).

## Bootstrap

- `platform/bootstrap/root.yaml`: one Application, identical in every Argo CD, syncs
  `platform/appsets/` from GitHub `main`. Replaces `rendered.yaml`, `region.yaml` and the `__REGION__` sed.
- Initial `helm install argocd` stays (chicken-egg, as in the Argo CD doc). Regional helm bootstrap keeps
  its `__SHARD__` sed of `values.yaml` for that one install; the self-managed Application afterwards comes
  from the `argocd-region` AppSet.
- `platform/appsets/` contents:
  - `addons/<addon>.yaml`: one AppSet per add-on, cluster gen with label selector.
    - hub: traefik, cert-manager, argo-rollouts, keycloak, kargo, kargo-config, break-glass, argocd.
    - region: argocd-region, kargo-shard.
    - hub, one per region: kargo-shard-hub, argocd-region-hub (see Regions registry).
  - `kargo.yaml`: hub-only AppSet → Application syncing `rendered/kargo` (replaces `rendered-kargo`).
  - `projects.yaml`: region AppSet → Application `projects` syncing `rendered/argocd/projects/{{region}}/`
    (AppSets can't create AppProjects).
  - `workloads.yaml`: see Workloads.
- All templates: `goTemplate: true`, `goTemplateOptions: ["missingkey=error"]`.

## Add-on sourcing

- Template = multi-source:
  - chart, pinned `targetRevision` (as today in `app.yaml`);
  - repo `main`, `ref: values` → `valueFiles: [$values/platform/infrastructure/<addon>/values.yaml]`;
  - repo `main`, path `platform/infrastructure/<addon>/manifests` (kustomize) where present. Replaces the
    bedag/raw `resources` inlining.
- Per-region values (old `__SHARD__`): template `helm.valuesObject` / `parameters` with
  `{{index .metadata.labels "example.com/region"}}` (or the regions-registry param on hub-side AppSets).
- `kargo-shard` / `kargo-shard-hub` chart pin: hardcoded in the AppSet; `mise run test` asserts it equals
  the `kargo` pin (replaces `PINNED-IN-KARGO-APP`).
- `platform/infrastructure/<addon>/app.yaml` replaced by `platform/appsets/addons/<addon>.yaml`.
  `mise-tasks/infra-render` deleted.
- Existing sync options (CreateNamespace, ServerSideApply, retry) carried over; add
  `SkipDryRunOnMissingResource=true` so CRD/webhook ordering races self-heal via retry.

## Regions registry

- `platform/render/clusters.cue` is the single source of regions. Renderer emits:
  - `rendered/infrastructure/regions/<r>.json` (`{region}`): git files gen for hub-side per-region AppSets
    (kargo-shard-hub, argocd-region-hub).
  - `rendered/infrastructure/keycloak/argocd-clients.json`: per-region public PKCE clients (today generated
    in `infra-render`); keycloak manifests consume it (kustomize / realm import).
  - `rendered/infrastructure/kargo/values-regions.yaml`: `api.argocd.urls` per region; extra valueFile on
    the kargo AppSet.
- `platform/cluster/regions` stays for host tasks (`up`/`down`/`infra`/`kargo-shards`); `mise run test`
  asserts it matches `clusters.cue`.

## Workloads

- `workloads.yaml`: one AppSet, matrix:
  1. cluster gen, selector `example.com/role: region`;
  2. git files gen `rendered/argocd/deploys/{{region}}/*.json`.
- Renderer (`delivery.cue`) stops emitting Applications. Emits per region:
  - `rendered/argocd/deploys/<r>/<sys>-<app>-<target>.json`: name, system, app, stage, target,
    namespace, branch (`env/<sys>/<app>/<target>`), project, argocdName, kargo stage ref.
  - `rendered/argocd/projects/<r>/<sys>.yaml`: AppProject as today (region destinations, viewer roles).
- Template reproduces today's Application exactly: name `<sys>-<app>-<target>`, labels
  `example.com/{system,app,stage,target}`, annotation `kargo.akuity.io/authorized-stage`, automated
  prune/selfHeal, CreateNamespace. Same names → Kargo Stages unchanged.

## Kargo interplay

- `argocd-update` sets `operation` and pins `desiredRevision` SHA; it doesn't edit `spec`, so the AppSet
  controller shouldn't revert it. Verify in clean `up`: a promotion completes (not stuck Running).
  If AppSet reverts anything, add `ignoreApplicationDifferences`.

## Ordering (`mise-tasks/infra`)

- New flow per Argo CD: helm install if missing → apply root → wait **in today's order** for each generated
  Application Synced + Healthy (same list and waits as now). Order still lives in `infra`.
- Host-only steps unchanged: CoreDNS rewrites, `region-dns`, `creds`, `certs`, `kargo-shards` token
  handoff (regional `kargo-shard` app retries until `kargo-hub-kubeconfig` exists).

## Sync-boundary change

- Infra edits (`platform/infrastructure/**`, `platform/appsets/**`) now reach clusters only after **push
  to `main`**; `infra` only bootstraps and waits. Unpushed infra changes aren't testable via `infra`.
- `platform/bootstrap/`, `platform/cluster/` still task-lane.

## Testing

- `mise run test`: golden for new rendered layout (deploys JSON, projects, regions, keycloak clients,
  kargo values); pin-equality check; regions-file vs `clusters.cue` check.
- Proof: clean `mise run up`, then all Applications Synced/Healthy on hub + each region, and one
  end-to-end promotion to a target in each region.

## Docs

- ADR 0017: AppSets + cluster gen, per-region topology kept, git-sourced add-ons, clean-up migration.
  Supersedes the bootstrap/inlining parts of ADR 0015 wiring.
- Update `docs/sync-boundaries.md`, `CLAUDE.md` (Layout, Commands, Changing infrastructure),
  `README.md`, `GLOSSARY.md` (root app, cluster labels, AppSet), `platform/render/README.md`,
  `docs/gotchas.md` for new failures found.

## Out of scope

- Hub-and-spoke Argo CD. Progressive Syncs (RollingSync). Testing unpushed infra changes.
