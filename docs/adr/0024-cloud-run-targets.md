# Cloud Run targets

Status: accepted. Extends ADR 0020 (gha deploy) and ADR 0021 (non-K8s platforms); changes ADR 0021's TERRAFORM target fields. Builds on ADR 0025 (target names, `environmentType` `dev|npr|prd`, `#GcpRegion`).

Some teams want to run a service on Cloud Run instead of a kind/GKE cluster, sometimes only for some stages (dev on kind, prod on Cloud Run). Cloud Run accepts Knative Serving YAML (`gcloud run services replace`), so the renderer can emit a Knative `Service` from the same app settings.

## Decision

**Target**: `config.platform: CLOUD_RUN`. The fields are flat, with no platform-named section:

```yaml
config:
  platform: CLOUD_RUN
  environment: dev
  environmentType: dev
  auth: {wifEmail: deployer@acme-payments-dev.iam.gserviceaccount.com}  # identity gha deploys with (WIF)
  billing: payments
  gcpProject: acme-payments-dev
  region: us-east4          # #GcpRegion (ADR 0025); target dir e.g. us-east4-dev-cr
```

| field | K8S | TERRAFORM | CLOUD_RUN |
|---|---|---|---|
| `cluster`, `namespace` | required | forbidden | forbidden |
| `gcpProject` | forbidden | required | required |
| `region` (`#GcpRegion`) | forbidden | required | required |
| `backend: {bucket, prefix?}` | forbidden | required | forbidden |

TERRAFORM's `terraform: {gcpProject, backend}` moves up to `config.gcpProject` + `config.backend`, and TERRAFORM gains a required `region`, which is added to `terraform.yaml`. A leftover `terraform:` is a lint error: `moved: use config.gcpProject / config.backend`.

**App**: the same `image` app as K8S. One app may mix K8S and CLOUD_RUN targets (one Warehouse, one digest through the pipeline). `module` apps can't use CLOUD_RUN targets. Runtime settings are `#Layer` fields (app → stage → target). Defaults depend on the platform:

| setting | K8S | CLOUD_RUN |
|---|---|---|
| `replicas` | Deployment replicas, default 2 | `autoscaling.knative.dev/minScale`, default 0 |
| `port` | optional (Service on :80) | `containerPort`, default 8080 |
| `resources` | requests/limits | `limits` |
| `env` plain | `value` | `value` |
| `env` `secret:<p>/<s>[@<v>]` | ExternalSecret if the cluster has a store, else omitted | `valueFrom.secretKeyRef: {name: projects/<p>/secrets/<s>, key: <v> or latest}` |
| `deployWith` | argocd or gha | ignored (always gha) |
| `workflow.file` default | `deploy.yaml` | `cloud-run.yaml` |

The minReplicas `policy` applies to K8S targets only.

The runtime service account isn't set: Cloud Run uses the project's default compute SA. It will be an app-layer field when needed.

**Render**: `rendered/apps/<cap>/<app>/<stage>/<target>/service.yaml`, one `serving.knative.dev/v1` `Service`:

- `metadata`: `name` is the app, `namespace` is `gcpProject`, labels as for K8S (capability, stage, target, environment, billing), annotation `run.googleapis.com/location: <region>`.
- `spec.template`: a `minScale` annotation and one container (image, port, env, limits).

There are no Argo CD params, no AppProject destination, and no `spec.shard`: the hub's default controller runs the Stage, as for TERRAFORM.

**Promotion**: the ADR 0020 gha steps. `yaml-update` writes `<repo>:<tag>@<digest>` into the container image of `service.yaml`. Then dispatch with inputs `capability, app, stage, target, commit, image`, and wait (30m).

**Workflow**: `.github/workflows/cloud-run.yaml` checks out the commit, checks that the image matches, and prints `service.yaml` to the job summary. It doesn't apply anything: there's no gcloud and no GCP auth yet. The WIF auth and `gcloud run services replace` steps are commented, like `terraform.yaml`.

## Considered Options

- **Render only, with no Kargo deploy step**: the target branch would record it, but no Stage would run anything. gha is already the pattern for things outside the cluster.
- **Kargo calls the Cloud Run Admin API** (`http` steps): Kargo would need a GCP credential in the cluster.
- **CLOUD_RUN-only apps, as for terraform**: a service couldn't move from kind to Cloud Run one stage at a time.
- **`cloudRun:` section on the target**: platform-named sections aren't needed: flat fields with per-platform required/forbidden already work for `cluster`/`namespace`.
- **`auth.wifEmail` as the runtime SA**: it's the deployer identity; runtime settings belong to the app.
- **`minScale` default 2**: two warm instances per service that nobody asked for.

## Consequences

- Breaking change for TERRAFORM targets (`terraform:` → flat fields). Both fixtures and `capabilities/payments/targets/us-{dev,stg}-tf` migrate in the same change.
- Like ADR 0020, a CLOUD_RUN Stage is Healthy after any successful promotion. The demo workflow deploys nothing, so there's nothing to verify.
- Moving a target between K8S and CLOUD_RUN isn't supported. Create a new target. Moving a stage of an app from a K8S target to a CLOUD_RUN target leaves the Argo CD Application to be pruned by the `workloads` AppSet (`preserveResourcesOnDeletion`: the workload stays).
- Secret refs are only validated, never resolved, so a wrong secret only fails at a real deploy.
- Cloud Run service names are unique per project+region. App names are already unique across capabilities, so there's no extra check.
