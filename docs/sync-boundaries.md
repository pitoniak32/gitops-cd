# Sync boundaries: Argo CD vs `mise run`

What changes reach the cluster by themselves, and what needs a command.

Rule of thumb: Argo CD only reads **`rendered/` on GitHub `main`** and the **`env/*` branches**. Everything under `platform/` is read from your **local checkout** by `mise` tasks. So a platform change does nothing until you run a task, and a capability change does nothing until it is merged and CI has rendered it.

```
GITOPS LANE (pull: Argo CD, automated + prune + selfHeal)
  capabilities/, teams/, platform/render/
    → PR, merge → CI renders rendered/ on main
    → hub rendered-kargo syncs rendered/kargo; each region's rendered-argocd syncs rendered/argocd/<region>/
      (hub runs no workloads)
    → Kargo: Freight from rendered/apps → gate <app>-<stage> → target stage <app>-<target>
      → env/<cap>/<app>/<target>
    → capability Applications sync the env/* branches

TASK LANE (push: mise run infra / up, from your local checkout)
  platform/cluster/{kind.yaml,region.yaml,regions} → kind hub + regional clusters (up only)
  platform/infrastructure/<addon>/         → inlined infra Applications → Argo CD installs + self-heals them
  platform/bootstrap/rendered.yaml         → hub root Application (rendered/kargo)
  platform/bootstrap/region.yaml           → regional root Application template, applied per region
  CoreDNS rewrites (hub + `region-dns`), fnox creds, mkcert cert → applied directly by kubectl
```

## Argo CD syncs it (merge to `main`, nothing to run)

| You change | Path to the cluster |
|---|---|
| `capabilities/**` (apps, stages, targets, `owners.yml`) | PR → merge → CI renders `rendered/` → hub `rendered-kargo` and each region's `rendered-argocd` sync it. Workloads don't change until a promotion. |
| Workload manifests | Kargo Warehouse discovers new `rendered/apps/<cap>/<app>/` commits and image tags → promoter assembles Freight → gate → target-stage promotion pushes `env/<cap>/<app>/<target>` → that target's Application syncs. |
| `teams/` | Takes effect on the next render, for CODEOWNERS only. It has no cluster effect (ADR 0011). |
| `platform/render/**` (renderer logic) | Same as capabilities: lands when CI renders on `main` after the merge. |
| Drift (manual `kubectl edit` of anything Argo CD manages) | Reverted by `selfHeal`. That includes infrastructure, but it reverts to the **last spec applied by `mise run infra`**, not to git. |

## `mise run infra` needed (idempotent, keeps the cluster)

The infrastructure Applications are not git-sourced. `mise run infra-render` inlines `values.yaml` and `manifests/` into each Application, and `infra` applies them with `kubectl`. Argo CD then installs, upgrades and self-heals them, but it never sees edits to the files.

| You change | Why a task is needed |
|---|---|
| `platform/infrastructure/<addon>/values.yaml`, `manifests/**`, `app.yaml` (chart version) | Argo CD only knows the Application spec that was last applied. |
| Keycloak realm (`kargo-realm.json`) | Same, and the configMap hash then restarts Keycloak, which re-imports the realm. |
| Argo CD's own values (`platform/infrastructure/argocd/`) | Argo CD manages itself, but only from the spec that `infra` applied. |
| `platform/bootstrap/{rendered,region}.yaml` | Applied by `kubectl`, not synced. `region.yaml` is a template: `infra` fills the region name and applies it to each `kind-region-<r>`. A new region is one line in `platform/cluster/regions`. |
| CoreDNS `keycloak.localhost` rewrite | Patched by the `infra` script. Argo CD doesn't manage `kube-system`. |
| Kargo git creds (`mise run creds`) / TLS cert (`mise run certs`) | They come from fnox / mkcert and never touch git. `infra` runs both, or run them alone. |
| Regional Argo CD (`platform/infrastructure/argocd-region/`) | Applied to each `kind-region-<r>` context by `infra`, inlined like the hub addons. Bootstrapped from the host; the hub holds no credentials to regions. |
| Kargo shards (`platform/infrastructure/kargo-shard-hub/`, `kargo-shard/`, `mise run kargo-shards`) | Templated per region from `platform/cluster/regions` (`__SHARD__`): hub identity Application, then the hub SA token is copied into each region's `kargo-hub-kubeconfig` Secret (never in git), then the regional controller Application. `infra` runs it last. Chart pin is read from `kargo/app.yaml`. |
| A new addon | Add it to the ordered list in `platform/mise-tasks/infra`. |
| Break-glass guard (`platform/infrastructure/break-glass/`: script, admin emails, approve route) | An infra addon like the others. Who is break-glass comes from `owners.yml` and arrives via the gitops lane (Stage annotations). |

## `mise run up` needed (deletes and recreates the cluster)

| You change | Why |
|---|---|
| `platform/cluster/` (`kind.yaml`, `region.yaml`, `regions` list: ports, nodes, network, regions) | kind can't reconfigure a running cluster. |
| Initial Argo CD install (`helm install` in `infra`) | It runs only when no `argocd` release exists. Later changes go through the self-managed `argocd` app instead. |
| Anything about bootstrap **order** (CRDs, webhooks, OIDC issuer before their users) | `infra` on a live cluster already has everything, so ordering bugs only show up from empty. |
| Proving a cluster, infrastructure or auth change | CLAUDE.md rule: finish with a clean `up`. |

Also needed outside the cluster: **push first**. `rendered-*` (hub and regional) and Kargo read GitHub `main` and `env/*`, so local, unpushed commits never reach the cluster, whatever you run.

## Quick check: where does my change land?

- Under `capabilities/` or `teams/`? → merge it; CI and Argo CD handle the rest.
- Under `platform/render/`? → merge it; it applies when CI renders.
- Under `platform/infrastructure/` or `platform/bootstrap/`? → `mise run infra`.
- Under `platform/cluster/`, or you need to prove it from scratch? → `mise run up`.
- `rendered/` → never edit it; CI owns it.
