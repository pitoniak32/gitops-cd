# Cloud Run targets

Some teams want to run a service on Cloud Run instead of a cluster, sometimes only for some stages (dev on kind, prod on Cloud Run). Cloud Run accepts Knative Serving YAML (`gcloud run services replace`), so the renderer emits a Knative `Service` from the same app settings.

**Target**: `platform: CLOUD_RUN` with flat fields, no platform-named section. Required and forbidden fields are per platform (the `platforms` table in `config.go`):

| field | K8S | TERRAFORM | CLOUD_RUN |
|---|---|---|---|
| `cluster`, `namespace` | required | forbidden | forbidden |
| `gcpProject`, `region` | forbidden | required | required |
| `backend` | forbidden | required | forbidden |

**App**: the same `image` app as for K8S. One app may mix K8S and CLOUD_RUN targets: one Warehouse, one digest through the pipeline. Platform defaults differ:

| setting | K8S | CLOUD_RUN |
|---|---|---|
| `replicas` | Deployment replicas, default 2, floor per `environmentType` | `autoscaling.knative.dev/minScale`, default 0 |
| `port` | optional (Service on :80) | `containerPort`, default 8080 |
| `resources` | requests/limits, default 100m / 128Mi | `limits`, default `1` / `512Mi`; below 1 vCPU sets `containerConcurrency: 1` |
| `env` `secret:<p>/<s>[@<v>]` | ExternalSecret if the cluster has a store, else omitted | `secretKeyRef` if `<p>` is the target's project, else an alias declared in `run.googleapis.com/secrets` |
| `deployWith` | argocd or gha | always gha |
| `workflow.file` default | `deploy.yaml` | `cloud-run.yaml` |

**Render**: `service.yaml`, one `serving.knative.dev/v1` `Service`. `name` is the app and `namespace` is `gcpProject`. Labels use GCP-safe keys (`app`, `capability`, `stage`, `target`, `environment`, `environment-type`, `billing`, …), plus `cloud.googleapis.com/location: <region>`. There are no Argo CD params and no `spec.shard`.

**Promotion**: `promote-gha` (ADR 0020, 0027) pins the image in `service.yaml`. The example `.github/workflows/cloud-run.yaml` prints the service and applies nothing, with the WIF and `gcloud run services replace` steps commented.

The runtime service account isn't set, so Cloud Run uses the project's default compute SA. It will be an app-layer field when needed.

## Considered Options

- **Render only, with no Kargo deploy step**: no Stage would run anything.
- **Kargo calls the Cloud Run Admin API**: Kargo would need a GCP credential.
- **CLOUD_RUN-only apps, as for terraform**: a service couldn't move from kind to Cloud Run one stage at a time.
- **A `cloudRun:` section on the target**: flat fields with per-platform required/forbidden rules already work.
- **`auth.wifEmail` as the runtime SA**: that's the deployer identity, and runtime settings belong to the app.
- **`minScale` default 2**: two warm instances per service that nobody asked for.

## Consequences

- A target can't move between K8S and CLOUD_RUN. Create a new target.
- The `service.yaml` shape follows Cloud Run's documented rules, but hasn't been checked against a live `gcloud run services replace`. Secret refs are only validated, never resolved.
- Like any `gha` deploy, a CLOUD_RUN Stage is Healthy after any successful promotion.
