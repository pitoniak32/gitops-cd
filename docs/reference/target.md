# target

A target is one place a stage can deploy to: a namespace on a platform cluster, a Cloud Run project and
region, or a Terraform project with its state backend. It lives at
`systems/<sys>/targets/<target>/target.yml` and is shared by every app in the system. A target
does nothing until an app lists it under `stages.<stage>.targets`.

How-to: [targets and stages](../guide/targets-stages.md).

## Defining a target

<!-- embed path=systems/payments/targets/us-east4-prd-k8s/target.yml -->
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

Source: [`payments/targets/us-east4-prd-k8s/target.yml`](../../platform/render/testdata/root/systems/payments/targets/us-east4-prd-k8s/target.yml)
<!-- /embed -->

Everything sits under `config` (required):

- **Where it is**: `platform`, then the platform's own fields (`cluster` and `namespace` above).
- **What it is**: `environment` and `environmentType`.
- **Who deploys and who pays**: `auth.wifEmail` and `billing`.

## Name

The directory name is the target name and must be `<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]`.
Lint checks it against the target's own fields: the region is the K8S `cluster`'s region or the
`region` field, the middle part is `environmentType`, the suffix is the platform
([ADR 0025](../adr/0025-target-naming-convention.md)). Add a qualifier to tell apart two targets that
would otherwise share a name, like `us-east4-npr-k8s-perf`.

```yaml path=systems/payments/targets/us-east4-dev-k8s/target.yml expect="name must be <region>-<env>-<platform>"
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

## Environment and environment type

Both are required.

- `environment`: a free-form name for where it runs (`dev`, `staging`, `perf`, `prod`): lowercase,
  starting with a letter, up to 40 characters. It's a label used in namespaces and labels, not policy.
- `environmentType`: `dev`, `npr` or `prd`. Platform policy keys on it:

| `environmentType` | K8S replicas floor | Pre-deploy checks | `autoPromote` into it |
|---|---|---|---|
| `dev` | 0 | none | allowed |
| `npr` | 1 | none | allowed |
| `prd` | 1 | `precheck.yaml` in the app's `sourceRepo` | not allowed |

A K8S `cluster` must also accept the environment type.

## Platforms

`platform` decides which other fields the target needs and which apps can deploy there. Fields of
another platform are rejected.

| Field | K8S (default) | CLOUD_RUN | TERRAFORM |
|---|---|---|---|
| `cluster` | `us-east4` or `dc1` | | |
| `namespace` | Kubernetes namespace | | |
| `gcpProject` | | GCP project id | GCP project id |
| `region` | | `us-central1`, `us-east1`, `us-east4` | same |
| `backend.bucket` | | | GCS bucket for state (required in `backend`) |
| `backend.prefix` | | | GCS object prefix |
| App artifact | `image` | `image` | `module` |
| Deploys with | Argo CD, or `gha` | GitHub Actions (`cloud-run.yaml`) | GitHub Actions (`terraform.yaml`) |

**CLOUD_RUN** renders a Knative service; apps default to scale to zero, port 8080, 1 CPU / `512Mi`
([ADR 0024](../adr/0024-cloud-run-targets.md)).

<!-- embed path=systems/payments/targets/us-east4-npr-cr/target.yml lines=2-9 -->
```yaml
config:
  platform: CLOUD_RUN
  environment: staging
  environmentType: npr
  auth: {wifEmail: deployer@acme-payments-npr.iam.gserviceaccount.com}
  billing: payments
  gcpProject: acme-payments-npr
  region: us-east4
```

Source: [`payments/targets/us-east4-npr-cr/target.yml:2-9`](../../platform/render/testdata/root/systems/payments/targets/us-east4-npr-cr/target.yml#L2-L9)
<!-- /embed -->

**TERRAFORM** plans then applies the app's module with state in the GCS `backend`
([ADR 0021](../adr/0021-terraform-apps.md)).

<!-- embed path=systems/payments/targets/us-east4-prd-tf/target.yml -->
```yaml
config:
  platform: TERRAFORM
  region: us-east4
  environment: prod
  environmentType: prd
  auth:
    wifEmail: team1-prod@team1-prod.iam.gserviceaccount.com
  billing: prod
  gcpProject: team1-prod
  backend:
    bucket: tfstate-team1-prod
    prefix: payments/custom
```

Source: [`payments/targets/us-east4-prd-tf/target.yml`](../../platform/render/testdata/root/systems/payments/targets/us-east4-prd-tf/target.yml)
<!-- /embed -->

## Auth and billing

- `auth.wifEmail` (required): the service account email of the workload identity that deploys,
  `<name>@<project>.iam.gserviceaccount.com`.
- `billing` (required): a cost label, lowercase letters, digits and `-`.

## Environment variables files

A target can hold shared variable files in `environment-variables/<name>.yml`, which apps opt into with
`envFrom`: see [env-vars](env-vars.md).

Every field: `gcd schema target`.
