# GHA deployer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Target Stages can deploy via a dispatched GitHub Actions workflow instead of Argo CD, chosen per app/stage/target.

**Architecture:** New layered `#Layer` fields `deployWith` + `workflow` resolve per deploy in `app.cue`. `delivery.cue` drops Argo CD params/destinations for GHA deploys and swaps the Stage's final `argocd-update` for two Kargo `http` steps (dispatch with `return_run_details`, then poll the run). `gcd creds` adds a shared generic secret with the PAT; an example `workflow_dispatch` workflow logs what it would deploy.

**Tech Stack:** CUE (`platform/render`), Kargo 1.12.5 `http` step + `sharedSecret()`, Go (`platform/cli`), GitHub Actions, bash/yq golden tests.

**Spec:** `docs/superpowers/specs/2026-11-09-gha-deployer-design.md`

## Global Constraints

- Field name is **`deployWith`** (not `deployer`: GLOSSARY lists "deployer" as an avoided synonym for Promoter). Values `argocd` (default) | `gha`. Stage label `example.com/deploy-with`.
- `workflow` defaults: `repo: pitoniak32/gitops-cd`, `file: deploy.yaml`, `ref: main`. Resolved per field: target > stage > app > default.
- Teams never write CUE; every user-facing error via `error("...")`.
- `rendered/` never edited by hand; golden updated only via `UPDATE=1 mise run test` and the diff reviewed.
- Secret: `kargo-shared-resources/github-actions`, label `kargo.akuity.io/cred-type: generic`, key `token`, from `fnox get GITHUB_PAT`. Never logged.
- Kargo `http`: `headers` is a list of `{name, value}`; `successExpression`/`failureExpression`/`errorExpression` are NOT wrapped in `${{ }}`; JSON body via `${{ quote({...}) }}`; wait step `retry: {timeout: 30m}`.
- GitHub API: `X-GitHub-Api-Version: 2022-11-28`; dispatch 200 body has `workflow_run_id`, `html_url`.
- VCS is jj: `jj commit -m ...`; no git mutations, no pushes. Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- Workflow file missing / lacks `workflow_dispatch` / wrong ref → dispatch returns 404/422: promotion must fail terminally with GitHub's `message`, not hang. (Task 2: `errorExpression` + `failureExpression` assertions.)
- Empty/missing PAT → `sharedSecret` yields `{}` → 401: same terminal failure; `gcd creds` refuses an empty `GITHUB_PAT`. (Task 3 test.)
- Run ends `cancelled`/`timed_out`/`skipped` → Stage fails; still `queued`/`in_progress` → Running. (Task 2: exact expressions asserted.)
- `workflow` set at app level while every target is argocd → no error, no effect. (Task 1 lint-clean assertion.)
- `kargo-restore` on a GHA target re-promotes, which re-dispatches the workflow (acceptable for demo); restore must still find `vars` by name. (Task 2: `vars` names `repo`, `src`, `branch`, `image` unchanged and first; new vars appended after.)

---

### Task 1: Schema + resolution (`deployWith`, `workflow`)

**Files:**
- Modify: `platform/render/app.cue` (`#Layer`, `_defaults`, `deploys`)
- Modify: `platform/render/pipeline.cue` (target entry)
- Modify: `platform/render/testdata/lint-cases.tsv`
- Modify: `platform/render/scripts/test.sh` (new assertions block)
- Regenerate: `platform/render/schemas/*.json`, `platform/render/testdata/golden/pipelines/*.json`

**Interfaces:**
- Produces: `deploys["<app>/<target>"].deployWith: "argocd" | "gha"` and `.workflow: {repo: string, file: string, ref: string}` (always concrete); `pipelines[app].stages[s].targets[t].deployWith`.
- Produces: `_ghRepo: "pitoniak32/gitops-cd"` derived from `_repoURL` (strip `https://github.com/`).

- [ ] **Step 1: Add failing lint cases** to `lint-cases.tsv` (TAB-separated):

```
capabilities/payments/apps/worker/app.yml	.deployWith = "jenkins"	^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: .*deployWith: expected argocd or gha
capabilities/payments/apps/worker/app.yml	.workflow.repo = "not-a-repo"	^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: .*workflow\.repo: expected <owner>/<name>
capabilities/payments/apps/worker/app.yml	.stages.dev.workflow.file = "deploy.sh"	^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: .*workflow\.file: expected a \.yml or \.yaml file name
```

