# Cloud Run targets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third target platform, `CLOUD_RUN`. An image app can deploy to it in any stage, rendered as a Knative `service.yaml` and promoted through gha with a workflow that only prints the manifest. TERRAFORM's `terraform:` section is flattened into `config.gcpProject` and `config.backend`.

**Architecture:** Changes are in the CUE renderer (`platform/render/`) plus one example workflow. Task 1 flattens TERRAFORM's fields, a refactor that must leave the golden output unchanged. Task 2 adds the CLOUD_RUN target schema and validation (lint only). Task 3 renders and promotes image apps on CLOUD_RUN targets. Task 4 adds the workflow and docs.

**Tech Stack:** CUE, bash test harness, yq, GitHub Actions YAML, jj.

**Spec:** `docs/adr/0024-cloud-run-targets.md` (builds on `docs/adr/0025-target-naming-convention.md`)

## Global Constraints

- Platforms: `"K8S" | "TERRAFORM" | "CLOUD_RUN"`. The platform-error message becomes `expected K8S, TERRAFORM or CLOUD_RUN`. Name suffix: `_platformSuffix.CLOUD_RUN: "cr"`.
- Target fields:

  | field | K8S | TERRAFORM | CLOUD_RUN |
  |---|---|---|---|
  | `cluster`, `namespace` | required | forbidden | forbidden |
  | `gcpProject` | forbidden | required | required |
  | `region` | forbidden | required | required |
  | `backend` | forbidden | required | forbidden |

  `gcpProject` uses the existing pattern and message (`expected a GCP project id`). `backend` is `{bucket!, prefix?}` with the existing patterns and messages.
- A leftover `terraform:` key fails with `moved: use config.gcpProject / config.backend` (raw-YAML check, on `config.terraform`).
- An image app may use K8S and CLOUD_RUN targets; a `module` app only TERRAFORM. Message for an image app on TERRAFORM, or a module app on K8S/CLOUD_RUN: `target <t> is <P>; <K8s|terraform> apps need <K8S or CLOUD_RUN|TERRAFORM> targets`.
- CLOUD_RUN deploy defaults:
  - `replicas` 0, written as `autoscaling.knative.dev/minScale`. The minReplicas `policy` applies to K8S targets only.
  - `port` 8080.
  - `cpu`/`memory`: the same defaults as K8S, written as `resources.limits`.
  - `deployWith` is always `"gha"`; a layer's `deployWith` is ignored.
  - `workflow.file` defaults to `cloud-run.yaml`.
- The env secret `secret:<p>/<s>[@<v>]` becomes `valueFrom.secretKeyRef: {name: "projects/<p>/secrets/<s>", key: "<v>"}`, where `<v>` defaults to `latest`.
- Rendered file: `rendered/apps/<sys>/<app>/<stage>/<target>/service.yaml`, a single document.
- Promotion: `yaml-update` on `./out/service.yaml`, key `spec.template.spec.containers.0.image`. The wait timeout is 30m. There's no `spec.shard`, no Argo CD params and no AppProject destination.
- Teams never write CUE. Messages come from `error("...")` placed on a YAML field. Watch for CUE shadowing.
- Run `cue fmt ./platform/render/...` before each commit. Run `mise run schemas` after schema changes. Update golden only with `UPDATE=1`, after reviewing the diff.
- VCS is jj: `jj describe -m "<msg>\n\nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" && jj new`. No git mutations, no push.

## Review Focus

1. **The minReplicas policy must not apply to CLOUD_RUN.** An npr/prd CLOUD_RUN deploy with the default `minScale` 0 must lint clean (Task 3 test).
2. **Mixed app.** One app with a K8S stage and a CLOUD_RUN stage gets one Warehouse, an Argo CD Application only for the K8S target, and a gha Stage for the CLOUD_RUN target (Task 3 assertions).
3. **`deployWith: argocd` set at app level on a mixed app** must not make the CLOUD_RUN target argocd (Task 3 test).
4. **Secret ref without `@<v>`** renders `key: latest` (Task 3 golden + assertion).
5. **A CLOUD_RUN target named with `k8s`, or on the wrong region,** gets the ADR 0025 name error with `expected <region>-<env>-cr` (Task 2 lint case).

