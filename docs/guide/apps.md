# Configure apps

You want to change what an app ships or how it runs: its image or module, replicas, environment variables,
secrets or resources, for every target or just some.

Every `app.yml` field: [reference/app.md](../reference/app.md).

## Image or module

An app ships exactly one artifact:

- `image`: a container image repository (no tag or digest). For K8S and CLOUD_RUN targets.
- `module`: a Terraform module pushed as an OCI artifact. For TERRAFORM targets only, deployed through
  GitHub Actions ([ADR 0021](../adr/0021-terraform-apps.md)).

Kargo watches the repository for SemVer tags. `imageConstraint` / `moduleConstraint` limit which tags
count (`^1` = 1.x only). When promoting, a promoter picks a tag; Freight pins its digest
([ADR 0016](../adr/0016-image-discovery-freight-assembly.md)).

<!-- embed path=capabilities/payments/apps/infra/app.yml -->
```yaml
module: ghcr.io/pitoniak32/tf-bucket
sourceRepo: pitoniak32/gitops-cd
moduleConstraint: ^1
env:
  TF_VAR_region: us-central1
workflow:
  ref: v1
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

Source: [`payments/apps/infra/app.yml`](../../platform/render/testdata/root/capabilities/payments/apps/infra/app.yml)
<!-- /embed -->

`image`, `module`, their constraints, `port` and `sourceRepo` are app-level only. The artifact promotes as
one unit across every target.

## Layers

`replicas`, `env`, `envFrom`, `resources`, `deployWith` and `workflow` can be set at three levels. The most
specific wins ([ADR 0004](../adr/0004-layered-override-merge.md)):

1. `stages.<stage>.targets.<target>` (one target of one stage)
2. `stages.<stage>` (every target of that stage)
3. top level of `app.yml` (everywhere)
4. platform defaults (per target platform), then global defaults

`env` merges per key. `envFrom` lists are combined across layers, so a narrower layer adds files and never
removes them. `workflow` and `resources` resolve per field. `replicas` and `deployWith` take the most
specific layer that sets them.

<!-- embed path=capabilities/payments/apps/my-app/app.yml lines="(1-8;text='app level'),(9-14;text='stage level: dev'),15,(17-23;text='target level: dc1-dev-k8s'),(24-34;text='staging: dc1-npr-k8s deploys with GitHub Actions')" -->
```yaml
# app level
image: ghcr.io/pitoniak32/nginx
sourceRepo: acme/my-app
imageConstraint: ^1
port: 80
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
  # staging: dc1-npr-k8s deploys with GitHub Actions
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
```

Source: [`payments/apps/my-app/app.yml:1-15,17-34`](../../platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml#L1-L34)
<!-- /embed -->

`gcd show -s` shows which layer each value came from. For `dc1-dev-k8s` in `dev`, the target level wins
for `replicas`, `resources` and `APP_ENV`. `STAGE_ENV` comes from the stage, and `APP_SECRET` from the app:

```sh
gcd show payments/my-app --stage dev --target dc1-dev-k8s -s
```

```text
      replicas: 3                                                # apps/my-app/app.yml:20
      resources:
        cpu: 250m                                                # apps/my-app/app.yml:22
        memory: 256Mi                                            # apps/my-app/app.yml:23
      deployWith: argocd                                         # global default
      envFrom:
        - launchdarkly                                           # apps/my-app/app.yml:11
      env:
        APP_SECRET: secret:acme-proj1/my-app-secret              # apps/my-app/app.yml:8
        STAGE_ENV: "false"                                       # apps/my-app/app.yml:13
        APP_ENV: "false"                                         # apps/my-app/app.yml:19
```

If you edit a wider layer and nothing changes, a narrower layer overrides it. `-s` shows you where.

## Environment variables

- `env`: `KEY: value` at any layer, merged per key.
- `secret:<gcp-project>/<secret>[@<version>]` as a value references a Secret Manager secret instead of a
  plain string. Lint checks the format. (The local kind clusters have no secret store, so these are
  validated but not rendered.)
- `envFrom: [<name>]`: pull in a target's shared file `targets/<target>/environment-variables/<name>.yml`
  ([ADR 0014](../adr/0014-target-environment-variables.md)). Every target the setting reaches must have
  that file. Keys in `env` win over keys from `envFrom`.

<!-- embed path=capabilities/payments/targets/dc1-dev-k8s/environment-variables/launchdarkly.yml -->
```yaml
LD_SDK_KEY: 'secret:acme-dev-onprem/launchdarkly-sdk-key'
LD_RELAY_URL: https://ld-relay.onprem-dev.example.com
APP_ENV: 'from-bundle'
```

Source: [`payments/targets/dc1-dev-k8s/environment-variables/launchdarkly.yml`](../../platform/render/testdata/root/capabilities/payments/targets/dc1-dev-k8s/environment-variables/launchdarkly.yml)
<!-- /embed -->

Fields: [reference/env-vars.md](../reference/env-vars.md).

## Replicas and resources

- `replicas`: 0 to 100. Global default 2. Each environment type sets a minimum for K8S targets: `npr`
  and `prd` need at least 1.
- `resources.cpu` / `resources.memory`: requests, e.g. `250m` / `256Mi`. Global defaults `100m` / `128Mi`.
  `cpu` and `memory` resolve separately: each takes the most specific layer that sets it.

## Deploying with GitHub Actions

`deployWith: gha` plus `workflow` (`repo`, `file`, `ref`) makes Kargo dispatch a workflow instead of syncing
Argo CD. It's a layer field, so it can apply to just one target, as `dc1-npr-k8s` does above. Details:
[targets and stages](targets-stages.md#deploy-with-github-actions).
