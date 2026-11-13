# app

An app is one deployable thing: the artifact it ships (a container image or a Terraform module) and
how it runs in each stage and target of its capability. It lives at
`capabilities/<cap>/apps/<app>/app.yml`; the directory name is the app name. The capability's
`codeOwners` approve changes to it.

How-to: [configure apps](../guide/apps.md).

## Defining an app

<!-- embed path=capabilities/payments/apps/my-app/app.yml -->
```yaml
image: ghcr.io/pitoniak32/nginx
sourceRepo: acme/my-app
imageConstraint: ^1
port: 80
replicas: 2
env:
  APP_ENV: 'true'
  APP_SECRET: 'secret:acme-proj1/my-app-secret'
stages:
  dev:
    envFrom: [launchdarkly]  # environment-variables files of the target, merged under env
    env:
      STAGE_ENV: 'false'
    replicas: 1
    targets:
      us-east4-dev-k8s: {}
      dc1-dev-k8s:  # target level wins over stage and app level
        env:
          APP_ENV: 'false'
        replicas: 3
        resources:
          cpu: 250m
          memory: 256Mi
  staging:
    targets:
      us-east4-npr-k8s: {}
      dc1-npr-k8s:
        env:
          APP_SECRET: 'secret:acme-proj2/my-app-secret@3'
        deployWith: gha
        workflow:
          repo: acme/deploys
          file: onprem.yml
          ref: v1
  perf:
    targets:
      us-east4-npr-k8s-perf: {}
  prod:
    resources:  # requests; settable at app, stage and target level
      cpu: 500m
      memory: 512Mi
    targets:
      us-east4-prd-k8s: {}
      dc1-prd-k8s: {}
```

Source: [`payments/apps/my-app/app.yml`](../../platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml)
<!-- /embed -->

An `app.yml` has two parts:

- **App-level fields**: what the app ships (`image`, `sourceRepo`, …), plus defaults for every target.
- **`stages`**: the stages the app deploys through and, under each, the targets it deploys to. Settings
  there override the app-level ones for that stage or target.

## Artifact

Every app ships exactly one artifact, set once at the app level:

- `image`: a container image repository, with no tag or digest (`ghcr.io/acme/web`, not
  `ghcr.io/acme/web:1.2.3`). For K8S and CLOUD_RUN targets.
- `module`: a Terraform module pushed as an OCI artifact, same format. For TERRAFORM targets only
  ([ADR 0021](../adr/0021-terraform-apps.md)).

Kargo watches the repository for SemVer tags. `imageConstraint` / `moduleConstraint` (a SemVer
constraint such as `^1`) limits which tags count. A promoter picks the tag when assembling Freight, and
the Freight pins its digest ([ADR 0016](../adr/0016-image-discovery-freight-assembly.md)).

<!-- embed path=capabilities/payments/apps/infra/app.yml lines=1-3 -->
```yaml
module: ghcr.io/pitoniak32/tf-bucket
sourceRepo: pitoniak32/gitops-cd
moduleConstraint: ^1
```

