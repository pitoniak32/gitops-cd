# Terraform Apps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Teams define terraform apps (`module:` OCI repo) + TERRAFORM targets in YAML; Kargo promotes them by pinning the module digest into a rendered `terraform.yaml` on the env branch and dispatching a GHA workflow (plan + apply in one run).

**Architecture:** Same `app.yml`/`target.yml` files, kind decided by `module` vs `image` and `config.platform`. Renderer (`platform/render/*.cue`) emits `terraform.yaml` per target instead of k8s manifests; Kargo Warehouse subscribes to the module as an `image`; target Stage reuses the ADR 0020 gha steps on the hub controller (no shard). `gcd kargo-restore` learns terraform branches.

**Tech Stack:** CUE, bash test scripts (yq/jq), Go (`platform/cli`), GitHub Actions, oras, Kargo 1.12.5.

**Spec:** `docs/superpowers/specs/2026-11-10-terraform-apps-design.md`

## Global Constraints

- VCS is jj: commit with `jj commit -m "<msg>"` (ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`); no git mutations, no push.
- Never edit `rendered/` or `.github/CODEOWNERS`.
- Team-facing errors via CUE `error("...")`; teams never write CUE (ADR 0006).
- Watch CUE field shadowing; use `_cap`/`_capStages`/`_capTargets`/`_repoURL`.
- Env key regex everywhere: `^[A-Za-z_][A-Za-z0-9_]*$`.
- Terraform wait `retry.timeout: 60m`; K8s gha stays `30m`. Terraform `workflow.file` default `terraform.yaml`.
- Backend prefix default `<cap>/<app>/<target>`.
- Module subscription: `imageSelectionStrategy: SemVer`, `discoveryLimit: 10`, **no `platform`**.
- Terraform target Stages: no `spec.shard`.
- Pin versions (`.mise.toml`, action SHAs/tags in workflows).
- After schema changes: `mise run schemas`. Test: `mise run test` (golden accept: `UPDATE=1 mise run test`, review `jj diff` of golden first).

## Review Focus

1. Mixed capability: K8s + terraform apps in one capability; K8s golden output must not change except where intended (only `env` key relax + new app). Pinned by: Task 5 asserts existing `my-app`/`worker` assertions in `test.sh` stay green unmodified.
2. Terraform app sharing a stage with K8s apps whose stage has `autoPromote` + prod: prod rule still fires for TERRAFORM prod target. Pinned by: Task 2 lint case.
3. `envFrom` file on a TERRAFORM target with lowercase keys merges like K8s. Pinned by: Task 4 golden fixture (`tf-dev` envFrom file with `TF_VAR_bucket_name`).
4. Kargo expression `}}` cut-off in new dispatch body with `module` input. Pinned by: existing `test.sh` `}}` grep covers new golden.
5. `kargo-restore` on a terraform branch without `terraform.yaml` module pin (never promoted / pre-feature): skipped with clear message, not crash. Pinned by: Task 6 Go test.

---

### Task 1: Relax env keys

**Files:**
- Modify: `platform/render/app.cue:11` (`#EnvironmentVariables`)
- Modify: `platform/render/testdata/lint-cases.tsv:15` (the `.bad_key` case)
- Regenerate: `platform/render/schemas/*.json`

- [ ] **Step 1:** Lint case line 15 → expr `."1BAD" = "x"`, want `^capabilities/payments/targets/us-dev/environment-variables/launchdarkly\.yml:[0-9]+:[0-9]+: .*1BAD`. In `test.sh` after `lint ok`: copy root, `yq -i '.env.TF_VAR_lower_ok = "x"'` on `apps/my-app/app.yml`, assert `scripts/lint.sh "$r" payments` exits 0 (`echo "lowercase env ok"`).
- [ ] **Step 2:** `mise run test` → FAIL at the lowercase assertion.
- [ ] **Step 3:** Key regex → `^[A-Za-z_][A-Za-z0-9_]*$`.
- [ ] **Step 4:** `mise run schemas && mise run test` → PASS, golden unchanged.
- [ ] **Step 5:** `jj commit -m "render: allow non-screaming env keys"`.