---

### Task 1: Flatten TERRAFORM fields

**Files:**
- Modify: `platform/render/team.cue` (`#Target.config`: drop `terraform?`, add top-level `gcpProject?`, `backend?`; required/forbidden per the table for K8S/TERRAFORM)
- Modify: `platform/render/input.cue` (raw check for the leftover `terraform:` key)
- Modify: `platform/render/app.cue` (`backend` from `tgt.backend`), `platform/render/terraform.cue` (`gcpProject: d.tgt.gcpProject`), `platform/render/pipeline.cue:39` (`_capTargets[tn].config.gcpProject`)
- Modify: TERRAFORM `target.yml` files in `platform/render/testdata/root/` and `systems/` (move the fields up), `testdata/lint-cases.tsv` (existing terraform cases use the new paths)
- Regenerate: `platform/render/schemas/`

- [ ] **Step 1: Write the failing lint cases** against a TERRAFORM fixture target:
  - `.config.terraform = {"gcpProject": "acme-x"}` → `config\.terraform: moved: use config\.gcpProject / config\.backend`
  - `del(.config.gcpProject)` → error located on `config.gcpProject`
  - On a K8S fixture: `.config.gcpProject = "acme-payments-dev"` → `gcpProject not allowed on a K8S target`, and `.config.backend = {"bucket": "b"}` → `backend not allowed on a K8S target`

  Rewrite the existing `.config.terraform...` cases to the flat paths.
- [ ] **Step 2: Migrate the fixtures and real TERRAFORM targets.** Run `mise run test`. Expected: FAIL.
- [ ] **Step 3: Implement the changes.**
- [ ] **Step 4: Run `mise run schemas`, `cue fmt`, then `mise run test`.** Expected: pass with the **golden unchanged**; a golden diff means a bug. Also run `mise run lint`, `cue fmt --check ./platform/render/...` and `mise run render`.
- [ ] **Step 5: Commit** with the message `refactor(render): flat gcpProject/backend on TERRAFORM targets (ADR 0024)`.

### Task 2: CLOUD_RUN target schema + validation