- [ ] **Step 2: Add resolution assertions** to `test.sh` after "kind cluster ok" (they use a synthetic copy so golden fixtures stay for Task 2):

```bash
# deployWith/workflow resolve per field, target > stage > app > default (Task 1)
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments/apps/my-app/app.yml"
yq -i '.workflow.ref = "release" | .stages.dev.deployWith = "gha" | .stages.dev.targets.onprem-dev = {"deployWith": "argocd", "workflow": {"file": "x.yml"}} | .stages.dev.targets.us-dev.workflow.repo = "acme/deploys"' "$c"
scripts/lint.sh "$r" payments >/dev/null || { echo "app-level workflow with argocd targets must lint clean" >&2; exit 1; }
scripts/render.sh "$r" payments "$o" >/dev/null
pt() { jq -r ".\"my-app\".stages.$1.targets.\"$2\".deployWith" "$o/pipelines/payments.json"; }
[[ "$(pt dev us-dev) $(pt dev onprem-dev) $(pt prod us-prod)" == "gha argocd argocd" ]] || { echo "deployWith resolution: $(pt dev us-dev) $(pt dev onprem-dev) $(pt prod us-prod)" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "deployWith resolution ok"
```

Workflow resolution (repo/file/ref) is asserted in Task 2 via the rendered Stage URL/body.

- [ ] **Step 3: Run, verify fail**

Run: `mise run test`
Expected: FAIL at the first new lint case (`lint case FAILED: ... deployWith = "jenkins"`).

- [ ] **Step 4: Implement in `app.cue`**

- `#Layer` gains:
  - `deployWith?: ("argocd" | "gha") | error("deployWith: expected argocd or gha")` (shape the disjunction so the message surfaces, like `#EnvValue`).
  - `workflow?: {repo?: =~"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$" | error("workflow.repo: expected <owner>/<name>"), file?: =~"^[A-Za-z0-9_.-]+\\.ya?ml$" | error("workflow.file: expected a .yml or .yaml file name"), ref?: string & !=""}`.
  - Comments: what each does; `workflow` only read where `deployWith` resolves to `gha`.
- `_defaults` gains `deployWith: "argocd"`, `workflow: {repo: _ghRepo, file: "deploy.yaml", ref: "main"}`.
- `deploys` entry gains `deployWith` and `workflow: {repo, file, ref}`, each via the same `[if t.x != _|_ {t.x}, if s.x ..., if a.x ..., _defaults.x][0]` pattern used for `cpu`.
- `pipeline.cue` target entry gains `deployWith: deploys["\(an)/\(tn)"].deployWith`.

- [ ] **Step 5: Regenerate schemas + golden, run tests**

Run: `mise run schemas && UPDATE=1 mise run test && mise run test`
Expected: `jj diff --stat` shows schema JSON (`deployWith`, `workflow` with descriptions), `golden/pipelines/*.json` (each target gains `"deployWith": "argocd"`), and nothing else under `golden/`. Final `mise run test` passes incl. `deployWith resolution ok` and 3 new `lint case ok`.

- [ ] **Step 6: Commit**

`jj commit -m "render: deployWith + workflow layered fields"`

---

### Task 2: Kargo GHA promotion + Argo CD filtering

**Files:**
- Modify: `platform/render/delivery.cue` (target Stage, `_deployRegion`, `_appProject`, `argocdDeploys`)
- Modify: `platform/render/testdata/root/capabilities/payments/apps/worker/app.yml` (`stages.dev.deployWith: gha`)
- Modify: `platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml` (`stages.staging.targets.onprem-stg.deployWith: gha`, `.workflow: {repo: acme/deploys, file: onprem.yml, ref: v1}`)
- Modify: `platform/render/scripts/test.sh` (update existing argocd file-list + onprem AppProject assertions; add GHA block)
- Regenerate: `platform/render/testdata/golden/**`

**Interfaces:**
- Consumes: `deploys[...].deployWith`, `deploys[...].workflow` (Task 1).
- Produces: Stage label `example.com/deploy-with`; Stage steps `dispatch` (outputs `runID`, `runURL`) and `wait`; vars appended after existing four: `workflowRepo`, `workflowFile`, `workflowRef`.

- [ ] **Step 1: Edit fixtures** (two `app.yml` changes above).

- [ ] **Step 2: Update/add assertions in `test.sh`**

