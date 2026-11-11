# Pre-deploy Checks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Platform-configured GitHub Actions checks, dispatched to each app's `sourceRepo` and awaited, gate every Promotion to a `prd` target before anything is pushed.

**Architecture:** A required `sourceRepo` in `#App`. Platform config `preDeploy: [envType]: [...]` in `platform/render/prechecks.cue`. A new `gha-check` ClusterPromotionTask built from the same CUE dispatch/wait definitions as `promote-gha`. Each gated target Stage's `promotionTemplate.steps` = `pre-1..N` (`gha-check`, per-step vars) + the existing `promote` step.

**Tech Stack:** CUE (`platform/render`, package `render`), Go tests (`platform/cli/internal/{render,lint}`), Kargo 1.12.5, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-11-pre-deploy-checks-design.md`

## Global Constraints

- Teams write no CUE (ADR 0006). Their only new field is `sourceRepo` in `app.yml`.
- `sourceRepo`: required, top-level `#App` only (not `#Layer`), `=~"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$" | error("expected <owner>/<name>")`.
- Check: `{file!, ref: *"main", timeout: *"30m"}`. `file` =~ `^[A-Za-z0-9_.-]+\.ya?ml$`. Timeout is whole minutes, 1..120. There is no `repo` field: checks always go to the app's `sourceRepo`.
- `preDeploy` is keyed on the **target's** `environmentType`. Initial config: `preDeploy: prd: [{file: "precheck.yaml"}]`.
- Wait hard cap `retry.timeout: "2h"`. Per-check timeout: `now() - date(response.body.created_at) > duration('${{ vars.checkTimeout }}')`.
- Dispatch inputs (exactly six): `capability, app, stage, target, commit` (= `commitFrom(vars.repo).ID`), and `image`/`module` via `(vars.artifactKey)`.
- Rendered `promote-gha` and `promote-argocd` stay byte-identical.
- VCS is jj: `jj commit -m ...`. No git mutations, no pushes. Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After every task: `mise run test` passes. Accept golden only via `UPDATE=1 mise run test`, after reading the diff.

## Review Focus

1. **A prd target on a gha deploy Stage:** the `pre-*` step vars `workflowRepo/File/Ref` must not leak into `promote` (that one must still dispatch `deploy.yaml`). Test in Task 3.
2. **A Stage that mixes npr and prd targets** (payments `my-app` has `dc1-npr` + `dc1-prd`-style targets): only the prd target Stages get `pre-*` steps. Test in Task 3.
3. **A terraform app on a prd target** (`infra` → `us-east4-prd-tf`): the check dispatch must send `module`, not `image`. Test in Task 2 (body uses `(vars.artifactKey)`) + Task 3 (`artifactKey` var set on the Stage).
4. **A missing `sourceRepo` on an app with no prd target:** still a lint error, at that `app.yml`. Test in Task 1.
5. **The Kargo expression `}}` cut-off** in the new dispatch body and timeout expression: `TestKargoExpressionsNotCutShort` must cover `_promotion-tasks.yaml`. Test in Task 2.

---

### Task 1: Required `sourceRepo` on apps

**Files:**
- Modify: `platform/render/app.cue` (`#App`, near `image?`)
- Modify: every `capabilities/*/apps/*/app.yml` (4) and `platform/render/testdata/root/capabilities/*/apps/*/app.yml` (6). Add `sourceRepo: pitoniak32/gitops-cd`, except testdata `payments/apps/my-app`, which gets `sourceRepo: acme/my-app` (used in Task 3).
- Modify: `platform/render/schemas/*` (regenerated)
- Test: `platform/cli/internal/lint/cases_test.go`

**Interfaces:**
- Produces: `apps[<app>].sourceRepo: string` (always set after validation)

