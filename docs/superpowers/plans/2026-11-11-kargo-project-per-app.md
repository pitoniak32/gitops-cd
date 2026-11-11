# Kargo Project per App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render one Kargo project `kargo-<cap>-<app>` per app (unprefixed Stage names, per-app viewers), plus per-app `viewer-<app>` roles in the per-capability Argo CD AppProjects.

**Architecture:** In CUE (`platform/render/`), the per-capability `_kargoCore` becomes a per-app comprehension with the project name as a parameter. The graph drops the `<app>-` prefix. A new `_appEmails[app]` feeds Kargo `viewers` and the Argo CD roles. The Go side (`gcd`) gets a cross-capability check for duplicate project names, `kargo-restore` finds projects by label, and lint locates two new CUE errors.

**Tech Stack:** CUE (Go library, `gcd`), Go tests in `platform/cli`, Kargo 1.12, Argo CD.

**Spec:** `docs/adr/0029-kargo-project-per-app.md`

## Global Constraints

- Project name `kargo-<cap>-<app>`; max 63 chars (namespace).
- Stage names: target `<target>`; control-flow `<D>`, `<S>-done`, `<S1>-<S2>…-done`; max 63 still (`_maxStageName`).
- RBAC in each project: SA/Role/RoleBinding `promote-<stage>`, SA + RoleBinding `viewers` (Role `kargo-viewer`). `users-<cap>` + `kargo-users-<cap>` unchanged (`_allEmails`).
- Warehouse name stays `<app>`. All projects of a capability stay in `rendered/kargo/<cap>.yaml`.
- Namespace labels: `kargo.akuity.io/project: "true"`, `example.com/capability: <cap>`, `example.com/app: <app>`.
- Argo CD: AppProject `<cap>` per region (unchanged). Roles `viewer-<app>` only (cap-wide `viewer` role removed); one policy per Application `p, proj:<cap>:viewer-<app>, applications, get, <cap>/<cap>-<app>-<target>, allow`, sorted; `groups: _appEmails[app]`.
- `authorizedStage`: `kargo-<cap>-<app>:<target>`.
- Unchanged: Application names, `env/*` branches, Promotion vars, ClusterPromotionTasks.
- `_appEmails[app]` = sorted unique: `_promoters[app][*]` ∪ (`apps.<app>.viewers` if set else `viewers`) ∪ `_breakGlass`.
- VCS is jj; no pushes. Teams write no CUE; user-facing errors via `error("...")`.

## Review Focus

1. Control-flow Stage name equal to a target Stage name in the same app (stage named like a target, or `<S>-done` = a target): must be a lint error at the stage in `app.yml`, not a CUE conflict dump. → Task 1 test.
2. Two capabilities rendering the same project name (`pay` + `ments-api` vs `pay-ments` + `api`): `gcd render` must fail naming the project. → Task 4 test.
3. App whose `apps.<app>.viewers` is set: its project `viewers` and Argo CD role must exclude the capability-level viewers and other apps' promoters. → Task 2 + Task 3 tests.
4. An app with no argocd deploys in a region (gha-only, terraform, Cloud Run) must get no `viewer-<app>` role in that region's AppProject (empty policies would be a useless role). → Task 3 test.
5. `kargo-restore payments` after the rename must restore every app project of the capability, and report a missing capability by its label, not a hardcoded namespace. → Task 5 test.

---

### Task 1: Unprefixed Stage names + collision lint

**Files:**
- Modify: `platform/render/graph.cue` (all `"\(an)-…"` node names, `U`)
- Modify: `platform/render/delivery.cue:31` (`_tgtName` → `out: #target`, drop `#app`), `:245` (`_nodeStages` entry check `n == x.stage`), `:36-42` (stageNameErrors messages)
- Modify: `platform/cli/internal/lint/locate.go:93` (no change in path shape; reuse for collisions)
- Test: `platform/cli/internal/render/kargo_test.go` (`TestKindClusterKargo`, `TestControlFlowGraph`), `platform/cli/internal/lint/rules_test.go:285-315`

**Interfaces:**
- Produces: `_graph[app][<stageName>]` keyed by unprefixed names; `_tgtName & {#target: t}` → `t`; new `stageNameErrors.<app>.<stage>` collision error.

- [ ] **Step 1: Update tests to unprefixed names**
  `TestKindClusterKargo`: `stagesOf(k, "my-app")` == `"dc1-dev-k8s dc1-npr-k8s dc1-prd-k8s dev dev-done prod staging us-east4-dev-k8s us-east4-npr-k8s us-east4-npr-k8s-perf us-east4-prd-k8s"`; every `wantSrc` drops `my-app-`/`worker-`/`api-` (e.g. `wantSrc(t, k, "dev-done", "false:All:dc1-dev-k8s,us-east4-dev-k8s")`). Same for `TestControlFlowGraph`. Until Task 2 the stages of all apps share one project, so look them up via label `example.com/app` (`stagesOf` already does; `wantSrc`/`stage` need an app-scoped variant: add `inApp(ds, app) []map[string]any` filtering on `metadata.labels."example.com/app"` in `testutil_test.go`).
  rules_test long-name cases: control-flow max now from stage names alone (`longStage-done`), message `Kargo Stage name <n> is <len> characters (max 63): shorten the target name` / `…: shorten the stage names`.