Source: [`payments/apps/infra/app.yml:1-3`](../../platform/render/testdata/root/capabilities/payments/apps/infra/app.yml#L1-L3)
<!-- /embed -->

The rest of the app identity:

- `sourceRepo` (required): the GitHub repository of the app's code, `<owner>/<name>`. Pre-deploy
  checks for `prd` targets run there.
- `port`: the container port, 1 to 65535. CLOUD_RUN defaults it to 8080.

These fields are app-level only: the artifact promotes as one unit across every target.

## Stages and targets

`stages` maps a stage name (a file in `stages/`) to the targets the app deploys to in that stage. A
target name is a directory in `targets/`. Stages the app doesn't list are skipped in its pipeline.

<!-- embed path=capabilities/payments/apps/infra/app.yml lines=8-17 -->
```yaml
stages:
  dev:
    targets:
      us-east4-dev-tf:
        envFrom: [bucket]
  prod:
    targets:
      us-east4-prd-tf:
        env:
          TF_VAR_region: us-east1
```

Source: [`payments/apps/infra/app.yml:8-17`](../../platform/render/testdata/root/capabilities/payments/apps/infra/app.yml#L8-L17)
<!-- /embed -->

`{}` deploys to the target with no overrides; here `us-east4-prd-tf` overrides one `env` key. Each target the app lists becomes its own Kargo Stage.

### Auto-promotion per app

Promotion is manual unless turned on. `stages/<stage>.yml` `autoPromote` sets it for every app in the
capability; `stages.<stage>.autoPromote` in `app.yml` overrides it for this app only (each app has its
own Kargo project, [ADR 0029](../adr/0029-kargo-project-per-app.md)). Here `worker` opts out of the
`dev` stage's auto-promotion:

<!-- embed path=capabilities/payments/apps/worker/app.yml lines=4-8 -->
```yaml
stages:
  dev:
    autoPromote: false  # this app only: overrides stages/dev.yml
    targets:
      us-east4-dev-k8s: {}
```

Source: [`payments/apps/worker/app.yml:4-8`](../../platform/render/testdata/root/capabilities/payments/apps/worker/app.yml#L4-L8)
<!-- /embed -->

Order: app override, then the stage file, then `false`. `gcd show <cap>/<app> -s` prints the effective
`autoPromote` of each target and where it comes from. A stage that deploys to a `prd` target can't
auto-promote, from either place.

## Layers

These fields can be set at three levels:

| Field | App | `stages.<stage>` | `stages.<stage>.targets.<target>` | Merge |
|---|---|---|---|---|
| `replicas` | ✓ | ✓ | ✓ | most specific wins |
| `env` | ✓ | ✓ | ✓ | per key |
| `envFrom` | ✓ | ✓ | ✓ | lists combined |
| `resources` | ✓ | ✓ | ✓ | `cpu`, `memory` separately |
| `deployWith` | ✓ | ✓ | ✓ | most specific wins |
| `workflow` | ✓ | ✓ | ✓ | `repo`, `file`, `ref` separately |

Below the app level come the target platform's defaults, then global defaults
([ADR 0004](../adr/0004-layered-override-merge.md)). Here the `dc1-dev-k8s` target overrides
`APP_ENV`, `replicas` and `resources` of its stage and app:

<!-- embed path=capabilities/payments/apps/my-app/app.yml lines="(5-8;text='app level'),(9-14;text='stage level: dev'),15,(17-23;text='target level: dc1-dev-k8s')" -->
```yaml
# app level
replicas: 2
env:
  APP_ENV: 'true'
  APP_SECRET: 'secret:acme-proj1/my-app-secret'
# stage level: dev
stages:
  dev:
    envFrom: [launchdarkly]  # environment-variables files of the target, merged under env
    env:
      STAGE_ENV: 'false'
    replicas: 1
    targets:
      # …
      # target level: dc1-dev-k8s
      dc1-dev-k8s:  # target level wins over stage and app level
        env:
          APP_ENV: 'false'
        replicas: 3
        resources:
          cpu: 250m
          memory: 256Mi
```

Source: [`payments/apps/my-app/app.yml:5-15,17-23`](../../platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml#L5-L23)
<!-- /embed -->

`gcd show <cap>/<app> -s` prints the merged result with the file and line each value came from.

## Environment variables

`env` is a map of `KEY: value`. A value of the form `secret:<gcp-project>/<secret>[@<version>]`
references a Secret Manager secret instead of a plain string; without `@<version>` the latest version
is used.

<!-- embed path=capabilities/payments/apps/my-app/app.yml lines=6-8 -->
```yaml
env:
  APP_ENV: 'true'
  APP_SECRET: 'secret:acme-proj1/my-app-secret'
```

Source: [`payments/apps/my-app/app.yml:6-8`](../../platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml#L6-L8)
<!-- /embed -->

`envFrom` lists names of the target's shared files, `targets/<target>/environment-variables/<name>.yml`
([env-vars](env-vars.md)). Every target the setting reaches must have that file. Keys in `env` win
over keys from `envFrom`.

## Replicas and resources

- `replicas`: 0 to 100; default 2. On K8S targets each environment type sets a floor: `npr` and `prd`
  need at least 1. CLOUD_RUN defaults to 0 (scale to zero).
- `resources.cpu`: whole cores or millicores (`1`, `250m`); default `100m`.
- `resources.memory`: `Mi` or `Gi` (`256Mi`); default `128Mi`.

CLOUD_RUN defaults `resources` to 1 CPU and `512Mi`. Resources are requests.

## Deploying with GitHub Actions

`deployWith` picks how a K8S target is deployed: `argocd` (default) syncs the target's Argo CD
application; `gha` makes the Kargo Stage dispatch a GitHub Actions workflow and wait for its run
([ADR 0020](../adr/0020-github-actions-deploy.md)). CLOUD_RUN and TERRAFORM targets always use GitHub
Actions.

`workflow` says which workflow:

| Field | Format | Default |
|---|---|---|
| `repo` | `<owner>/<name>` | this repository |
| `file` | `<name>.yml` or `.yaml` | the platform's: `deploy.yaml` (K8S), `cloud-run.yaml`, `terraform.yaml` |
| `ref` | git ref | `main` |

<!-- embed path=capabilities/payments/apps/flagged/app.yml lines=8-19 -->
```yaml
deployWith: gha
workflow:  # fields default separately: repo, file, ref
  repo: pitoniak32/deploys
  file: deploy.yml
stages:
  dev:
    workflow:  # stage level overrides app level
      repo: pitoniak32/deploys
      file: deploy-dev.yml
      ref: v2
    targets:
      us-east4-dev-k8s: {}
```

Source: [`payments/apps/flagged/app.yml:8-19`](../../platform/render/testdata/root/capabilities/payments/apps/flagged/app.yml#L8-L19)
<!-- /embed -->

Every field: `gcd schema app`.
