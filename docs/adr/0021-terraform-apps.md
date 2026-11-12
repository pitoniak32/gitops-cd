# Terraform apps

Some teams ship infrastructure with Terraform, not Kubernetes workloads. They get the same capability pipeline (stages, promoters, break-glass, one Kargo Stage per target), with a versioned module artifact applied by GitHub Actions (ADR 0020). Argo CD has no part in it.

- **App**: `module: <OCI repo>` (and optional `moduleConstraint`) instead of `image`, exactly one of the two. Settings: `env`, `envFrom` and `workflow` (default file `terraform.yaml`), layered as usual. `replicas`, `resources`, `port`, `deployWith` and `imageConstraint` are lint errors. Env keys are C identifiers, so `TF_VAR_<name>` sets variables.
- **Target**: `platform: TERRAFORM` with flat `gcpProject`, `region` (ADR 0025) and `backend: {bucket, prefix?}` (GCS; the prefix defaults to `<cap>/<app>/<target>`), plus `environment`, `environmentType`, `auth.wifEmail` and `billing`. No `cluster`/`namespace`. A module app may only use TERRAFORM targets.
- **Render**: `rendered/apps/<cap>/<app>/<stage>/<target>/terraform.yaml` (capability, app, stage, target, environment, gcpProject, wifEmail, backend, module, env). No manifests, no Argo CD params.
- **Artifact**: the module is pushed as an OCI artifact (`oras push`, single manifest, empty config; `mise run tf-module-push`). The Warehouse discovers it with an ordinary `image` subscription (SemVer, no `platform`), and Freight pins the digest. The pinned value goes into `module` in `terraform.yaml`.
- **Controller**: TERRAFORM Stages have no `spec.shard`. The hub's default controller runs them, because nothing regional is involved.
- **Apply**: the Kargo promotion is the approval, so the workflow runs plan + apply in one job. The example `.github/workflows/terraform.yaml` only runs `init -backend=false` + `validate`, with the real WIF/plan/apply steps commented.

## Considered Options

- **Module in git, a Release asset, or no artifact**: git tags lose the digest pin, Kargo can't discover Release assets, and a module in the capability folder couples module and config versions.
- **Helm `chart` subscription for the module**: Freight records only the chart version, not a digest.
- **A separate file kind or `tfapps/` dir**: duplicates routing, graph and RBAC.
- **`vars` rendered to `terraform.tfvars.json`**: `TF_VAR_*` covers variables and provider env alike.
- **Plan, then a GitHub Environment reviewer, then apply**: a second gate outside Kargo.

## Consequences

- `kargo-restore` re-dispatches terraform targets, so plan + apply run again (a no-op when nothing drifted).
- A target can't switch platform. Create a new one instead.
- Module discovery breaks for the whole repository if any tag is an OCI index or has a non-JSON config (`docs/gotchas.md`).
- Module packages must be public, because Kargo has no registry credential.
