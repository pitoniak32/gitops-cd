# Why gitops-cd

gitops-cd is one config model and one promotion flow for every way a team ships. Teams describe apps,
stages and targets in YAML; the platform validates it, renders it, and promotes it through a pipeline in
[Kargo](https://kargo.io). How a target is deployed is a per-target choice, not a property of the
platform: Argo CD sync for Kubernetes, GitHub Actions workflows for Terraform modules, Cloud Run
services or a team's own Kubernetes deploy, and a new platform when something else is needed. The
YAML, the review, the promotion and the guardrails are the same whichever deploy method a target uses.

## Compared

| | gitops-cd | Argo CD / Flux | Kargo alone | CI pipelines | Commercial / IDP |
|---|---|---|---|---|---|
| **Governance**: mandatory checks and restrictions on every platform | ✓ platform-owned, teams can't edit or skip | ~ cluster admission policy only; no gate before deploy | ~ verification after deploy; config teams can edit | ~ per-repo rules; teams own the pipeline file | ✓ policy engines and approvals, at license cost |
| **Consistent access** from one file | ✓ one `owners.yml` per capability generates Kargo, Argo CD and CODEOWNERS access | ✗ AppProject roles and IdP groups, set per cluster | ✗ project RBAC written per project | ~ repo and environment permissions, per repo | ~ the portal's roles, plus each underlying tool's |
| **Learn with guardrails**: new members can't ship a broken config | ✓ one schema, located errors in the PR and locally | ✗ errors surface at sync | ✗ hand-written Kargo resources | ~ reusable workflows, errors at run time | ~ templates at scaffold time, drift after |
| **Consistency** across apps, targets and platforms | ✓ every app renders through the same model | ~ by convention (shared charts) | ✗ each pipeline built by hand | ✗ pipelines drift between repos | ~ per vendor model |
| **Full picture** for teams | ✓ merged config with sources, render diff per PR, pipeline view | ~ sync status per app | ✓ pipeline view | ✗ run logs, no environment state | ✓ dashboards |
| **Flexibility**: deploy method per target, open source core | ✓ Argo CD or GitHub Actions per target; custom logic only where needed | ✗ Kubernetes only | ~ any step, wired by hand | ✓ anything, nothing shared | ~ vendor integrations, lock-in |

✓ built in · ~ partly, or with effort · ✗ not provided

## Governance

The platform team sets mandatory checks and restrictions once, and every capability, app and target gets
them, whatever platform it deploys to. Teams can't configure them away: the policy lives in platform-owned
code (`platform/cli/internal/capability/config.go`), not in team YAML.

**What the platform enforces today**

- **Pre-deploy checks**: before any deploy to a `prd` target, Kargo dispatches the platform's
  `precheck.yaml` workflow in the app's source repo and waits; anything but success fails the promotion
  before anything is pushed. This runs for Argo CD, Cloud Run, Terraform and GitHub Actions targets alike
  ([ADR 0028](../adr/0028-platform-pre-deploy-checks.md)).
- **No automatic promotion into production**: a stage that deploys to a `prd` target is promoted by a
  person.

  ```yaml path=capabilities/payments/stages/prod.yml expect="autoPromote not allowed"
  dependsOn: [staging, perf]
  autoPromote: true
  ```

- **Replica floors** per environment type (`npr` and `prd` need at least 1):

  ```yaml path=capabilities/payments/apps/web/app.yml expect="prd requires replicas >= 1"
  image: ghcr.io/example/web
  sourceRepo: pitoniak32/gitops-cd
  stages:
    prod:
      replicas: 0
      targets: {us-east4-prd-k8s: {}}
  ```

- **Where things may run**: targets may only name platform clusters that accept their environment type,
  and target names follow one convention ([ADR 0025](../adr/0025-target-naming-convention.md)).

**How it can't be bypassed**

- **The PR is the only way in.** Lint runs in CI on every PR; a config that breaks a rule doesn't merge.
- **The UIs are read-only for changes.** No Kargo or Argo CD user can create, edit or delete resources;
  an admission policy in the cluster rejects anything but promotions and Freight, whatever a user's RBAC
  says (`platform/infrastructure/kargo-config/manifests/gitops-only.yaml`).
- **Access comes from one file, generated everywhere** ([below](#consistent-access-from-one-file)).
- **Skipping verification is guarded.** Approving Freight that skipped an upstream stage is limited to
  named on-call people by a guard in front of the Kargo API ([ADR 0013](../adr/0013-break-glass-approval.md)).

### Consistent access from one file

Each capability's `owners.yml` is the only place permissions are set. From it the platform generates who
may approve PRs (CODEOWNERS), promote each stage and view each app in Kargo, view apps in Argo CD, and break
glass, matched on login email, never on identity-provider groups
([ADR 0011](../adr/0011-access-by-email.md)). Nobody grants access by hand in a UI, so the tools can't
drift apart: what the file says is what every tool enforces. Changes to it need platform approval.

<!-- embed path=capabilities/payments/owners.yml lines=1-8 -->
```yaml
codeOwners: [team1]
promoters:
  dev: [doug@example.com]
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
viewers: [bob@example.com]
breakGlass: [carol@example.com]
```

Source: [`payments/owners.yml:1-8`](../../platform/render/testdata/root/capabilities/payments/owners.yml#L1-L8)
<!-- /embed -->

With Argo CD, Kargo and CI configured separately, the same permissions live in several places (AppProject
roles, Kargo project RBAC, IdP groups, repo settings), each edited on its own, with nothing keeping them
in step.

### Full picture for teams

**Teams still see everything about their apps.** `gcd show <cap>/<app> -s` prints the merged config of
every target with the file and line each value came from; every PR gets a comment with the rendered diff;
each app has its own Kargo project showing its whole pipeline
([ADR 0029](../adr/0029-kargo-project-per-app.md)).

## Learn with guardrails

A new team member can learn by changing real config: the tool tells them exactly what's wrong and where,
before review.

- `gcd schema app` prints every field with its type, pattern and description.
- `gcd lint` (locally and in the PR) reports `file:line:col: field: message`:

  ```yaml path=capabilities/payments/apps/web/app.yml expect="image: invalid value"
  image: ghcr.io/example/web:1.2.3
  sourceRepo: pitoniak32/gitops-cd
  stages:
    dev:
      targets: {us-east4-dev-k8s: {}}
  ```

- A sandbox (`gcd --root platform/render/testdata/root …`) to try anything without touching real
  capabilities.
- A [guide](../guide/) whose every example is checked against lint, so it can't teach something that no
  longer works.

## Consistency

Every app, target and platform goes through the same model: the same files, the same layered overrides
([ADR 0004](../adr/0004-layered-override-merge.md)), the same promotion graph and the same access model.
One app can even span platforms. This one runs on Kubernetes in `dev` and on Cloud Run in `staging`:

<!-- embed path=capabilities/payments/apps/web/app.yml -->
```yaml
# mixed app (ADR 0024): dev on K8S, staging on CLOUD_RUN
image: ghcr.io/example/web
sourceRepo: pitoniak32/gitops-cd
deployWith: argocd # app-level; must not affect the CLOUD_RUN target
env:
  GREETING: hello
  API_KEY: secret:acme-secrets/web-api-key
  DB_PASS: secret:acme-payments-npr/db_pass@3
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}
  staging:
    targets: {us-east4-npr-cr: {}}
```

Source: [`payments/apps/web/app.yml`](../../platform/render/testdata/root/capabilities/payments/apps/web/app.yml)
<!-- /embed -->

Teams write only YAML ([ADR 0006](../adr/0006-teams-write-only-yaml.md)); the Kubernetes manifests,
Kargo pipelines and Argo CD applications are generated, so they look the same for every team.

## Flexibility

Deploy methods are chosen per target, and every one promotes the same way:

| Target | Deploys with |
|---|---|
| K8S | Argo CD sync (default), or `deployWith: gha` for a team's own workflow ([ADR 0020](../adr/0020-github-actions-deploy.md)) |
| CLOUD_RUN | the platform's `cloud-run.yaml` workflow ([ADR 0024](../adr/0024-cloud-run-targets.md)) |
| TERRAFORM | the platform's `terraform.yaml` workflow, plan then apply ([ADR 0021](../adr/0021-terraform-apps.md)) |

A Terraform app is the same `app.yml` with a module instead of an image:

<!-- embed path=capabilities/payments/apps/infra/app.yml lines=1-3 -->
```yaml
module: ghcr.io/pitoniak32/tf-bucket
sourceRepo: pitoniak32/gitops-cd
moduleConstraint: ^1
```

Source: [`payments/apps/infra/app.yml:1-3`](../../platform/render/testdata/root/capabilities/payments/apps/infra/app.yml#L1-L3)
<!-- /embed -->

The core is open source and widely used: Kargo for promotion, Argo CD for sync, GitHub Actions for
workflows, Keycloak for login. Custom logic sits only where it adds value: the `gcd` renderer and linter
([ADR 0030](../adr/0030-renderer-in-go.md)) and shared promotion steps
([ADR 0027](../adr/0027-shared-promotion-tasks.md)). A new platform is one entry in the renderer's
platform table plus its output.

## Trade-offs

- **Teams can do only what the schema allows.** A new kind of setting is a platform change (schema, render,
  docs), not a team edit. That is the point, but it puts the platform team on the path for new needs.
- **`gcd` is code the platform team owns.** The renderer, linter and their tests need maintaining, as any
  internal tool does.
- **One repo for every capability.** Reviews, CI and history are shared; ownership is enforced through
  generated CODEOWNERS rather than separate repos ([ADR 0003](../adr/0003-monorepo.md)).
- **Rendered manifests are a second artifact.** Every merge renders to the `rendered` branch, which Kargo
  and Argo CD read ([ADR 0002](../adr/0002-rendered-manifests-with-kargo.md)).

## Next

- **Team deploy templates**: a team requires a stage structure for every app it deploys (for example
  `dev → staging → prod`) and lint enforces it.
- **Checks and restrictions on named stages or targets**: today mandatory checks are chosen per environment
  type (`prd`). Selecting them per stage or target, for one team or all, builds on the same mechanism.