### Task 2: TERRAFORM targets

**Files:**
- Modify: `platform/render/team.cue` (`#Target`)
- Modify: `platform/render/pipeline.cue:34` (cluster only for K8S)
- Create: `platform/render/testdata/root/capabilities/payments/targets/{tf-dev,tf-prod}/target.yml`
- Create: `platform/render/testdata/root/capabilities/payments/targets/tf-dev/environment-variables/bucket.yml` (`TF_VAR_bucket_name: payments-dev`)
- Modify: `platform/render/testdata/lint-cases.tsv`

**Interfaces:**
- Produces: `targets[t].config.platform: "K8S" | "TERRAFORM"` (default `"K8S"`); TERRAFORM: `config.terraform: {gcpProject: string, backend: {bucket: string, prefix?: string}}`; no `cluster`/`namespace` (closed). Both: `environment`, `auth.wifEmail`, `billing`.

- [ ] **Step 1: Fixtures + lint cases.** `tf-dev` (dev, gcpProject `team1-dev`, bucket `tfstate-team1-dev`), `tf-prod` (prod, `team1-prod`, `tfstate-team1-prod`, `prefix: payments/custom`). No app uses them yet (unused targets lint clean today — verify). Lint cases (want regex abbreviated, all `^capabilities/payments/targets/tf-dev/target\.yml:[0-9]+:[0-9]+: `):
  - `.config.cluster = "us"` → `.*cluster.*not allowed`
  - `del(.config.terraform.backend.bucket)` → `.*bucket`
  - `.config.terraform.gcpProject = "Bad"` → `.*gcpProject`
  - K8S target `us-dev` with `del(.config.namespace)` → `.*namespace`
- [ ] **Step 2:** `mise run test` → FAIL (platform `TERRAFORM` rejected).
- [ ] **Step 3:** `#Target.config`: `platform: *"K8S" | "TERRAFORM"`; `if platform == "K8S"` block holds cluster/namespace/environment-vs-cluster check; `if platform == "TERRAFORM"` holds `terraform`. GCP project regex `^[a-z][a-z0-9-]{4,28}[a-z0-9]$` (same as `#EnvValue`), bucket `^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$`, prefix `^[A-Za-z0-9._/-]+$`. Disallow other branch's fields with `error("<field> not allowed on a <platform> target")`. `pipeline.cue`: emit `cluster` only for K8S, `gcpProject` for TERRAFORM.
- [ ] **Step 4:** `mise run schemas && mise run test` → PASS (golden pipelines unchanged: targets unused).
- [ ] **Step 5:** `jj commit -m "render: TERRAFORM targets"`.

### Task 3: Terraform app schema + deploys

**Files:**
- Modify: `platform/render/app.cue` (`#App`, `#Layer` use, `_defaults`, `deploys`, `_placements`)
- Modify: `platform/render/k8s.cue` (`manifests` only for `kind == "k8s"`)
- Modify: `platform/render/delivery.cue` (`_argoDeploys` filter on `kind == "k8s"` too, `_deployRegion` uses it — already)
- Create: `platform/render/testdata/root/capabilities/payments/apps/infra/app.yml`
- Modify: `platform/render/testdata/lint-cases.tsv`

**Interfaces:**
- Produces on each `deploys["<app>/<target>"]`: `kind: "k8s" | "terraform"`; terraform entries have `module: string`, `deployWith: "gha"`, `workflow: {repo, file, ref}` (file default `terraform.yaml`), `env`, `envFrom`, `tgt`, `backend: {bucket, prefix}` (prefix resolved), no `replicas/cpu/memory/image/port`.
- `apps[a].module?: string`, `apps[a].moduleConstraint?: string`.

