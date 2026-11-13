# Terraform apps — design

Date: 2026-11-10. Extends ADR 0016 (Freight assembly) and ADR 0020 (gha deploy). Becomes ADR 0021.

## Goal

A team defines a terraform **app** and terraform **targets** in its system; promotion goes through the same stages/graph/owners/break-glass as K8s apps, and each target promotion dispatches a GitHub Actions workflow with a Freight-pinned OCI module artifact and the target's rendered config. Plan + apply in one run; the Kargo promotion is the approval. No Argo CD.

Success: a terraform app renders, lints, gets a Kargo pipeline; promoting a target pushes `env/<sys>/<app>/<target>` and dispatches the workflow with `module=<repo>:<tag>@<digest>`.

## Decisions

- **Artifact**: Terraform module pushed as OCI artifact (`oras push`, single manifest, default empty config). Kargo 1.12.5 `image` subscription (SemVer) discovers it; Freight pins digest; `imageFrom(<repo>).Digest`/`.Tag` work. Verified in Kargo source, not against a registry. Constraints: no `platform` on the subscription (filters out every tag); no OCI index; no non-JSON custom config (breaks discovery of all tags). Helm `chart` subscription rejected: no digest in Freight.
- **App kind**: same `app.yml`; `module:` instead of `image:` makes it a terraform app. Separate file/dir rejected: duplicates routing, graph, RBAC.
- **Config**: `env` + `envFrom` with existing layering (app → stage → target, envFrom files below env). Workflow exports env; teams write `TF_VAR_<name>` themselves.
- **Env keys** relaxed for all apps: `^[A-Za-z_][A-Za-z0-9_]*$` (some apps need non-SCREAMING_SNAKE).
- **Apply**: plan + apply in one run.
- **Cloud**: GCP + GCS backend.
- **Target names**: free (no `-tf` convention).

## Schema (`platform/render/`)

### `#App` (app.cue)
- Exactly one of `image` | `module` (same repo regex as `image`), error otherwise: `"set exactly one of image (K8s app) or module (terraform app)"`.
- `moduleConstraint?`: SemVer range, like `imageConstraint`.
- Terraform app (module set) at any layer: `replicas`, `resources`, `port`, `deployWith` → `error("not allowed on a terraform app")`. Allowed: `env`, `envFrom`, `workflow`, `stages`.
- `deploys` entry gains `kind: "k8s" | "terraform"`; terraform: `deployWith: "gha"` forced, `workflow.file` default `terraform.yaml` (repo/ref defaults unchanged), `module` instead of `image`; no replicas/cpu/memory/placement check.

### `#Target` (team.cue)
- `config.platform: *"K8S" | "TERRAFORM"`.
- K8S: `cluster`, `namespace` required (as today).
- TERRAFORM: `terraform: {gcpProject: GCP project id regex, backend: {bucket: GCS bucket regex, prefix?: string}}`; `cluster`/`namespace` not allowed. No region: terraform Stages run on the hub's default controller (no Argo CD involved, ADR 0015 shards exist only for Argo CD). Prefix default `<sys>/<app>/<target>` (resolved per deploy).
- Both: `environment`, `auth.wifEmail`, `billing`. Environment check against cluster only for K8S.
- App/target kind match: module app → every target TERRAFORM, image app → every target K8S; error names the target.
- Prod rule (`autoPromote` blocked on stages reaching prod) uses `tgt.environment`: unchanged, covers both.

### `#EnvironmentVariables`
- Key regex `^[A-Za-z_][A-Za-z0-9_]*$` for all apps and envFrom files.

## Render

- Terraform deploy → `rendered/apps/<sys>/<app>/<stage>/<target>/terraform.yaml`, single YAML doc:
  ```yaml
  system: payments
  app: infra
  stage: dev
  target: us-dev-tf
  environment: dev
  gcpProject: team1-dev
  wifEmail: team1-dev@team1-dev.iam.gserviceaccount.com
  backend: {bucket: tfstate-team1-dev, prefix: payments/infra/us-dev-tf}
  module: ghcr.io/org/mod        # promotion pins <repo>:<tag>@<digest>
  env: {TF_VAR_name: x, ...}     # secret: refs passed through for the workflow
  ```
