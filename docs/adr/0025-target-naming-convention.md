# Target naming convention

Status: proposed. Adds `environmentType`, changes the cluster regions (ADR 0015); ADR 0024 builds on it.

Target names were ad hoc (`us-stg`, `onprem-prod`, `tf-dev`) and said nothing reliable about where or what a target is. Target names end up in Kargo Stage names, `env/*` branches and Argo CD Applications, so a fixed shape makes them readable everywhere. This is still a demo, so renaming everything now is cheap.

## Decision

**Name**: `<region>-<env>-<platform>[-<qualifier>]`, enforced by lint against the target's own fields:

- `<region>`: the target's region. K8S: `clusters[cluster].region`. TERRAFORM and CLOUD_RUN: `config.region`.
- `<env>`: `config.environmentType`.
- `<platform>`: `k8s` | `tf` | `cr` for K8S | TERRAFORM | CLOUD_RUN.
- `<qualifier>` (optional, `[a-z0-9]+`): separates targets that share the first three parts (e.g. `us-east4-npr-k8s-perf`).

Lint error on the target dir: `name must be <region>-<env>-<platform>[-<qualifier>]: expected <expected prefix>`.

**Environments**: a target has two fields:

- `environment`: the environment's name (`#Name`, e.g. `dev`, `staging`, `perf`, `prod`, `qa1`). It is free-form and descriptive, and lands in the `example.com/environment` label.
- `environmentType` (new, required): `dev | npr | prd`, the class that policy keys on:
  - the target name's `<env>`
  - cluster `environmentTypes` (was `environments`)
  - `policy` (`dev` minReplicas 0, `npr` 1, `prd` 1)
  - the "no autoPromote on a stage that deploys to prod" rule, now `environmentType == "prd"`
  - a new `example.com/environment-type` label

Stage names (`dev`, `staging`, `perf`, `prod`) don't change: stages are the pipeline, environments are where a target runs.

A K8S `namespace` defaulting to `environment` is a separate change. Namespaces stay explicit and keep their current values here.

**Regions**: one platform-owned list in `clusters.cue`:

- `#GcpRegion`: `us-east1 | us-east4 | us-central1`. Required `config.region` on TERRAFORM and CLOUD_RUN targets.
- `#DatacenterRegion`: `dc1 | dc2`, generic on-prem datacenters. An org has as many as it runs, so its own names replace these.
- A cluster `region` must be one of the two lists.

The local kind clusters are renamed to what they stand in for: `us` → `us-east4` (GKE-like) and `onprem` → `dc1`. That renames everything derived from the region: kind cluster `region-<r>`, Kargo shard, regional Argo CD (`argocd-<r>.localhost`), Keycloak client `argocd-<r>`, `rendered/argocd/{deploys,projects}/<r>/` and `rendered/infrastructure/regions/<r>.json`. `mise run regions` regenerates the config.

**Renames** (all `capabilities/*` targets and test fixtures):

| capability | old | new |
|---|---|---|
| payments | us-dev | us-east4-dev-k8s |
| payments | us-stg | us-east4-npr-k8s |
| payments | us-perf | us-east4-npr-k8s-perf |
| payments | us-prod | us-east4-prd-k8s |
| payments | onprem-dev | dc1-dev-k8s |
| payments | onprem-stg | dc1-npr-k8s |
| payments | onprem-prod | dc1-prd-k8s |
| payments | us-dev-tf | us-east4-dev-tf |
| payments | us-stg-tf | us-east4-npr-tf |
| search | us-dev | us-east4-dev-k8s |
| search | us-prod | us-east4-prd-k8s |
| search | onprem-prod | dc1-prd-k8s |

Existing targets keep their `environment` and `namespace`, and gain `environmentType` (`dev` → `dev`, `staging`/`perf` → `npr`, `prod` → `prd`).

## Considered Options

- **Convention only, no lint**: names drift again the first time someone copies a target.
- **Rename only fixtures and new targets**: two naming styles in the demo; no live state worth keeping.
- **Replace `environment` with `dev|npr|prd`**: loses the descriptive name (`staging` vs `perf`), which the namespace default will need.
- **Derive `environmentType` from a fixed `environment` list**: a hidden mapping, and a platform change for every new environment name.
- **Keep kind regions `us`/`onprem`**: a target name's region wouldn't be a real region, and `onprem` can't express several datacenters.
- **Org-specific on-prem names**: every org has its own; `dc1`, `dc2` are placeholders.

## Consequences

- Clean `mise run up` required: kind clusters, shards, Argo CD instances and Keycloak clients are renamed.
- Kargo state reset: every target Stage is new. `mise run kargo-restore` can't map old Freight onto new names; promote again from the Warehouse. The old targets' `env/*` branches are deleted as part of the rollout (`docs/operations.md` gets the step).
- Old Argo CD Applications are pruned by the `workloads` AppSet with `preserveResourcesOnDeletion`, but the clusters are rebuilt anyway.
- Adding a GCP region or datacenter is a platform change to `clusters.cue`.
- The 40-character `#Name` limit now includes region and platform: `us-central1-npr-k8s-` leaves 19 characters for a qualifier.
