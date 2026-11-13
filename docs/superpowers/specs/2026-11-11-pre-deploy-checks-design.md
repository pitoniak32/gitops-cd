# Platform pre-deploy checks (design)

Status: approved design, not implemented. Becomes ADR 0028 on implementation.

## Goal

The platform team can gate deploys to an environment type (initially `prd`) on one or more GitHub Actions workflows: Kargo dispatches each workflow, waits for its run, and fails the Promotion (nothing pushed, nothing deployed) unless every run succeeds. Typical use: change-management / freeze-window checks.

Teams don't configure the checks and write no CUE (ADR 0006); the checks are platform policy. The one team-facing addition is `sourceRepo` in `app.yml`: where the app's code lives, required; every pre-deploy check is dispatched to the app's own repo.

## Non-goals

- Post-deploy verification. That is Kargo Stage `spec.verification` (AnalysisTemplates), a separate piece of work.
- Per-system or per-stage check config.
- Splitting `promote-argocd`/`promote-gha` into composable tasks (Approach A). The config interface is independent of that, so it can move later without touching config.

## Kargo facts this relies on (verified, Kargo 1.12.5 source)

- A (Cluster)PromotionTask step can't reference another task (CRD rule `has(self.uses) && !has(self.task)`). Composition happens in the Stage's `promotionTemplate.steps`.
- A task step's own `vars` override Promotion vars (`promotionTaskVarsToStepVars`), so one task can be used several times per Stage with different values.
- Inflated steps are aliased `<taskAlias>::<stepAlias>`. Within a task, `task.outputs.<alias>` still works.
- `retry` comes from the task's own steps; a `retry` on the referencing step is ignored. So `retry.timeout` is fixed per task.

## Configuration