- No `manifests.yaml`/`deployment.yaml`, no `rendered/argocd/deploys/...` file, no AppProject destination (`_argoDeploys` already filters on `deployWith`; `_deployRegion`/`manifests` must skip terraform deploys).

## Kargo (delivery.cue)

- Warehouse: git subscription on app rendered path (unchanged) + `image` subscription on `module` (`SemVer`, `moduleConstraint` → `constraint`, `discoveryLimit: 10`, no `platform`). Manual Freight.
- Target Stage: K8S keeps `shard` = cluster region; TERRAFORM gets no `spec.shard` (hub default controller, like Warehouses/control-flow Stages; it already has git creds and `kargo-shared-resources/github-actions`). Labels add `example.com/platform: k8s|terraform`; `example.com/deploy-with: gha`.
- Steps: git-clone, git-clear, copy, `yaml-update` `./out/terraform.yaml` key `module` → `<repo>:<tag>@<digest>`, git-commit (message names module tag), git-push, `dispatch` (inputs `system, app, stage, target, commit, module`), `wait` with `retry.timeout: 60m` (30m stays for K8s gha).
- RBAC, control-flow Stages, graph, break-glass: unchanged.

## Example workflow + module

- `.github/workflows/terraform.yaml` (`workflow_dispatch`, inputs above, inputs via env):
  1. checkout `commit`
  2. check `module` input == `yq .module terraform.yaml`
  3. `oras pull <module>` into `./module`
  4. export `.env` from terraform.yaml
  5. `terraform init -backend=false && terraform validate`
  6. job summary (`_demo: nothing applied_`)
  Real steps left commented: `google-github-actions/auth` with WIF `wifEmail`, `terraform init -backend-config=bucket=... -backend-config=prefix=...`, `terraform plan -out tfplan && terraform apply tfplan`.
  Versions pinned (terraform, oras actions).
- `examples/terraform/bucket/`: minimal module (variables + no-op/`terraform_data`).
- `mise run tf-module-push [version]` (cheap, optional): `oras push ghcr.io/<owner>/tf-bucket:<v>` with default config, single manifest; idempotent (skip if tag exists, `oras manifest fetch`). oras pinned in `.mise.toml`. Needs a PAT with `write:packages`; not part of `up`. Demo stays render-only if never run (Warehouse just discovers nothing).

## Demo data

- `systems/payments/apps/infra/app.yml`: `module: ghcr.io/pitoniak32/tf-bucket`, stages dev → `us-dev-tf`, staging → `us-stg-tf`, sample `env` incl. lowercase `TF_VAR_bucket_name` and one envFrom file.
- `systems/payments/targets/{us-dev-tf,us-stg-tf}/target.yml` (TERRAFORM).

## Docs

- ADR 0021 `terraform-apps.md` (decisions + considered options above).
- GLOSSARY: Terraform app, Terraform target, Module; update App/Target/deploy method wording.
- README + `platform/render/README.md` short section; `docs/sync-boundaries.md` if relevant.

## Testing

- Golden: new testdata system with a terraform app (2 stages, envFrom, workflow override) + existing K8s cases unchanged except env-key change.
- Lint cases (`file:line:col: message`):
  - image and module both / neither
  - module app → K8S target; image app → TERRAFORM target
  - `replicas` / `port` / `deployWith` on terraform app (app, stage, target level)
  - TERRAFORM target with `cluster` or `namespace`, missing `terraform.backend.bucket`
  - lowercase env key accepted; `1BAD` rejected
- `mise run schemas`, `mise run test`, `platform/appsets/check.sh`.
- No cluster change → no `mise run up` needed; Kargo behaviour (OCI discovery) verified manually after `tf-module-push` if run.

## Consequences

- `mise run kargo-restore` re-promotes terraform targets = re-applies.
- Switching a target's platform not supported; make a new target.
- Terraform Stages have no `spec.shard`; golden asserts it (gotchas: unset shard → hub controller).
- Stage health: always Healthy after success (ADR 0020 caveat); run conclusion is the only signal.
- Module discovery breaks for the whole repo if any tag is an index or has invalid config; documented in ADR + `docs/gotchas.md`.