- [ ] **Step 1: Add lint cases** to `lintCases` and bump the row-count guard `64` → `66`:
  - `{"del(.sourceRepo)", "capabilities/payments/apps/infra/app.yml", delAt("sourceRepo"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*sourceRepo`}`. `infra` is terraform. Covers Review Focus 4: the field is required regardless of targets.
  - `{".sourceRepo = \"nope\"", appW, setTo("nope", "sourceRepo"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: sourceRepo: expected <owner>/<name>$`}`
- [ ] **Step 2:** `cd platform/cli && go test ./internal/lint/ -run TestLintCases`. Expected: FAIL, field unknown or no error.
- [ ] **Step 3:** Add `sourceRepo!: =~"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$" | error("expected <owner>/<name>")` to `#App`, with a comment: GitHub repo of the app's code; pre-deploy checks are dispatched there. If the missing-field message doesn't locate at the file, add a `_appFiles` check in the style of the image/module rule: `if v.sourceRepo == _|_ {apps: (an): sourceRepo: error("sourceRepo required: <owner>/<name> of the app's GitHub repo")}`.
- [ ] **Step 4:** Add `sourceRepo` to all 10 `app.yml` files (values above), then run `mise run schemas`.
- [ ] **Step 5:** `mise run test`. Expected: PASS. Golden doesn't change, because `sourceRepo` isn't rendered yet.
- [ ] **Step 6:** `mise run lint` on the real capabilities. Expected: no output.
- [ ] **Step 7:** `jj commit -m "feat(render): required app sourceRepo"`

### Task 2: `gha-check` ClusterPromotionTask + platform config

**Files:**
- Create: `platform/render/prechecks.cue`
- Modify: `platform/render/promotiontasks.cue`
- Test: `platform/cli/internal/render/gha_test.go`, `kargo_test.go`, golden `platform/render/testdata/golden/kargo/_promotion-tasks.yaml`

**Interfaces:**
- Produces:
  - `#PreDeployCheck` and `preDeploy: [#EnvironmentType]: [...#PreDeployCheck]` in package `render`. `prechecks.cue` is *not* added to `cueload.Platform(...)` lists. Capability loads get it via the package.
  - ClusterPromotionTask `gha-check`. Vars in this order: `repo, image, capability, app, stage, target, artifactKey, workflowRepo, workflowFile, workflowRef, checkTimeout`. Steps: `dispatch` (http POST), `wait` (http GET, `retry.timeout: 2h`).
  - CUE definitions `_ghaDispatch`/`_ghaWait`, reused by `promote-gha`.

- [ ] **Step 1: Write the failing test** `TestGhaCheckTask` in `gha_test.go`:
  - the task list is exactly `promote-argocd promote-gha gha-check`; update the expectation in `TestStagesSatisfyPromotionTasks` to match
  - `gha-check` var names equal the list above, and its steps' `uses` are `http,http`
  - dispatch `config.url` equals `promote-gha`'s dispatch url, and so do its headers
  - dispatch body contains `'commit': commitFrom(vars.repo).ID`, `'return_run_details': true`, and `(vars.artifactKey): vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest`, and does *not* contain `task.outputs.commit`
  - wait has `retry.timeout == "2h"`; its `failureExpression` contains `now() - date(response.body.created_at) > duration('${{ vars.checkTimeout }}')` and the full `promote-gha` wait failure expression
  - wait `errorExpression` mentions `vars.checkTimeout` in the timeout case
- [ ] **Step 2:** Make `TestKargoExpressionsNotCutShort` also scan `testdata/golden/kargo/_promotion-tasks.yaml` and the shared render's `PromotionTasksFile`, if `kargo/*.yaml` doesn't already match it.
- [ ] **Step 3:** `go test ./internal/render/ -run 'TestGhaCheckTask|TestStagesSatisfy'`. Expected: FAIL, no `gha-check`.
- [ ] **Step 4:** Implement:
  - `prechecks.cue` with the schema from Global Constraints. Timeout: `=~"^[0-9]+m$"` plus a range check `1..120` via `strconv.Atoi(strings.TrimSuffix(t, "m"))` → `error("timeout: whole minutes, 1m..120m")`. Config `preDeploy: prd: [{file: "precheck.yaml"}]`.
  - `promotiontasks.cue`:
    - `_ghaDispatch: {#inputs: string}`, the http step whose body is built from `#inputs`.
    - `_ghaWait: {#timeout: string, #extraFailure: *"" | string}`.
    - `promote-gha` uses them with today's values (`task.outputs.commit.commit`, `_ghaWaitTimeout`, no extra failure).
    - `gha-check` gets `#timeout: "2h"`, and its extra failure is the clause above, ORed in as `|| (response.status == 200 && <clause>)`. Its error expression for that case is `'run ' + string(response.body.id) + ' exceeded ' + vars.checkTimeout + ': ' + response.body.html_url`.
- [ ] **Step 5:** `UPDATE=1 mise run test`, then `jj diff`. Expected: the golden diff only *adds* the `gha-check` document; the `promote-*` docs are unchanged.
- [ ] **Step 6:** `mise run test`. Expected: PASS.
- [ ] **Step 7:** `jj commit -m "feat(render): gha-check ClusterPromotionTask"`

### Task 3: Wire checks into target Stages

**Files:**
- Modify: `platform/render/delivery.cue` (target Stage, the `steps:` line and annotations)
- Test: `platform/cli/internal/render/gha_test.go` (helpers `taskOf`/`taskSteps` currently assume one step), new `TestPreDeployChecks`, golden `kargo/payments.yaml`

**Interfaces:**
- Consumes: `preDeploy` (Task 2), `apps[an].sourceRepo` (Task 1), `deploys[...].tgt.environmentType`
- Produces: target Stage steps `[{as: "pre-<i+1>", task: {kind: ClusterPromotionTask, name: gha-check}, vars: [workflowRepo=sourceRepo, workflowFile, workflowRef, checkTimeout]}..., {as: "promote", ...}]`; annotation `example.com/pre-deploy: "<sourceRepo>/<file>@<ref>[,...]"`, only on gated target Stages

- [ ] **Step 1:** Change `taskOf` to resolve the step with `as: promote`, not "exactly one step". Add `stepsOf(ds, name) []any` returning the template steps.
- [ ] **Step 2: Write the failing test** `TestPreDeployChecks`. For every target Stage in payments + search:
  - target `prd` (look up the target's `environmentType` via the Stage's `example.com/target` label in the fixture target files) ⇔ the steps are `pre-1, promote`, and `pre-1.task.name == "gha-check"`
  - non-prd Stages: steps are `promote` only, and there's no `example.com/pre-deploy` annotation
  - `pre-1` step vars: `workflowRepo` = `acme/my-app` for `my-app-*` prd Stages and `pitoniak32/gitops-cd` for the others; `workflowFile=precheck.yaml`, `workflowRef=main`, `checkTimeout=30m`
  - prd Stage annotation `example.com/pre-deploy` = `<repo>/precheck.yaml@main`
  - gha-deploy prd Stage, if the fixture has one (otherwise synthesize via `synth` with `deployWith: gha` on a prd target): the Promotion-level `workflowFile` is still `deploy.yaml`, and the `pre-1` step-level one is `precheck.yaml` (Review Focus 1)
  - `infra` prd Stage: Promotion var `artifactKey == "module"` (Review Focus 3)
- [ ] **Step 3: Extend** `TestStagesSatisfyPromotionTasks`: for each step, every task var has a value from Promotion vars ∪ that step's own `vars` (mirrors Kargo `promotionTaskVarsToStepVars`).
- [ ] **Step 4:** `go test ./internal/render/ -run 'TestPreDeployChecks|TestStagesSatisfy'`. Expected: FAIL.
- [ ] **Step 5:** Implement in `delivery.cue`:
  - `steps: list.Concat([[for i, c in _checks {...}], [promote step]])`, with `let _checks = *preDeploy[d.tgt.environmentType] | []`. Use a guard such as `[if preDeploy[d.tgt.environmentType] != _|_ {preDeploy[...]}, []][0]`; watch for field shadowing.
  - Merge the annotation into `annotations` when `len(_checks) > 0`.
- [ ] **Step 6:** `UPDATE=1 mise run test`, then `jj diff`. Expected: only prd target Stages change (steps + annotation).
- [ ] **Step 7:** `mise run test` and `mise run render`. Expected: PASS; `rendered/kargo/*.yaml` prd Stages have `pre-1`.
- [ ] **Step 8:** `jj commit -m "feat(render): pre-deploy checks on prd target Stages"`

### Task 4: Example check workflow

**Files:**
- Create: `.github/workflows/precheck.yaml`

- [ ] **Step 1:** Write the workflow, modelled on `deploy.yaml`:
  - `workflow_dispatch` inputs: `capability, app, stage, target, commit` (required) and both `image` and `module` (`required: false`). Image apps send `image`, terraform apps send `module`, and GitHub rejects undeclared inputs with a 422.
  - `run-name: "precheck ${{ inputs.capability }}/${{ inputs.app }} → ${{ inputs.target }}"`
  - inputs passed through `env:` only
  - a step that fails with `::error::PRECHECK_FAIL is set` when `vars.PRECHECK_FAIL == 'true'`
  - a job summary listing the inputs
  - `timeout-minutes: 10`, `permissions: contents: read`
- [ ] **Step 2:** `actionlint .github/workflows/precheck.yaml` if it's in `.mise.toml`; otherwise `yq . .github/workflows/precheck.yaml`. Expected: no errors.
- [ ] **Step 3:** Amend the spec's input list: workflows declare `image` and `module` as optional. Then `jj commit -m "ci: example precheck workflow"`.

### Task 5: Docs

**Files:**
- Create: `docs/adr/0028-platform-pre-deploy-checks.md` (status accepted; decision = spec summary; considered options: team-configured `preDeploy` in stages, Approach A task split, fixed `repo` field, timeout buckets; consequences: rollbacks/restore re-run checks, a failing check blocks rollback, PAT must cover every `sourceRepo`; amends 0027, links 0020)
- Modify: `GLOSSARY.md` (pre-deploy check, source repo), `docs/gotchas.md` (422 undeclared inputs, 404 missing check file, PAT coverage, blocked rollback), `docs/operations.md` (live verification below + `PRECHECK_FAIL` toggle), `CLAUDE.md` (layout line: `prechecks` in `platform/render/`; `#App` `sourceRepo`), `platform/render/README.md` (prechecks section), `docs/adr/0027-*.md` (one "Amended by 0028" line)

- [ ] **Step 1:** Write the docs above, following the existing ADR format (see 0027).
- [ ] **Step 2:** `mise run test`. Expected: PASS.
- [ ] **Step 3:** `jj commit -m "docs: ADR 0028 pre-deploy checks"`

### Task 6: Live verification (human: needs a push to `main`)

Not runnable by the agent (no pushes). The user pushes, CI renders `rendered`, then on kind:

- [ ] `gh variable set PRECHECK_FAIL --body false`. Promote Freight to a prod target. Expected: `pre-1::dispatch` → run succeeds → `promote::*` steps run.
- [ ] `gh variable set PRECHECK_FAIL --body true`. Promote again. Expected: Promotion Errored at `pre-1::wait`; `git ls-remote origin env/<cap>/<app>/<target>` head unchanged.
- [ ] Temporarily set `timeout: "1m"` and make `precheck.yaml` sleep 120s, then push. Expected: fails at ~1m with "exceeded 1m". If the error says `now`/`date`/`duration` is unknown, switch to the spec's bucket fallback (new plan).
- [ ] Revert the temporary changes.