- [ ] **Step 2: Add failing lint case `TestStageNameCollision`** in `rules_test.go`: payments fixture, add stage `us-east4-dev-k8s` (dependsOn so it gets a control-flow entry, i.e. >1 target) used by `my-app`; expect `^capabilities/payments/apps/my-app/app\.yml:[0-9]+:[0-9]+: .*Kargo Stage name us-east4-dev-k8s is used by both stage us-east4-dev-k8s and target us-east4-dev-k8s: rename the stage$`.
- [ ] **Step 3: Run** `cd platform/cli && go test ./internal/render/ ./internal/lint/` → FAIL.
- [ ] **Step 4: Implement**: graph names without prefix. Collision: in `delivery.cue` add to `stageNameErrors` `(an): (sn): error(...)` when a control-flow name ∈ that app's target names; compute from `_gp` (control-flow names) and `pipelines[an].stages[*].targets` (target names), **not** from `_graph` (the collision makes `_graph` itself conflict). Guard `_graph` node emission so a colliding control-flow node is skipped (so the only error is the lint message).
- [ ] **Step 5: Run** `go test ./internal/render/ ./internal/lint/` → PASS except golden; `UPDATE=1 go test ./internal/render/ -run TestGolden`; review golden diff = names only.
- [ ] **Step 6: Commit** `jj commit -m "feat(render): unprefixed Kargo Stage names"`

### Task 2: Kargo project per app

**Files:**
- Modify: `platform/render/delivery.cue` (`_kargoProject` → `_kargoProjectOf: {#app: string, out: "kargo-\(_cap)-\(#app)"}`; `_kargoCore` per app; `_rbacMeta` takes `#app`; `_kargoRBAC` names `promote-<stage>`; `_kargoViewers` per app; `authorizedStage`; `projectNameErrors`)
- Modify: `platform/render/owners.cue` (add `_appEmails`)
- Modify: `platform/cli/internal/lint/locate.go` (case `projectNameErrors`: `<app>` → `apps/<app>/app.yml`, top of file)
- Test: `kargo_test.go`, `rbac_test.go`, `gha_test.go`, `terraform_test.go`, `cloudrun_test.go`, `prechecks_test.go`, `lint/rules_test.go`; golden

**Interfaces:**
- Consumes: Task 1 names, `inApp` helper.
- Produces: `_appEmails: [app]: [...string]` (owners.cue) for Task 3; `_kargoProjectOf`; test helper `inProject(ds, ns) []map[string]any` (filter `metadata.namespace`, plus the Namespace/Project objects named `ns`).

- [ ] **Step 1: Failing tests**
  - `TestKindClusterKargo`: `kargo/payments.yaml` contains Namespaces `kargo-payments-infra`, `kargo-payments-my-app`, `kargo-payments-worker`, each with labels per Global Constraints, each followed by Project + ProjectConfig of the same name; `my-app` ProjectConfig `promotionPolicies` lists only my-app's auto Stages.
  - `TestKindClusterRBAC`: in `inProject(k, "kargo-payments-my-app")`: Role `promote-prod` resourceNames == unprefixed prod nodes; `claims(.., "viewers")` == my-app's `_appEmails`; fixture: give `my-app` an `apps.my-app.viewers` override in `testdata/root/capabilities/payments/owners.yml` and assert the capability viewer email is absent from `kargo-payments-my-app` viewers but present in `kargo-payments-worker` viewers. `users-payments` claims unchanged.
  - `argocd_test`: deploy json `authorizedStage` == `kargo-payments-my-app:us-east4-dev-k8s`.
  - Replace `find(k, …)` stage lookups in gha/terraform/cloudrun/prechecks tests with `inProject`-scoped lookups.
  - `rules_test`: `TestProjectNameTooLong`: app name making `kargo-payments-<app>` 64 chars → `^capabilities/payments/apps/<app>/app\.yml:1:1: .*Kargo project name kargo-payments-<app> is 64 characters \(max 63\): shorten the app name$`.
- [ ] **Step 2: Run** `go test ./internal/render/ ./internal/lint/` → FAIL.
- [ ] **Step 3: Implement** per Global Constraints. `projectNameErrors: {for an, _ in pipelines if len("kargo-\(_cap)-\(an)") > 63 {(an): error(...)}}`; add to the error sets `gcd lint`/`render` read (same place `stageNameErrors` is wired).
- [ ] **Step 4: Run** tests → PASS; `UPDATE=1` golden; review diff.
- [ ] **Step 5: Commit** `jj commit -m "feat(render): Kargo project per app"`