- [ ] **Step 1: Fixture.** `apps/infra/app.yml`: `module: ghcr.io/pitoniak32/tf-bucket`, `moduleConstraint: ^1`, `envFrom: [bucket]` at `stages.dev.targets.tf-dev`, `env: {TF_VAR_region: us-central1}`, stages `dev: {targets: {tf-dev: {}}}`, `prod: {targets: {tf-prod: {env: {TF_VAR_region: us-east1}}}}`, `workflow: {ref: v1}`. Owners inherited.
- [ ] **Step 2: Lint cases** on `capabilities/payments/apps/infra/app.yml`:
  - `.image = "ghcr.io/x/y"` → `.*set exactly one of image \(K8s app\) or module \(terraform app\)`
  - `del(.module)` → same message
  - `.replicas = 2`, `.stages.dev.port = 80`, `.stages.dev.targets.tf-dev.deployWith = "argocd"`, `.resources.cpu = "1"` → `.*not allowed on a terraform app`
  - `.stages.dev.targets = {"us-dev": {}}` → `.*target us-dev is K8S; terraform apps need TERRAFORM targets`
  - on `apps/worker/app.yml`: `.stages.dev.targets = {"tf-dev": {}}` → `.*target tf-dev is TERRAFORM; K8s apps need K8S targets`
  - on `stages/dev.yml`… n/a; `stages/prod.yml` `.autoPromote = true` already covered (prod rule now also via tf-prod).
- [ ] **Step 3:** `mise run test` → FAIL.
- [ ] **Step 4:** Implement. `#App`: `image?`, `module?` (same regex), `moduleConstraint?`; exactly-one check outside `#App` (like `apps: [_]: stages: struct.MinFields(1)`) so the schema stays precise; terraform-forbidden fields checked on each layer when `module` set. `deploys`: branch on `a.module != _|_`; `_defaults.workflow.file` per kind. `_placements` only K8S targets.
- [ ] **Step 5:** Minimal delivery.cue change so render builds: Warehouse image subscription `repoURL` = `apps[an].image` or `apps[an].module`; everything else about terraform Stages waits for Task 5. `mise run test` → lint cases PASS; golden diff = `pipelines/payments.json` + `kargo/payments.yaml` gain `infra`, nothing else. `UPDATE=1 mise run test`.
- [ ] **Step 6:** `jj commit -m "render: terraform app schema"`.

### Task 4: Render `terraform.yaml`

**Files:**
- Create: `platform/render/terraform.cue` (`terraformFiles` per terraform deploy)
- Modify: `platform/render/files.cue` (emit it)
- Modify: `platform/render/scripts/test.sh` (assertions)

**Interfaces:**
- Consumes: Task 3 `deploys` terraform fields.
- Produces: `rendered/apps/<cap>/<app>/<stage>/<target>/terraform.yaml`, single doc, keys in this order: `capability, app, stage, target, environment, gcpProject, wifEmail, backend{bucket,prefix}, module, env`.

- [ ] **Step 1: Assertions** in `test.sh` (new block `echo "terraform render ok"`):
  - `ls $tmp/apps/payments/infra/dev/tf-dev` == `terraform.yaml` only.
  - `yq -o=json -I=0 '.' .../infra/dev/tf-dev/terraform.yaml` == `{"capability":"payments","app":"infra","stage":"dev","target":"tf-dev","environment":"dev","gcpProject":"team1-dev","wifEmail":"<fixture>","backend":{"bucket":"tfstate-team1-dev","prefix":"payments/infra/tf-dev"},"module":"ghcr.io/pitoniak32/tf-bucket","env":{"TF_VAR_bucket_name":"payments-dev","TF_VAR_region":"us-central1"}}`
  - tf-prod: `backend.prefix == "payments/custom"`, `env.TF_VAR_region == "us-east1"`.
  - No `argocd/deploys/*/payments/infra-*` files (extend existing argocd file-list assertion: unchanged list).
- [ ] **Step 2:** `mise run test` → FAIL.
- [ ] **Step 3:** Implement `terraformFiles`; `files.cue` adds `"apps/\(_cap)/\(d.app)/\(d.stage)/\(d.target)/terraform.yaml": yaml.Marshal(...)` for terraform deploys. `env` keys sorted (CUE struct order follows layering — use explicit sorted map if diff unstable).
- [ ] **Step 4:** `UPDATE=1 mise run test` after reviewing golden diff; then `mise run test` → PASS.
- [ ] **Step 5:** `jj commit -m "render: terraform.yaml per terraform target"`.

### Task 5: Kargo for terraform apps