New file `platform/render/prechecks.cue` in package `render`, consumed by `delivery.cue` during system renders (resolving the check repo needs the system's `apps`, so the platform-only `--platform` load must not depend on it):

```cue
#PreDeployCheck: {
	file!:   =~"^[A-Za-z0-9_.-]+\\.ya?ml$"
	ref:     *"main" | string
	timeout: *"30m" | =~"^[0-9]+m$" // whole minutes, 1..120
}
preDeploy: [#EnvironmentType]: [...#PreDeployCheck]
preDeploy: prd: [{file: "precheck.yaml"}]
```

- No `repo`: every check is dispatched to the app's `sourceRepo` (below), so the same check reaches each team's repo. The demo apps set `sourceRepo` to this repo, so the demo check is `.github/workflows/precheck.yaml` here. A fixed central repo (optional `repo`) can be added later without breaking config.
- Keyed on the **target's** `environmentType`, not the stage name. A stage that mixes npr and prd targets gates only its prd target Stages.
- Timeout over 120m or not whole minutes fails the render (CUE `error(...)`).
- Changes reach every system on the next render (no per-stage rollout), like ClusterPromotionTask changes (ADR 0027).

## App source repo (team-facing)

New **required** field in `#App` (top level only, not `#Layer`: an app has one repo, not one per stage/target):

```yaml
# systems/payments/apps/api/app.yml
image: registry.example.com/payments/api
sourceRepo: example-org/payments-api   # required
```

- Validation: `sourceRepo!: =~"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$" | error("expected <owner>/<name>")`. Required for every app (image and terraform), whether or not it deploys to an environment type with checks: one rule, no surprise when an app first reaches prod. Missing ⇒ lint error at the app.yml.
- Migration (same change): add `sourceRepo: <this repo>` to every existing `systems/*/apps/*/app.yml` and `platform/render/testdata/root/.../app.yml`.
- `mise run schemas` regenerates `app.json`; lint cases cover a bad value.
- **Contract for teams** (documented in the README/gotchas, not enforced at render): the repo must contain every check file the platform lists for an environment type the app deploys to, on the check's `ref`, declaring the inputs below (`image` and `module` both optional). Missing file ⇒ dispatch 404 ⇒ that prod Promotion fails, with GitHub's message.
- Only used for pre-deploy checks here. Defaulting `workflow.repo` (gha deploys) to it is a possible follow-up, not in scope.

## Behaviour

- Runs on every **target Stage** (app × target) whose target's environment type has checks, before `git-clone`, in list order. N apps × M prd targets ⇒ N×M dispatches per check, each with its own `app`/`target`.
- Dispatch inputs (fixed set; GitHub returns 422 for undeclared inputs, so each check workflow must declare all of them as `workflow_dispatch` inputs; `image` and `module` both, optional, since an app sends one):
  - `system`, `app`, `stage`, `target`
  - `commit`: the Freight's rendered-branch commit (`commitFrom(vars.repo).ID`); nothing is pushed yet
  - `image`: `<repo>:<tag>@<digest>` (terraform apps: key `module`, same as `promote-gha` via `artifactKey`)
- Fails the Promotion on: a dispatch that isn't a 200, a non-200 poll other than 404, `completed` with a conclusion that isn't `success`, or the timeout.
- Timeout: the wait step has a fixed `retry.timeout: 2h`, which is the hard cap. Its `failureExpression` also fails once `now() - date(response.body.created_at) > duration('${{ vars.checkTimeout }}')`, giving each check its own timeout ≤ 2h with a single task.
  - **Risk:** requires Kargo's expression env to expose expr-lang `now`/`date`/`duration`. Verified in the live test. If unavailable, fallback: timeout buckets (`15m|30m|60m|120m`), one `gha-check-<t>` task each, and `timeout` must be one of them.
- Rollbacks, manual promotions and `mise run kargo-restore` re-run the checks. A failing check blocks a rollback; the way around it is fixing/overriding the check workflow, not Kargo.
- Auth: the shared PAT `kargo-shared-resources/github-actions` (ADR 0020). Its fine-grained grant must include every check repo (`Actions: read and write`). Documented, not enforced.

## Rendering

`platform/render/promotiontasks.cue`:

- Extract `promote-gha`'s two http steps into CUE definitions `_ghaDispatch` (parameterised by the inputs expression) and `_ghaWait` (parameterised by the failure expression and timeout).
- `promote-gha` = `_promoteSteps` + `_ghaDispatch` + `_ghaWait` (rendered output unchanged).
- New `ClusterPromotionTask` `gha-check`: `_ghaDispatch` (Freight-based inputs) + `_ghaWait` (2h cap + `checkTimeout` expression). Vars: `repo`, `image`, `system`, `app`, `stage`, `target`, `artifactKey`, `workflowRepo`, `workflowFile`, `workflowRef`, `checkTimeout`.
- All three tasks are in `rendered/kargo/_promotion-tasks.yaml`.

`platform/render/delivery.cue`, target Stage `promotionTemplate.spec`:

```cue
steps: list.Concat([
	[for i, c in preDeploy[d.tgt.environmentType] {
		as:   "pre-\(i+1)"
		task: {kind: "ClusterPromotionTask", name: "gha-check"}
		vars: [
			{name: "workflowRepo", value: apps[d.app].sourceRepo},
			{name: "workflowFile", value: c.file},
			{name: "workflowRef", value: c.ref},
			{name: "checkTimeout", value: c.timeout},
		]
	}],
	[{as: "promote", task: {kind: "ClusterPromotionTask", name: "promote-\(d.deployWith)"}}],
])
```

- Shared vars (`repo`, `image`, …) stay on `promotionTemplate.spec.vars`; `kargo-restore` is unaffected.
- On a gha deploy Stage, the step vars `workflowRepo/File/Ref` on `pre-*` override only within those steps. The Promotion-level gha deploy vars still apply to `promote`.
- Gated Stages get the annotation `example.com/pre-deploy: <sourceRepo>/<file>@<ref>[,…]`.

## Example workflow

`.github/workflows/precheck.yaml` declares the inputs (`image`, `module` optional), writes a job summary, and fails when the repo variable `PRECHECK_FAIL` is `true`, so a failing check can be shown without a code change (`gh variable set PRECHECK_FAIL --body true`).

## Testing

`mise run test`:

- Golden: `testdata` has a prd target, so its Stage gets `pre-1`, and `_promotion-tasks.yaml` gains `gha-check`. `promote-gha` output stays byte-identical.
- `TestStagesSatisfyPromotionTasks` extended: for each task step, Promotion vars ∪ step vars cover the task's vars.
- New assertion: only Stages with a `prd` target have `gha-check` steps; step count = `len(preDeploy.prd)`.
- `testdata`: one app sets a different `sourceRepo`; golden shows each app's `pre-1` `workflowRepo` = its own `sourceRepo`.
- Lint case: app.yml without `sourceRepo` ⇒ error at that file.
- CUE errors: timeout `0m`, `121m`, `1h`, missing `file`.

Live (kind, documented in `docs/operations.md`):

1. Promote to a prod target: `pre-1` dispatches `precheck.yaml`, run succeeds, deploy proceeds.
2. `PRECHECK_FAIL=true`: Promotion fails at `pre-1::wait`, the target branch is unchanged.
3. A temporary `timeout: 1m` with a workflow that sleeps 2m: fails at ~1m (proves `now/date/duration`, else switch to the bucket fallback).

## Docs

- ADR 0028 "Platform pre-deploy checks": amends 0027 (third task, Stage steps no longer always a single step), links 0020.
- `GLOSSARY.md`: "pre-deploy check".
- `docs/gotchas.md`: undeclared inputs → 422; missing check file in an app's `sourceRepo` → 404; PAT must cover every app's `sourceRepo`; a failing check blocks rollbacks.
- `GLOSSARY.md`: "source repo".
- `CLAUDE.md` layout line: `prechecks` in `platform/render/`.
- `platform/render/README.md`: prechecks section.
