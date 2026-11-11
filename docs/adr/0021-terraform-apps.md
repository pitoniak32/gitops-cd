# Terraform apps

Status: accepted. Extends ADR 0016 (Freight = config commit + one discovered artifact) and ADR 0020 (gha deploy).
Amended by ADR 0024: `terraform: {gcpProject, backend}` → `config.gcpProject`, `config.backend`.

Some teams ship infrastructure with Terraform, not Kubernetes workloads. They want the same capability pipeline (stages, promoters, break-glass, one Kargo Stage per target) with a versioned module artifact, applied by GitHub Actions. Argo CD has no part in it.

Now:

- **App**: `app.yml` sets `module: <OCI repo>` (and optionally `moduleConstraint`) instead of `image`; exactly one of the two. Settings: `env`, `envFrom`, `workflow` (default file `terraform.yaml`), layered app → stage → target as for K8s apps. `replicas`, `resources`, `port`, `deployWith`, `imageConstraint` are lint errors.
- **Target**: `config.platform: TERRAFORM` (default `K8S`) with `terraform: {gcpProject, backend: {bucket, prefix?}}` (GCS; prefix default `<cap>/<app>/<target>`), plus `environment`, `auth.wifEmail`, `billing`. No `cluster`/`namespace`. A terraform app may only use TERRAFORM targets, a K8s app only K8S targets.
- **Env keys**: any C identifier (`^[A-Za-z_][A-Za-z0-9_]*$`) for every app; terraform apps write `TF_VAR_<name>` to set variables.
- **Render**: one `rendered/apps/<cap>/<app>/<stage>/<target>/terraform.yaml` (capability, app, stage, target, environment, gcpProject, wifEmail, backend, module, env). No manifests, no Argo CD params, no AppProject destination.
- **Artifact**: the module is pushed as an OCI artifact (`oras push`, single manifest, default empty config; `mise run tf-module-push`). The Warehouse discovers it with an ordinary `image` subscription (SemVer, no `platform`); Freight pins the digest.
- **Promotion**: the ADR 0020 gha steps: copy the target folder to `env/<cap>/<app>/<target>`, `yaml-update` `module` to `<repo>:<tag>@<digest>`, commit (`..., module <tag>`), push, dispatch with inputs `capability, app, stage, target, commit, module`, wait up to **60m**. The Stage var holding the module repo stays named `image` (`gcd kargo-restore` reads it by name).
- **Controller**: terraform target Stages have no `spec.shard`; the hub's default controller runs them (they touch no cluster, ADR 0015 shards exist for Argo CD).
- **Apply**: the Kargo promotion is the approval; the workflow runs plan + apply in one job. `.github/workflows/terraform.yaml` is an example: it checks the module matches the branch, pulls it, exports `env`, and only runs `init -backend=false` + `validate` (no GCP in the demo); the real WIF auth + `init -backend-config` + `plan`/`apply` steps are commented.

## Considered Options

- **Module in git / Release asset / no artifact**: git tags lose the digest pin; Kargo can't discover Release assets; a module inside the capability folder couples module and config versions.
- **Helm `chart` subscription for the OCI module**: Freight records only the chart version, no digest.
- **Separate `terraform.yml` file kind or `tfapps/` dir**: duplicates routing, graph and RBAC for no gain.
- **`vars` rendered to `terraform.tfvars.json`**: teams wanted plain env inheritance; `TF_VAR_*` covers variables and provider env alike.
- **Plan, then a GitHub Environment reviewer, then apply**: a second gate outside Kargo, and the Kargo wait would need a per-target timeout.
- **Run on the region shard**: nothing regional to reach; the shard's only reason is its Argo CD.

## Consequences

- `mise run kargo-restore` re-promotes terraform targets, which re-dispatches the workflow: plan + apply run again (a no-op plan when nothing drifted).
- A target can't switch platform; create a new target.
- Like ADR 0020, a terraform Stage is Healthy after any successful promotion; the run conclusion is the only check.
- Module discovery breaks for the whole repository if any tag is an OCI index or carries a non-JSON config (`docs/gotchas.md`). Setting `platform` on the subscription would filter out every tag; the renderer never sets it.
- Module packages must be public (Kargo has no registry credential; the repo is public too). A private module would need an image credential in `kargo-shared-resources`.
- Verified against Kargo 1.12.5 source, not yet against a live registry; first `tf-module-push` + Freight assembly is the check.
