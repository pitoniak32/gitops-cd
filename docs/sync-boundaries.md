# Sync boundaries: Argo CD vs `mise run`

What changes reach the cluster by themselves, and what needs a command.

Rule of thumb: Argo CD reads **GitHub `main`** (`platform/appsets/`, `platform/infrastructure/`), the **`rendered` branch** (`rendered/`, written by CI on each push to main, ADR 0022) and the **`env/*` branches**. Every Argo CD (hub + each region) runs the same root app; ApplicationSets with the cluster generator pick what each one deploys from its cluster labels (ADR 0017). So a change under those paths does nothing until it is **pushed to `main`** (capabilities: merged and rendered by CI). `mise` tasks only cover what can't live in git: cluster creation, the first Argo CD install, the root app, CoreDNS, secrets and the Kargo shard token.

```
GITOPS LANE (pull: Argo CD, automated + prune + selfHeal)
  capabilities/, teams/, platform/render/
    → PR, merge → CI renders rendered/ onto branch rendered
    → hub rendered-kargo syncs rendered/kargo
    → each region: argocd-projects syncs rendered/argocd/projects/<region>/,
      workloads AppSet makes one Application per rendered/argocd/deploys/<region>/<cap>/<app>-<target>.json
      (hub runs no workloads)
    → Kargo: Freight from rendered/apps → target stage <app>-<target> (control-flow Stages where needed)
      → env/<cap>/<app>/<target>
    → workload Applications sync the env/* branches
  platform/appsets/**, platform/infrastructure/**
    → push to main → root app syncs the AppSets → add-on Applications (chart + values/manifests from main)

TASK LANE (push: mise run infra / up, from your local checkout)
  platform/cluster/{kind.yaml,region.yaml,regions} → kind hub + regional clusters (up only)
  platform/bootstrap/root.yaml             → root Application, applied to every Argo CD
  first helm install of each Argo CD       → values.yaml (+ argocd-region AppSet valuesObject)
  CoreDNS rewrites (hub + `region-dns`), fnox creds, mkcert cert, shard token → applied directly by kubectl
```

## Argo CD syncs it (push/merge to `main`, nothing to run)

| You change | Path to the cluster |
|---|---|
| `capabilities/**` (apps, stages, targets, `owners.yml`) | PR (owners change: commit `mise run codeowners` output) → merge → CI renders `rendered/` onto branch `rendered` → hub `rendered-kargo`, each region's `argocd-projects` and `workloads` AppSet pick it up. Workloads don't change until a promotion. |
| Workload manifests | Kargo Warehouse discovers new `rendered/apps/<cap>/<app>/` commits and image tags → promoter assembles Freight → target-stage promotion (via control-flow Stages where needed) pushes `env/<cap>/<app>/<target>` → that target's Application syncs. |
| A `deployWith: gha` target's workflow | Not rendered: GitHub reads `<repo>/.github/workflows/<file>` at `ref` when the Stage dispatches it. Push the workflow before promoting. Its token comes from `mise run creds` (`GITHUB_PAT`). |
| `teams/` | Takes effect on the next render, for CODEOWNERS only. It has no cluster effect (ADR 0011). |
| `platform/render/**` (renderer logic) | Same as capabilities: lands when CI renders on `main` after the merge. |
| `platform/infrastructure/<addon>/values.yaml`, `manifests/**` | Push to `main`; the add-on's Application reads them through its AppSet (`$values` ref / kustomize path). |
| `platform/appsets/**` (chart pins, region values, which cluster gets what) | Push to `main`; the root app syncs the AppSets, which regenerate their Applications. |
| Keycloak realm (`kargo-realm.json`) | Push; the configMap hash then restarts Keycloak, which re-imports the realm. |
| Argo CD's own values (`argocd/`, `argocd-region/`, the AppSets' `valuesObject`) | Push; each Argo CD manages itself through its `argocd` Application. |
| `platform/render/promotiontasks.cue` (promotion steps) | Merge; CI renders `rendered/kargo/_promotion-tasks.yaml`, hub `rendered-kargo` syncs it. Applies to every capability's next Promotion at once (ADR 0027). |
| `platform/render/clusters.cue` regions | `mise run regions` (committed config; CI checks drift), merge; CI renders `rendered/infrastructure/regions/`. A new region also needs a clean `up`. |
| Drift (manual `kubectl edit` of anything Argo CD manages) | Reverted by `selfHeal` to what's on `main`. |

## `mise run infra` needed (idempotent, keeps the cluster)

| You change | Why a task is needed |
|---|---|
| `platform/bootstrap/root.yaml` | Applied by `kubectl` to every Argo CD, not synced. |
| CoreDNS `keycloak.localhost` rewrite | Patched by the `infra` script. Argo CD doesn't manage `kube-system`. |
| Kargo git creds (`mise run creds`) / TLS cert (`mise run certs`) | They come from fnox / mkcert and never touch git. `infra` runs both, or run them alone. |
| Kargo shard token (`mise run kargo-shards`) | The hub SA token is copied into each region's `kargo-hub-kubeconfig` Secret (never in git). The hub and regional shard Applications themselves come from AppSets. `infra` runs it last. |
| A new hub add-on's position in the bootstrap order | Add it to the hub wait list `infra.HubWaitRest` in `platform/cli/internal/infra`. |

`infra` itself doesn't change anything Argo CD manages; it only installs Argo CD if missing, applies the root app and waits for the add-ons (regions bootstrap in parallel with the hub).

## `mise run up` needed (deletes and recreates the cluster)

| You change | Why |
|---|---|
| `platform/cluster/` (`kind.yaml`, `region.yaml`, `regions` list: ports, nodes, network, regions) | kind can't reconfigure a running cluster. |
| Initial Argo CD install (`helm install` in `infra`) | It runs only when no `argocd` release exists. Later changes go through the self-managed `argocd` app instead. |
| Anything about bootstrap **order** (CRDs, webhooks, OIDC issuer before their users) | `infra` on a live cluster already has everything, so ordering bugs only show up from empty. |
| Proving a cluster, infrastructure or auth change | CLAUDE.md rule: finish with a clean `up`. |

Also needed outside the cluster: **push first**. Every Argo CD and Kargo read GitHub `main` and `env/*`, so local, unpushed commits never reach the cluster, whatever you run.

## Quick check: where does my change land?

- Under `capabilities/` or `teams/`? → merge it; CI and Argo CD handle the rest.
- Under `platform/render/`? → merge it; it applies when CI renders.
- Under `platform/infrastructure/` or `platform/appsets/`? → push to `main`.
- `platform/render/clusters.cue` regions? → `mise run regions`, merge, clean `mise run up`.
- `platform/bootstrap/`? → `mise run infra`.
- Under `platform/cluster/`, or you need to prove it from scratch? → `mise run up`.
- `rendered/` → never edit it; CI owns branch `rendered`.