**Files:**
- Modify: `platform/render/team.cue` (platform enum and message, `_platformSuffix`, CLOUD_RUN required/forbidden)
- Modify: `platform/render/input.cue` (the region-required raw check covers CLOUD_RUN, message `region required on a CLOUD_RUN target; allowed: ...`; the name rule's region source is `config.region` for TERRAFORM and CLOUD_RUN)
- Create fixture: `platform/render/testdata/root/systems/payments/targets/us-east4-npr-cr/target.yml`:
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
  No app uses it yet, so it renders nothing.
- Modify: `testdata/lint-cases.tsv`, `scripts/test.sh` (dir-rename name case)
- Regenerate: `platform/render/schemas/`

- [ ] **Step 1: Write the failing lint cases** on `us-east4-npr-cr/target.yml`:
  - `del(.config.region)` → `region required on a CLOUD_RUN target; allowed: us-central1, us-east1, us-east4`
  - `.config.region = "us-east9"` → `unknown region; allowed:`
  - `.config.cluster = "us-east4"` → `cluster not allowed on a CLOUD_RUN target`
  - `.config.namespace = "x"` → `namespace not allowed on a CLOUD_RUN target`
  - `.config.backend = {"bucket": "bkt"}` → `backend not allowed on a CLOUD_RUN target`
  - `del(.config.gcpProject)` → error on `config.gcpProject`
  - `.config.platform = "LAMBDA"` → `expected K8S, TERRAFORM or CLOUD_RUN`

  `test.sh` name case: rename the dir to `us-east4-npr-k8s-x`, expecting `expected us-east4-npr-cr[-<qualifier>]`.
- [ ] **Step 2: Run `mise run test`.** Expected: FAIL.
- [ ] **Step 3: Implement the changes.**
- [ ] **Step 4: Run `mise run schemas`, `cue fmt`, then `mise run test`.** Expected: pass, golden unchanged. Then `mise run lint` and `cue fmt --check`.
- [ ] **Step 5: Commit** with the message `feat(render): CLOUD_RUN target platform schema (ADR 0024)`.

### Task 3: Image apps on CLOUD_RUN: render + promotion

**Files:**
- Modify: `platform/render/app.cue` (`deploys`: K8S or CLOUD_RUN for image apps; CLOUD_RUN defaults; `policy` only when `tgt.platform == "K8S"`; `deployWith: "gha"` on CLOUD_RUN; workflow defaults `_defaults.crWorkflow: {repo: _ghRepo, file: "cloud-run.yaml", ref: "main"}`)
- Create: `platform/render/cloudrun.cue` (`cloudRunFiles`, mirroring `terraform.cue`)
- Modify: `platform/render/files.cue` (`for p, f in cloudRunFiles {(p): yaml.Marshal(f)}`)
- Modify: `platform/render/delivery.cue:157-159` (`yaml-update` path/key per target platform; `example.com/platform` label: `k8s`, `terraform` or `cloudrun`, keeping existing values), `platform/render/pipeline.cue` (CLOUD_RUN targets list `gcpProject` + `region`)
- Create fixture app: `platform/render/testdata/root/systems/payments/apps/web/app.yml`:
  ```yaml
  image: ghcr.io/example/web
  deployWith: argocd   # app-level; must not affect the CLOUD_RUN target (Review Focus 3)
  env:
    GREETING: hello
    API_KEY: secret:acme-secrets/web-api-key
  stages:
    dev:
      targets: {us-east4-dev-k8s: {}}
    staging:
      targets: {us-east4-npr-cr: {}}
  ```
  `deployWith: argocd` is set explicitly at app level, for Review Focus 3. The fixture `owners.yml` already covers dev and staging promoters.
- Modify: `scripts/test.sh`, golden

`service.yaml` shape (exact keys; the values come from the deploy):

```yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: <app>
  namespace: <gcpProject>
  labels: {app.kubernetes.io/name, app.kubernetes.io/managed-by: cue-platform, example.com/system, stage, target, environment, environment-type, billing}  # same keys as k8s.cue podLabels
  annotations: {run.googleapis.com/location: <region>}
spec:
  template:
    metadata:
      labels: <same as metadata.labels>
      annotations: {autoscaling.knative.dev/minScale: "<replicas>"}
    spec:
      containers:
        - name: <app>
          image: <image repo>          # Freight pins the digest at promotion
          ports: [{containerPort: <port>}]
          env: [sorted by name; value or valueFrom.secretKeyRef]
          resources: {limits: {cpu: <cpu>, memory: <memory>}}
```

- [ ] **Step 1: Write the failing assertions in `test.sh`** (payments render `$k` = kargo, `$tmp/apps/payments/web/...`):
  - `apps/payments/web/staging/us-east4-npr-cr/service.yaml` exists, with `.kind == "Service"`, `.apiVersion == "serving.knative.dev/v1"`, `.metadata.namespace == "acme-payments-npr"`, `.metadata.annotations."run.googleapis.com/location" == "us-east4"`.
  - `minScale == "0"` on that npr target with no replicas set (Review Focus 1), `containerPort == 8080`.
  - Env `API_KEY` → `valueFrom.secretKeyRef == {"name":"projects/acme-secrets/secrets/web-api-key","key":"latest"}`, and `GREETING` → `value: hello`.
  - Stage `web-us-east4-npr-cr`: `example.com/deploy-with == gha`, no `.spec.shard`, the yaml-update step path is `./out/service.yaml` with key `spec.template.spec.containers.0.image`, and the `workflowFile` var is `cloud-run.yaml`.
  - Stage `web-us-east4-dev-k8s`: last step `argocd-update`; `argocd/deploys/us-east4/payments/web-us-east4-dev-k8s.json` exists; no `argocd/deploys/*/payments/web-us-east4-npr-cr.json`; one Warehouse `web`.
  - `example.com/platform` label is `cloudrun` on the CR Stage and `k8s` on the K8S Stage.
  - Lint case: a module app (`infra`) given `us-east4-npr-cr` → `target us-east4-npr-cr is CLOUD_RUN; terraform apps need TERRAFORM targets`. An image app given a TERRAFORM target → `K8s apps need K8S or CLOUD_RUN targets`.
- [ ] **Step 2: Run `mise run test`.** Expected: FAIL.
- [ ] **Step 3: Implement the changes.** Reuse `_secretRe` from `k8s.cue`. Unlike K8S, secrets are always rendered on CLOUD_RUN (no secret-store gate).
- [ ] **Step 4: Review the golden diff.** Expected: a new web app (deployment/manifests for dev, `service.yaml` for staging), web Kargo objects, a web params file, pipeline JSON for web, and `promote-web-*` roles. Nothing changes for existing apps except the `example.com/platform` label, if its source moved but its values stayed. Then run `UPDATE=1 mise run test` followed by `mise run test`. Also run `mise run lint`, `mise run codeowners` (new `apps/web/` line expected; commit `.github/CODEOWNERS` if the generated fragment changes), and `cue fmt --check`.
- [ ] **Step 5: Real deploy.** Create `systems/payments/targets/us-east4-npr-cr/target.yml`: the same shape as the fixture, with `gcpProject`/`auth.wifEmail`/`billing` copied from the real payments TERRAFORM targets' conventions. In `systems/payments/apps/worker/app.yml`, switch the `staging` stage's target from `us-east4-npr-k8s` to `us-east4-npr-cr`, dropping any staging-target-level `deployWith`/`workflow` that only applied to K8S. Run `mise run lint`, `mise run render`, `mise run codeowners` (no drift expected) and `cue fmt --check`.
- [ ] **Step 6: Commit** with the message `feat(render): render and promote image apps on CLOUD_RUN targets (ADR 0024)`.

### Task 4: Example workflow + docs

**Files:**
- Create: `.github/workflows/cloud-run.yaml`. Copy `deploy.yaml`'s shape: same inputs (`system, app, stage, target, commit, image`), inputs via env, checkout `${{ inputs.commit }}`. It checks that `yq '.spec.template.spec.containers[0].image' service.yaml` equals `$IMAGE` (`::error` on mismatch), and appends a summary plus `service.yaml` in a ```yaml block to `$GITHUB_STEP_SUMMARY`. Add commented-out steps for `google-github-actions/auth` (WIF, `service_account` from the deploy identity) and `gcloud run services replace service.yaml --region <region>` with the note `# not applied yet (ADR 0024)`.
- Modify: `docs/adr/0024-cloud-run-targets.md` (Status → accepted; under App add one line: "The minReplicas `policy` applies to K8S targets only."), `docs/adr/0021-terraform-apps.md` (one line: "Amended by ADR 0024: `terraform: {gcpProject, backend}` → `config.gcpProject`, `config.backend`"), `CLAUDE.md` (Layout target bullet: CLOUD_RUN platform, flat fields), `GLOSSARY.md` (Platform), `README.md` (platforms table/mention), `platform/render/README.md` (target fields, `service.yaml` output).

- [ ] **Step 1: Write the workflow.** Check it with `yq '.' .github/workflows/cloud-run.yaml >/dev/null` and, if installed, `actionlint .github/workflows/cloud-run.yaml`.
- [ ] **Step 2: Update the docs.** Then run `grep -rnE 'terraform\.gcpProject|terraform: \{|K8S or TERRAFORM' CLAUDE.md README.md GLOSSARY.md docs platform/render/README.md --exclude-dir=superpowers`. Expected: hits only in ADR history (0021, 0024).
- [ ] **Step 3: Run `mise run test`.** Expected: pass.
- [ ] **Step 4: Commit** with the message `feat: cloud-run example workflow, docs (ADR 0024)`.
