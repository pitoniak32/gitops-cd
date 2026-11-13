# Targets and stages

You want to add a place to deploy (a target) or a step in your pipeline (a stage), or deploy through
GitHub Actions instead of Argo CD.

Fields: [reference/target.md](../reference/target.md), [reference/stage.md](../reference/stage.md).

## Stages: the pipeline

One file per stage, `stages/<stage>.yml`; the file name is the stage name. `dependsOn` lists the stages
whose verified Freight this stage takes. Together the files form a graph, and Kargo renders it as one
pipeline per app ([ADR 0012](../adr/0012-kargo-stage-per-target.md)).

The sandbox `payments` pipeline is `dev → staging, perf → prod`:

<!-- embed path=capabilities/payments/stages/staging.yml -->
```yaml
dependsOn: [dev]
```

Source: [`payments/stages/staging.yml`](../../platform/render/testdata/root/capabilities/payments/stages/staging.yml)
<!-- /embed -->

<!-- embed path=capabilities/payments/stages/perf.yml -->
```yaml
dependsOn: [dev]
color: teal  # Kargo UI color; default is picked per stage
```

Source: [`payments/stages/perf.yml`](../../platform/render/testdata/root/capabilities/payments/stages/perf.yml)
<!-- /embed -->

<!-- embed path=capabilities/payments/stages/prod.yml -->
```yaml
dependsOn: [staging, perf]
```

Source: [`payments/stages/prod.yml`](../../platform/render/testdata/root/capabilities/payments/stages/prod.yml)
<!-- /embed -->

- `autoPromote: true` promotes automatically once upstream stages verify; without it a promoter promotes
  by hand. One app can override its stage's setting with `stages.<stage>.autoPromote` in its `app.yml`.
  Prod is always promoted by a person, so a stage that deploys to any `prd` target can't auto-promote
  (in either file):

  ```yaml path=capabilities/payments/stages/prod.yml expect="autoPromote not allowed"
  dependsOn: [staging, perf]
  autoPromote: true
  ```

- `color` is cosmetic (the Kargo UI color).
- An app uses only the stages it lists under `stages:` in its `app.yml`. Stages it skips are bypassed in
  its pipeline.

Each target an app deploys to in a stage becomes its own Kargo Stage, named after the target, so you
promote target by target. Where several targets fan in or out, the platform adds control-flow Stages
(`<stage>-done`, `<stage>`) that need no action of their own.

## Targets: where things run

One directory per target, `targets/<target>/target.yml`, shared by every app in the capability. A target
does nothing until an app lists it under `stages.<stage>.targets`.

### Naming

`<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]`, checked against the target's own fields
([ADR 0025](../adr/0025-target-naming-convention.md)). Use the qualifier to tell apart two targets that
would otherwise share a name (`us-east4-npr-k8s-perf`).

```yaml path=capabilities/payments/targets/us-east4-dev-k8s/target.yml expect="name must be <region>-<env>-<platform>"
config:
  cluster: us-east4
  namespace: payments-us-dev
  environment: dev
  environmentType: npr
  auth:
    wifEmail: team1-dev@team1-dev.iam.gserviceaccount.com
  billing: dev
  platform: K8S
```

Here `environmentType: npr` means lint wants the directory to be named `us-east4-npr-k8s[-<qualifier>]`.

### Environment and environment type

- `environmentType` (required): `dev`, `npr` or `prd`. Platform policy keys on it: minimum replicas
  (`npr`/`prd` ≥ 1 on K8S), no auto-promotion into `prd`, and pre-deploy checks.
- `environment` (required): a free-form name for where it runs (`dev`, `staging`, `perf`, `prod`). It's a
  label, not a policy.

### Platforms

`platform` decides what the target needs and what apps can deploy there.

**K8S** (default): a cluster from the platform registry plus a namespace. Image apps. Deploys with Argo CD
unless `deployWith: gha`.

<!-- embed path=capabilities/payments/targets/us-east4-prd-k8s/target.yml -->
```yaml
config:
  cluster: us-east4
  namespace: payments-us-prod
  environment: prod
  environmentType: prd
  auth:
    wifEmail: team1-prod@team1-prod.iam.gserviceaccount.com
  billing: prod
  platform: K8S
```

Source: [`payments/targets/us-east4-prd-k8s/target.yml`](../../platform/render/testdata/root/capabilities/payments/targets/us-east4-prd-k8s/target.yml)
<!-- /embed -->

**CLOUD_RUN**: a GCP project and region. Image apps render a Knative service, always deployed by the
platform's `cloud-run.yaml` workflow ([ADR 0024](../adr/0024-cloud-run-targets.md)). Defaults suited to
Cloud Run: scale to zero, port 8080, 1 CPU / 512Mi.

<!-- embed path=capabilities/payments/targets/us-east4-npr-cr/target.yml -->
```yaml
# CLOUD_RUN target: image apps render a Knative service; deploys with gha only
config:
  platform: CLOUD_RUN
  environment: staging
  environmentType: npr
  auth: {wifEmail: deployer@acme-payments-npr.iam.gserviceaccount.com}
  billing: payments
  gcpProject: acme-payments-npr
  region: us-east4
```

Source: [`payments/targets/us-east4-npr-cr/target.yml`](../../platform/render/testdata/root/capabilities/payments/targets/us-east4-npr-cr/target.yml)
<!-- /embed -->

**TERRAFORM**: a GCP project, region and GCS state `backend`. Only terraform apps (`module:`), always
deployed by the `terraform.yaml` workflow, plan then apply ([ADR 0021](../adr/0021-terraform-apps.md)).

<!-- embed path=capabilities/payments/targets/us-east4-dev-tf/target.yml -->
```yaml
# TERRAFORM target: gcpProject/region/backend instead of a cluster; deploys with gha only
config:
  platform: TERRAFORM
  region: us-east4
  environment: dev
  environmentType: dev
  auth:
    wifEmail: team1-dev@team1-dev.iam.gserviceaccount.com
  billing: dev
  gcpProject: team1-dev
  backend:
    bucket: tfstate-team1-dev
```

Source: [`payments/targets/us-east4-dev-tf/target.yml`](../../platform/render/testdata/root/capabilities/payments/targets/us-east4-dev-tf/target.yml)
<!-- /embed -->

Lint rejects fields that don't belong to the target's platform, and apps whose artifact doesn't match it
(an `image` app on a TERRAFORM target).

## Deploy with GitHub Actions

By default a K8S deploy syncs the target's Argo CD application. `deployWith: gha` makes the Kargo Stage
dispatch a GitHub Actions workflow and wait for its run instead
([ADR 0020](../adr/0020-github-actions-deploy.md)). Set it in `app.yml` at any layer, together with
`workflow`:

```yaml fragment
stages:
  staging:
    targets:
      dc1-npr-k8s:
        deployWith: gha
        workflow:
          repo: acme/deploys   # default: this repo
          file: onprem.yml     # default: the platform's (deploy.yaml for K8S)
          ref: v1              # default: main
```

`workflow` fields resolve separately, so you can set just `ref` at the app level and `file` per stage.
CLOUD_RUN and TERRAFORM targets always use GitHub Actions, whatever `deployWith` says.

## Pre-deploy checks

Before a Kargo Stage deploys to a `prd` target, the platform dispatches its check workflow
(`precheck.yaml`) in your app's `sourceRepo` and waits for it. Anything but success fails the promotion
before anything is pushed ([ADR 0028](../adr/0028-platform-pre-deploy-checks.md)). Teams can't configure
or remove these checks. Your `sourceRepo` needs the workflow file on `main`, with `workflow_dispatch`.