**Files:**
- Modify: `platform/render/delivery.cue` (Warehouse subscription, target Stage shard/labels/vars/steps)
- Modify: `platform/render/scripts/test.sh`

**Interfaces:**
- Consumes: Task 3 `deploys[..].kind`, `.module`, `apps[a].moduleConstraint`.
- Produces: Stage vars `repo, src, branch, image, workflowRepo, workflowFile, workflowRef` (var `image` holds the module repo — `gcd kargo-restore` reads vars by name); labels `example.com/platform: k8s|terraform` on target Stages; terraform commit message `promote <cap>/<app> to <stage>/<target> from <sha>, module <tag>`.

- [ ] **Step 1: Assertions** (`echo "terraform kargo ok"`), with `st()` from the gha block:
  - Warehouse `infra` subscriptions: `.spec.subscriptions[1].image | .repoURL + " " + .imageSelectionStrategy + " " + .constraint + " " + (.discoveryLimit|tostring) + " " + (has("platform")|tostring)` == `ghcr.io/pitoniak32/tf-bucket SemVer ^1 10 false`.
  - `infra-tf-dev`: `.spec.shard // "-"` == `-`; labels platform/deploy-with == `terraform gha`; my-app-us-dev platform label == `k8s`.
  - steps == `git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http`; yaml-update `.config.path` == `./out/terraform.yaml`, key `module`, value `${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}`.
  - vars values `[4:]` == `pitoniak32/gitops-cd terraform.yaml v1`.
  - dispatch body contains `'module': vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest` and not `'image':`.
  - wait `.retry.timeout` == `60m`; worker-us-dev stays `30m`.
  - ProjectConfig auto-promotion list gains `infra-tf-dev` (update existing assertion string).
  - shard assertion (`my-app-*` list) unchanged.
- [ ] **Step 2:** `mise run test` → FAIL.
- [ ] **Step 3:** Implement: Warehouse image subscription from `apps[an].image` or `.module` (+ constraint); `spec.shard` only for K8S; yaml-update path/key by kind; dispatch inputs `module` vs `image`; wait timeout by kind; commit message `module`/`image`; `argocd-update` only for k8s argocd.
- [ ] **Step 4:** Review golden diff, `UPDATE=1 mise run test`, `mise run test` → PASS (incl. `}}` grep).
- [ ] **Step 5:** `jj commit -m "render: kargo pipeline for terraform apps"`.

### Task 6: `gcd kargo-restore` for terraform targets

**Files:**
- Modify: `platform/cli/internal/kargo/restore.go` (`FreightFromBranch`, head reading, commit-message regex)
- Modify: `platform/cli/internal/kargo/restore_test.go`

**Interfaces:**
- `func FreightFromBranch(warehouse, repoURL, imageRepo, headMessage, pinned string) (*unstructured.Unstructured, error)` — `pinned` = `<repo>:<tag>@<digest>` from `deployment.yaml` container image **or** `terraform.yaml` `.module`.
- `func terraformModule(raw []byte) string` next to `deploymentImage`.

- [ ] **Step 1: Tests:** `TestTerraformModule` (yaml with `module: ghcr.io/o/m:1.2.3@sha256:abc` → that string; missing → `""`); `TestFreightFromBranch` case with head message `promote payments/infra to dev/tf-dev from <40-hex>, module 1.2.3` → Freight images `[{repoURL: ghcr.io/o/m, tag: 1.2.3, digest: sha256:abc}]`; case unpinned `module: ghcr.io/o/m` → error containing `no pinned`.
- [ ] **Step 2:** `cd platform/cli && go test ./internal/kargo/` → FAIL.
- [ ] **Step 3:** Commit-message regex accepts `, (image|module) <tag>`; Head fetches `deployment.yaml`, falls back to `terraform.yaml` via same `gh api contents` call; error text `no pinned artifact (<repo>:<tag>@<digest>) in deployment.yaml or terraform.yaml`.
- [ ] **Step 4:** `go test ./... && go vet ./...` → PASS.
- [ ] **Step 5:** `jj commit -m "cli: kargo-restore terraform targets"`.

### Task 7: Example workflow, module, push task, demo data