- Existing Argo CD file-list assertion: remove `./deploys/us/payments/worker-us-dev.json` and `./deploys/onprem/payments/my-app-onprem-stg.json`.
- Existing onprem AppProject destinations: becomes `in-cluster/payments-onprem-dev in-cluster/payments-onprem-prod`.
- `stages_of`, graph `want` lines, promote roles: unchanged (deployWith doesn't alter the graph).
- New block:

```bash
# GHA targets (deployWith: gha): env branch written, then dispatch + wait instead of argocd-update
st() { yq "select(.kind==\"Stage\" and .metadata.name==\"$1\") | $2" "$k"; }
[[ "$(st worker-us-dev '[.spec.promotionTemplate.spec.steps[].uses] | join(",")')" == "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" ]] || { echo "worker-us-dev steps" >&2; exit 1; }
[[ "$(st worker-us-stg '.spec.promotionTemplate.spec.steps[-1].uses')" == "argocd-update" ]] || { echo "worker-us-stg must stay argocd" >&2; exit 1; }
[[ "$(st worker-us-dev '.metadata.labels."example.com/deploy-with"') $(st worker-us-stg '.metadata.labels."example.com/deploy-with"')" == "gha argocd" ]] || { echo "deploy-with label" >&2; exit 1; }
[[ "$(st worker-us-dev '[.spec.promotionTemplate.spec.vars[].name] | join(",")')" == "repo,src,branch,image,workflowRepo,workflowFile,workflowRef" ]] || { echo "vars order (kargo-restore reads by name)" >&2; exit 1; }
[[ "$(st worker-us-dev '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "pitoniak32/gitops-cd deploy.yaml main" ]] || { echo "default workflow" >&2; exit 1; }
[[ "$(st my-app-onprem-stg '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "acme/deploys onprem.yml v1" ]] || { echo "target workflow override" >&2; exit 1; }
d1='.spec.promotionTemplate.spec.steps[] | select(.as == "dispatch")'
[[ "$(st worker-us-dev "$d1 | .config.method + \" \" + .config.url")" == 'POST https://api.github.com/repos/${{ vars.workflowRepo }}/actions/workflows/${{ vars.workflowFile }}/dispatches' ]] || { echo "dispatch url" >&2; exit 1; }
[[ "$(st worker-us-dev "$d1 | .config.headers[] | select(.name == \"Authorization\") | .value")" == "Bearer \${{ sharedSecret('github-actions').token }}" ]] || { echo "auth header" >&2; exit 1; }
[[ "$(st worker-us-dev "$d1 | .config.successExpression + \" | \" + .config.failureExpression + \" | \" + .config.errorExpression")" == "response.status == 200 | response.status != 200 | response.body.message" ]] || { echo "dispatch expressions" >&2; exit 1; }
[[ "$(st worker-us-dev "$d1 | [.config.outputs[] | .name + \"=\" + .fromExpression] | join(\",\")")" == "runID=response.body.workflow_run_id,runURL=response.body.html_url" ]] || { echo "dispatch outputs" >&2; exit 1; }
for f in "'ref': vars.workflowRef" "'return_run_details': true" "'commit': outputs.commit.commit" "'target': 'us-dev'" "imageFrom(vars.image).Digest"; do
  st worker-us-dev "$d1 | .config.body" | grep -qF "$f" || { echo "dispatch body missing $f" >&2; exit 1; }
done
w1='.spec.promotionTemplate.spec.steps[] | select(.as == "wait")'
[[ "$(st worker-us-dev "$w1 | .retry.timeout + \" \" + .config.url")" == '30m https://api.github.com/repos/${{ vars.workflowRepo }}/actions/runs/${{ outputs.dispatch.runID }}' ]] || { echo "wait step" >&2; exit 1; }
[[ "$(st worker-us-dev "$w1 | .config.successExpression")" == "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'" ]] || { echo "wait success" >&2; exit 1; }
[[ "$(st worker-us-dev "$w1 | .config.failureExpression")" == "response.status != 200 || (response.body.status == 'completed' && response.body.conclusion != 'success')" ]] || { echo "wait failure" >&2; exit 1; }
echo "gha targets ok"
```

- [ ] **Step 3: Run, verify fail**

Run: `mise run test`
Expected: FAIL at the golden diff (fixtures now say gha, renderer still emits argocd params for `worker-us-dev`/`my-app-onprem-stg`). Also run `UPDATE=1 mise run test; mise run test` once and confirm it fails at `argocd output must be per region` (renderer not changed yet), then `jj restore platform/render/testdata/golden` before Step 4.

- [ ] **Step 4: Implement in `delivery.cue`**

- `_argoDeploys: {for k, d in deploys if d.deployWith == "argocd" {(k): d}}`; use it in `_deployRegion`, `_appProject` destinations and `argocdDeploys` instead of `deploys`.
- Target Stage: labels add `"example.com/deploy-with": deploys["\(an)/\(tn)"].deployWith`; vars append `workflowRepo`, `workflowFile`, `workflowRef` (only for gha deploys — the vars-order assertion is on a gha Stage; argocd Stages keep their four vars).
- Steps: common list through `git-push`, then `[argocd-update]` or `[dispatch, wait]` by `deployWith` (`list.Concat`).
- `dispatch`: `uses: http`, `as: dispatch`, `method: POST`, URL/headers/expressions/outputs exactly as the assertions; headers `Authorization`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`, `Content-Type: application/json`.
- `body`:

```
${{ quote({'ref': vars.workflowRef, 'return_run_details': true, 'inputs': {'capability': '<cap>', 'app': '<app>', 'stage': '<stage>', 'target': '<target>', 'commit': outputs.commit.commit, 'image': vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest}}) }}
```

- `wait`: `uses: http`, `as: wait`, `retry: timeout: "30m"`, `method: GET`, same headers minus `Content-Type`, expressions as asserted, `errorExpression: "'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url"`.
- Update the header comment block ("Promotion model") with one line on GHA targets.

- [ ] **Step 5: Regenerate golden, run tests**

Run: `UPDATE=1 mise run test && mise run test`
Expected: golden diff limited to `kargo/payments.yaml` (worker-us-dev, my-app-onprem-stg Stages + all target Stages gain the label), `argocd/deploys/{us/payments/worker-us-dev.json,onprem/payments/my-app-onprem-stg.json}` deleted, `argocd/projects/onprem/payments.yaml` loses one destination, `pipelines/payments.json` deployWith values. Final run prints `gha targets ok`, `kind cluster ok`, `control-flow graph ok`.

- [ ] **Step 6: Commit**

`jj commit -m "render: GHA target Stages dispatch + wait workflow, no Argo CD app"`

---

### Task 3: `gcd creds` writes the GHA token secret

**Files:**
- Modify: `platform/cli/internal/creds/creds.go`
- Modify: `platform/cli/internal/creds/creds_test.go`
- Modify: `platform/mise-tasks/creds` (`#MISE description`: "Kargo git credentials (GitHub App) + GitHub Actions token (PAT) from fnox, shared by all projects")

**Interfaces:**
- `Run(ctx, hub, get, log) error` signature unchanged; additionally reads `GITHUB_PAT`.

- [ ] **Step 1: Write failing tests**

- `TestCredsSecret`: add `"GITHUB_PAT": "sekret-pat-4"` to `vals`; assert secret `kargo-shared-resources/github-actions` exists with label `kargo.akuity.io/cred-type=generic`, `Data` keys exactly `token`, `token == "sekret-pat-4"`. The existing log-leak loop covers the PAT.
- `TestCredsIdempotent`: run `Run` twice on the same fake clientset; second returns nil and both secrets unchanged.
- `TestCredsMissingPAT`: all App values set, `GITHUB_PAT` empty → error containing `GITHUB_PAT is empty in fnox`.

- [ ] **Step 2: Run, verify fail**

Run: `cd platform/cli && go test ./internal/creds/`
Expected: FAIL (`github-actions` not found / no error for empty PAT).

- [ ] **Step 3: Implement**: add `GITHUB_PAT` to the fetched names (same empty check); `hub.UpsertSecret` a second Secret `github-actions`; log `secret kargo-shared-resources/github-actions applied in <ctx>`. Update package/`Run` doc comments.

- [ ] **Step 4: Run, verify pass**

Run: `mise run test`
Expected: PASS (gofmt, vet, `ok .../internal/creds`).

- [ ] **Step 5: Commit**

`jj commit -m "cli: creds writes github-actions token secret for GHA promotion"`

---

### Task 4: Example workflow, live config, docs

**Files:**
- Create: `.github/workflows/deploy.yaml`
- Modify: `capabilities/payments/apps/worker/app.yml` (`stages.dev.deployWith: gha`)
- Create: `docs/adr/0020-github-actions-deploy.md`
- Modify: `GLOSSARY.md` (new **Deploy with** entry after **Deploy**; `_Avoid_: deployer`)
- Modify: `docs/sync-boundaries.md`, `docs/gotchas.md`, `CLAUDE.md` (`creds` line: + GHA token), `README.md` (if it lists fnox secrets / Stage steps)
- Modify: `docs/superpowers/specs/2026-11-09-gha-deployer-design.md` (rename `deployer` → `deployWith`, label `example.com/deploy-with`; note why)

- [ ] **Step 1: Workflow** `.github/workflows/deploy.yaml`
- `name: deploy (example)`; `on: workflow_dispatch` with required string inputs `capability, app, stage, target, commit, image`.
- `run-name: "deploy ${{ inputs.capability }}/${{ inputs.app }} → ${{ inputs.target }} (${{ inputs.image }})"`.
- `permissions: contents: read`; one job `deploy`, `runs-on: ubuntu-latest`, `timeout-minutes: 10`.
- Steps: `actions/checkout@v4` with `ref: ${{ inputs.commit }}`; step that `cat`s `deployment.yaml` + `manifests.yaml` and asserts `deployment.yaml`'s image equals `inputs.image` (`yq` preinstalled on ubuntu-latest; fail with `::error::` otherwise); step writing `$GITHUB_STEP_SUMMARY` table (capability, app, stage, target, commit, image) + "demo: nothing deployed".
- Pass inputs via `env:`, never inline `${{ }}` in `run:` (script injection).

- [ ] **Step 2: Validate**

Run: `yq '.on.workflow_dispatch.inputs | keys | join(",")' .github/workflows/deploy.yaml`
Expected: `capability,app,stage,target,commit,image`. If `actionlint` is available (`mise x actionlint -- actionlint`), it reports nothing.

- [ ] **Step 3: Live config + lint**: set `stages.dev.deployWith: gha` in `capabilities/payments/apps/worker/app.yml`.
Run: `mise run lint payments && mise run render "$(mktemp -d)"`
Expected: no lint output; render succeeds (don't write to `rendered/`).

- [ ] **Step 4: Docs**
- ADR 0020 (format of 0016: Status, context, decision, Considered Options, Consequences): layered `deployWith`; OSS `http` (Enterprise `gha-*` steps rejected); env branch still written; PAT over App-token minting; `return_run_details` vs correlation-ID polling; consequences: switching to gha orphans the workload (`preserveResourcesOnDeletion: true`) — manual cleanup; switching back re-creates the Application from the current branch; `kargo-restore` re-dispatches; GitHub.com only (GHES ignores `return_run_details`).
- `sync-boundaries.md`: workflow file read from GitHub at dispatch (`ref`); PAT via `mise run creds`.
- `gotchas.md`: GHA dispatch 404/422 = missing workflow or no `workflow_dispatch` on `ref`; 401 = `github-actions` secret missing/expired PAT (`mise run creds`); PAT needs `Actions: read and write` on each workflow repo.

- [ ] **Step 5: Run full tests**

Run: `mise run test`
Expected: PASS.

- [ ] **Step 6: Commit**

`jj commit -m "gha deploy: example workflow, worker dev on GHA, ADR 0020"`

---

### Task 5: End-to-end (human: needs push)

Pushing is out of scope for the agent (CLAUDE.md). Hand these to the user:

- [ ] Push; CI renders `rendered/` (expect no `worker-us-dev.json`; Stage `worker-us-dev` with `http` steps).
- [ ] `mise run creds` (PAT with `Actions: read and write` on `pitoniak32/gitops-cd`).
- [ ] Kargo UI: assemble `worker` Freight, promote `worker-us-dev`. Expect: Promotion Running → Succeeded; Actions tab shows `deploy payments/worker → us-dev (...)` green; `env/payments/worker/us-dev` head == the run's checked-out SHA.
- [ ] Negative: `kubectl -n kargo-shared-resources patch secret github-actions -p '{"stringData":{"token":"bad"}}'`, promote again → Promotion Failed at `dispatch` with `Bad credentials`; `mise run creds` restores.
- [ ] Promote `worker-us-stg` → still Argo CD path, unaffected.
- [ ] Any new non-obvious failure → `docs/gotchas.md`.
