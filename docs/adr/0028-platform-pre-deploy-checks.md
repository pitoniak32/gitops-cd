# Platform pre-deploy checks

The platform team gates deploys on checks it owns (change management, freeze windows). They run as GitHub Actions workflows in each app's repo, and teams can't remove or change them.

- **Platform config, not team config**: `PreDeploy` in `system/config.go`, `<environmentType>: [{file, ref: main, timeout: 30m}]`, today `prd: [{file: precheck.yaml}]`. It is keyed on the target's `environmentType`, so a stage that mixes npr and prd targets gates only its prd target Stages. `timeout` is whole minutes, 1m..120m.
- **App source repo**: `app.yml` has a required `sourceRepo: <owner>/<name>` (app level, not layered). Every check is dispatched there.
- **Stage steps**: `pre-1..N` (ClusterPromotionTask `gha-check`, ADR 0027, with `workflowRepo/File/Ref` and `checkTimeout` set on the step itself) and then `promote`. Inputs come from the Freight (`commit` is the rendered config commit, because nothing is pushed yet). Gated Stages carry `example.com/pre-deploy: <sourceRepo>/<file>@<ref>[,…]`.
- **Timeout**: Kargo takes `retry` from the task's own steps, so `gha-check`'s wait has a fixed `2h` cap. Its `failureExpression` also fails once the run's age exceeds `checkTimeout`. Kargo's http step only has `response` in scope, so the timeout is templated in with `${{ }}`.

## Considered Options

- **Team-configured checks in `stages/<s>.yml`**: teams could drop them.
- **A fixed `repo` per check**: not needed while every check runs in the app's repo. It can be added later without breaking config.
- **Kargo verification (AnalysisTemplates)**: that runs after the deploy, and these checks must run before it.

## Consequences

- Checks run once per target Stage: N apps × M prd targets means N×M runs per check.
- Every Promotion of a gated Stage runs them, including rollbacks and `kargo-restore`. A failing check blocks a rollback too.
- A failed check fails the Promotion before `git-clone`: no branch commit, no sync, no deploy. A timed-out run keeps running on GitHub.
- Each `sourceRepo` needs the check file on `ref`, with `workflow_dispatch` declaring all inputs. This isn't checked at render time. The shared PAT needs `Actions: read and write` on every `sourceRepo`.