**Files:**
- Create: `.github/workflows/terraform.yaml`
- Create: `examples/terraform/bucket/{main.tf,variables.tf}`
- Create: `platform/mise-tasks/tf-module-push`
- Modify: `.mise.toml` (pin `oras`, `terraform`)
- Create: `capabilities/payments/apps/infra/app.yml`, `capabilities/payments/targets/{us-dev-tf,us-stg-tf}/target.yml`

- [ ] **Step 1: Workflow** `deploy terraform (example)`: `workflow_dispatch` inputs `capability, app, stage, target, commit, module` (all required strings), `permissions: {contents: read, packages: read}`, inputs via `env:`. Steps: checkout `commit`; fail if `yq .module terraform.yaml != $MODULE`; `oras-project/setup-oras` (pinned) + `oras login ghcr.io -u $GITHUB_ACTOR --password-stdin <<<"$GITHUB_TOKEN"` + `oras pull "$MODULE" -o module`; `hashicorp/setup-terraform` (pinned, same version as `.mise.toml`); export `.env` via `yq -o=shell '.env' terraform.yaml >> "$GITHUB_ENV"`-equivalent (one `KEY=value` per line; values with newlines rejected); `terraform -chdir=module init -backend=false && terraform -chdir=module validate`; summary table + `_demo: nothing applied_`. Commented real block: `google-github-actions/auth` (`service_account: wifEmail`, WIF provider placeholder), `init -backend-config=bucket=… -backend-config=prefix=…`, `plan -out tfplan`, `apply tfplan`.
- [ ] **Step 2: Module** `examples/terraform/bucket`: `variable "bucket_name"`, `variable "region"`, `resource "terraform_data" "bucket"` with `input = {name, region}`; no providers. Verify: `terraform -chdir=examples/terraform/bucket init -backend=false && terraform -chdir=examples/terraform/bucket validate` → `Success!`.
- [ ] **Step 3: `tf-module-push [version]`** (default `1.0.0`): repo `ghcr.io/pitoniak32/tf-bucket`; if `oras manifest fetch "$ref" >/dev/null 2>&1` → echo `exists`, exit 0; else `tar -czf` module dir and `oras push "$ref" --artifact-type application/vnd.example.tf.module.v1 bucket.tgz:application/vnd.example.tf.module.v1.tar+gzip` (default empty config, single manifest). Login via `GITHUB_PAT` from fnox. `#MISE description=` header like other tasks. Run twice → second prints `exists` (only if user opts to push; else `bash -n` check).
- [ ] **Step 4: Demo data** mirroring fixtures: `us-dev-tf` (dev), `us-stg-tf` (staging), `infra` app dev → us-dev-tf, staging → us-stg-tf, sample `env` with `TF_VAR_bucket_name`. `mise run lint payments` → clean; `mise run render /tmp/x` (scratchpad) → `apps/payments/infra/dev/us-dev-tf/terraform.yaml` exists.
- [ ] **Step 5:** `jj commit -m "feat: terraform example workflow, module, demo app"`.

### Task 8: Docs

**Files:**
- Create: `docs/adr/0021-terraform-apps.md`
- Modify: `GLOSSARY.md`, `README.md`, `platform/render/README.md`, `docs/gotchas.md`, `CLAUDE.md` (layout line: `targets` platform, terraform apps)

- [ ] **Step 1:** ADR 0021 from spec (Decisions, Considered Options: chart subscription, separate file kind, `vars`→tfvars, plan-gated apply, shard; Consequences incl. kargo-restore re-applies, Healthy-after-success, no platform switch). Status accepted; extends 0016, 0020.
- [ ] **Step 2:** GLOSSARY: **Terraform app**, **Terraform target**, **Module** (_Avoid_: stack, workspace); edit **App**/**Target** to mention both kinds; env-key wording.
- [ ] **Step 3:** gotchas: OCI module discovery pitfalls (no `platform`, no index, no non-JSON config → whole repo discovery fails).
- [ ] **Step 4:** `mise run test` → PASS (docs untouched by tests; sanity).
- [ ] **Step 5:** `jj commit -m "docs: ADR 0021 terraform apps"`.
