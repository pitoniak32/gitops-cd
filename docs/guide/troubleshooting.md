# Troubleshooting

You got a lint error you don't understand, or your change merged but isn't running.

## Lint errors

Lint prints `file:line:col: field.path: message`. Below are the messages app teams hit most, each with a
config that triggers it. Each example below is checked to produce its message.

### `field not allowed`

A key that doesn't exist at that level, usually a typo or a field set at the wrong layer.

```yaml path=capabilities/payments/apps/web/app.yml expect="replica: field not allowed"
image: ghcr.io/example/web
replica: 3
sourceRepo: pitoniak32/gitops-cd
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}
```

Fix: `replicas`. Valid fields per level: `gcd schema app` or [reference/app.md](../reference/app.md).
(`image`, `module`, `port`, `sourceRepo` and the constraints are app-level only.)

### `image: invalid value … (must match …)`

`image` is a repository only. No tag, no digest.

```yaml path=capabilities/payments/apps/web/app.yml expect="image: invalid value"
image: ghcr.io/example/web:1.2.3
sourceRepo: pitoniak32/gitops-cd
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}
```

Fix: `image: ghcr.io/example/web`. You pick the tag when you assemble Freight. Use `imageConstraint`
(e.g. `^1`) to limit which tags count ([configure apps](apps.md#image-or-module)).

### `set exactly one of image (K8s app) or module (terraform app)`

Every app ships one artifact. Add `image` (or `module` for Terraform), not both.

```yaml path=capabilities/payments/apps/web/app.yml expect="set exactly one of image"
sourceRepo: pitoniak32/gitops-cd
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}
```

### `unknown target; allowed: …` / `unknown stage; allowed: …`

The app names a target or stage the capability doesn't have. The message lists the valid names.

```yaml path=capabilities/payments/apps/web/app.yml expect="unknown target"
image: ghcr.io/example/web
sourceRepo: pitoniak32/gitops-cd
stages:
  dev:
    targets: {us-east4-dev-k9s: {}}
```

```yaml path=capabilities/payments/apps/web/app.yml expect="stages.qa: unknown stage"
image: ghcr.io/example/web
sourceRepo: pitoniak32/gitops-cd
stages:
  qa:
    targets: {us-east4-dev-k8s: {}}
```

Fix the typo, or add the stage (`stages/qa.yml`) or target (`targets/<name>/target.yml`) in the same PR
([targets and stages](targets-stages.md)).

### `prd requires replicas >= 1`

Each environment type has a minimum number of replicas on K8S targets: `npr` and `prd` need at least 1.

```yaml path=capabilities/payments/apps/web/app.yml expect="prd requires replicas >= 1"
image: ghcr.io/example/web
sourceRepo: pitoniak32/gitops-cd
stages:
  prod:
    replicas: 0
    targets: {us-east4-prd-k8s: {}}
```

### `expected a plain string or secret:<gcp-project>/<secret>[@<version>]`

An `env` value starting with `secret:` must name a GCP project and a secret.

```yaml path=capabilities/payments/apps/web/app.yml expect="expected a plain string or secret:"
image: ghcr.io/example/web
env:
  DB_PASS: secret:db_pass
sourceRepo: pitoniak32/gitops-cd
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}
```

Fix: `secret:acme-payments-npr/db_pass` (optionally `@3` or `@latest`).

### `target … has no environment-variables/<name>.yml`

`envFrom: [<name>]` needs that file in every target the setting reaches. Setting it at the app level
reaches every target.

```yaml path=capabilities/payments/apps/web/app.yml expect="has no environment-variables/datadog.yml"
image: ghcr.io/example/web
envFrom: [datadog]
sourceRepo: pitoniak32/gitops-cd
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}
```

Fix: add `targets/us-east4-dev-k8s/environment-variables/datadog.yml`, or move `envFrom` down to the
stages and targets that have it ([configure apps](apps.md#environment-variables)).

### `app … uses stage …: needs promoters.<stage>`

Every stage an app uses needs someone allowed to promote it. This one is reported in `owners.yml`.

```yaml path=capabilities/payments/owners.yml expect="needs promoters.dev"
codeOwners: [team1]
promoters:
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
```

Fix: add `promoters.dev` (or `apps.<app>.promoters.dev` for one app).

### `unknown team` / `expected a lowercase @example.com email`

`codeOwners` takes team file names from `teams/`, not GitHub team slugs. People are lowercase
`@example.com` emails, because access matches the login's `email` claim exactly.

```yaml path=capabilities/payments/owners.yml expect="codeOwners.0: unknown team"
codeOwners: [payments-team]
promoters:
  dev: [doug@example.com]
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
```

```yaml path=capabilities/payments/owners.yml expect="expected a lowercase @example.com email"
codeOwners: [team1]
promoters:
  dev: [Doug@example.com]
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
```

### Target names and `autoPromote` on prod

`name must be <region>-<env>-<platform>[-<qualifier>]` and `deploys to prod targets; autoPromote not
allowed` are explained, with examples, in [targets and stages](targets-stages.md).

## Merged, but not running

Work down the chain ([concepts](concepts.md#how-a-change-reaches-a-cluster)):

1. **Rendered?** The `config.yaml` run on `main` must be green. It writes the branch `rendered` (commit
   `render: <sha>`). If it failed, nothing downstream changes.
2. **Freight?** Nothing deploys on merge. In Kargo, open the project `kargo-<cap>-<app>`, then the
   Warehouse, and assemble Freight from your config commit plus an image tag. If your tag isn't offered,
   check it's SemVer and matches `imageConstraint`.
3. **Promoted?** Each target is its own Kargo Stage. Promote the one you expect, or check
   `autoPromote` on its stage. Can't promote? Your email must be in that stage's `promoters`
   ([promote and access](promote-access.md)).
4. **Blocked by verification?** "Manual approval required" means the Freight skipped an upstream stage.
   Promote it there first, or ask a `breakGlass` person.
5. **Pre-deploy check failed?** On `prd` targets, the promotion fails if the `precheck.yaml` run in your
   `sourceRepo` fails, or the file or its `workflow_dispatch` inputs are missing.
6. **Argo CD?** The application `<cap>-<app>-<target>` syncs the branch `env/<cap>/<app>/<target>`. Sync
   `Unknown` with "revision not found" before the target's first promotion is expected.
7. **GitHub Actions target?** The Kargo Stage dispatches the workflow and waits. A 404 means the workflow
   file isn't at `ref`. A 422 means no `workflow_dispatch` or mismatched inputs. A successful gha Stage
   always shows Healthy. Check the workflow run itself.

Platform-side causes (credentials, cluster, Kargo state) are in [gotchas](../gotchas.md); ask the
platform team.