### Task 3: Argo CD per-app viewer roles

**Files:**
- Modify: `platform/render/delivery.cue` (`_appProject.spec.roles`)
- Test: `platform/cli/internal/render/argocd_test.go` (`TestKindClusterArgoCD` viewer block)

**Interfaces:**
- Consumes: `_appEmails` (Task 2), `_argoDeploys`.

- [ ] **Step 1: Failing test**: us-east4 `payments` AppProject roles == exactly `viewer-my-app`, `viewer-worker` (names sorted); `viewer-my-app.policies` == one line per my-app us-east4 argocd Application, e.g. `p, proj:payments:viewer-my-app, applications, get, payments/payments-my-app-us-east4-dev-k8s, allow`; `groups` == `_appEmails["my-app"]`; no role named `viewer`; no `viewer-infra` (terraform, no argocd deploy).
- [ ] **Step 2: Run** `go test ./internal/render/ -run TestKindClusterArgoCD` → FAIL.
- [ ] **Step 3: Implement**: roles = for each app with ≥1 `_argoDeploys` entry in `#region` (sorted).
- [ ] **Step 4: Run** → PASS; golden update.
- [ ] **Step 5: Commit** `jj commit -m "feat(render): per-app Argo CD viewer roles"`

### Task 4: Reject duplicate Kargo project names across capabilities

**Files:**
- Modify: `platform/cli/internal/render/render.go` (`UniqueApps`, ~line 130 call site)
- Test: `platform/cli/internal/render/render_test.go` (`TestUniqueApps` sibling)

- [ ] **Step 1: Failing test `TestUniqueKargoProjects`**: render `payments`; copy `kargo/payments.yaml` to `kargo/payments-dup.yaml` in out; `UniqueApps(out)` error contains `duplicate Kargo project kargo-payments-my-app`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**: in `UniqueApps`, also read `kargo/*.yaml` (skip `_`-prefixed), collect `kind: Project` names; report duplicates with both files.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `jj commit -m "feat(render): reject duplicate Kargo projects"`

### Task 5: kargo-restore finds projects by label

**Files:**
- Modify: `platform/cli/internal/kargo/restore.go:244-260`
- Test: `platform/cli/internal/kargo/restore_test.go`

- [ ] **Step 1: Failing tests**: `TestRestore` fixtures use namespaces `kargo-payments-my-app` + `kargo-payments-worker` labeled `example.com/capability: payments`, `example.com/app`; Stages unprefixed; output labels `payments/my-app/<target>`; missing cap message `other: no Kargo project labeled example.com/capability=other (not rendered/synced yet?), skipped`.
- [ ] **Step 2: Run** `go test ./internal/kargo/` → FAIL.
- [ ] **Step 3: Implement**: `Namespaces().List(ListOptions{LabelSelector: "example.com/capability=" + cap})`, sorted by name; loop stages per namespace; label from namespace's `example.com/app`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `jj commit -m "feat(cli): kargo-restore per-app projects"`

### Task 6: Docs

**Files:** `GLOSSARY.md` (Target stage, Control-flow Stage names; add **Kargo project**: one per app, `kargo-<cap>-<app>`), `README.md`, `CLAUDE.md` (`promote-<app>-<stage>` → `promote-<stage>` in `kargo-<cap>-<app>`), `platform/render/README.md`, `docs/operations.md`, `docs/break-glass.md`, `docs/gotchas.md`, `docs/sync-boundaries.md`, `platform/infrastructure/kargo/values.yaml` comments, `platform/appsets/kargo.yaml` comment; status lines in ADR 0007/0012/0016/0019/0023 (`amended by ADR 0029`); ADR 0029 `Status: accepted`.

- [ ] **Step 1:** `grep -rn 'kargo-<cap>\|kargo-payments\b\|<app>-<target>\|<app>-<stage>\|promote-<app>' --exclude-dir=superpowers docs README.md GLOSSARY.md CLAUDE.md platform` → update every hit.
- [ ] **Step 2: Verify** grep returns no stale hits (old plans/specs under `docs/superpowers/` stay).
- [ ] **Step 3: Commit** `jj commit -m "docs: Kargo project per app (ADR 0029)"`

### Task 7: Full verification

- [ ] `mise run test` → PASS. `mise run lint` → no errors. `mise run render` → `rendered/kargo/*.yaml` has one Project per app.
- [ ] After user pushes + CI renders: `rendered-kargo` prunes `kargo-payments`/`kargo-search`; clean `mise run up`, then `mise run kargo-restore`; check a promoter of one app sees only that app's project contents and can't create Freight in another app's project; Argo CD viewer sees only their app's Applications.
