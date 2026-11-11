# Platform pre-deploy checks

Status: accepted. Amends ADR 0027 (a third ClusterPromotionTask; target Stage templates are no longer always one step). Reuses ADR 0020's dispatch and wait.

The platform team needs to gate prod deploys on checks it owns (change management, freeze windows), run as GitHub Actions workflows in each app's repo. Teams must not be able to remove or change them.

## Decision

- **Platform config, not team config.** `platform/render/prechecks.cue`: `preDeploy: <environmentType>: [{file, ref: *main, timeout: *30m}]`, initially `prd: [{file: "precheck.yaml"}]`. Keyed on the target's `environmentType`, so a stage that mixes npr and prd targets gates only its prd target Stages. `timeout` is whole minutes, 1m..120m.
- **App source repo.** `app.yml` gets a required `sourceRepo: <owner>/<name>` (top level, not layered). Every check is dispatched to it; there's no per-check repo.
- **ClusterPromotionTask `gha-check`**: the same `dispatch` and `wait` http steps as `promote-gha` (shared CUE definitions; `promote-gha`'s rendered steps are unchanged), with inputs from the Freight (`commit` = `commitFrom(vars.repo).ID`, nothing is pushed yet). Inputs: `capability, app, stage, target, commit` and `image` or `module`.
- **Stage steps**: `pre-1..N` (`gha-check`, with `workflowRepo/File/Ref` and `checkTimeout` set on the step itself) then `promote`. Step vars override Promotion vars only within that step, so a gha deploy's own workflow vars stay on `promote`. Gated Stages carry `example.com/pre-deploy: <sourceRepo>/<file>@<ref>[,…]`.
- **Timeout**: Kargo takes `retry` from the task's own steps (the referencing step's `retry` is ignored), so `gha-check`'s wait has a fixed `2h` cap, and its `failureExpression` also fails once `now() - date(response.body.created_at) > duration(vars.checkTimeout)` while the run hasn't completed.

## Considered Options

- **Team-configured `preDeploy` in `stages/<s>.yml`**: rejected; the checks are platform policy, and teams could drop them.
- **Split promote into composable tasks** (`git-promote`, `argocd-sync`, `gha-run`): a task can't reference another task (CRD rule), so composition would move into every Stage template. More moving parts for no gain today; the config interface doesn't change if it's done later.
- **A fixed `repo` per check**: not needed while every check runs in the app's repo; an optional `repo` can be added without breaking config.
- **Timeout buckets** (one task per `15m|30m|60m|120m`): the fallback if Kargo's expression env lacks `now`/`date`/`duration`.
- **Kargo verification (AnalysisTemplates)**: runs after the deploy; these checks must run before it. Post-deploy verification is separate work.

## Consequences

- Checks run once per target Stage: N apps × M prd targets dispatch N×M runs per check.
- Every Promotion of a gated Stage runs them, including rollbacks and `mise run kargo-restore`. A failing check blocks a rollback too.
- A failed check fails the Promotion before `git-clone`: no target branch commit, no Argo CD sync, no deploy workflow. A timed-out run keeps running on GitHub; Kargo just stops waiting.
- Each `sourceRepo` must contain the check file on `ref`, with `workflow_dispatch` declaring all the inputs (`image` and `module` optional); otherwise dispatch fails with 404/422. Not checked at render time.
- The shared PAT (`kargo-shared-resources/github-actions`) needs `Actions: read and write` on every `sourceRepo`.
- A change to `prechecks.cue` reaches every capability's next Promotion once CI renders it, like a task change (ADR 0027).
