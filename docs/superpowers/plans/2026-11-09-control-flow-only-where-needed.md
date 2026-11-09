# Control-flow Only Where Needed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render Kargo control-flow Stages only where they prevent crossing many-to-many edges or give a multi-target stage a one-click release point; connect targets directly elsewhere.

**Architecture:** New CUE file `platform/render/graph.cue` computes per app the Kargo node graph (control-flow + target nodes, each node's sources) from `pipelines`. `delivery.cue` renders Stages, ProjectConfig and promote RBAC from that graph instead of the gate-per-stage loops. Tests are the existing bash harness (`platform/render/scripts/test.sh`) on rendered fixtures plus synthetic temp-copy cases.

**Tech Stack:** CUE 0.17 (package `render`), bash/yq/jq tests, golden files.

**Spec:** `docs/superpowers/specs/2026-11-09-control-flow-only-where-needed-design.md`

## Global Constraints

- VCS jj: commit with `jj describe -m "<msg>" && jj new`. No git mutations, no push.
- Target Stage names `<app>-<target>`, promotion steps, branches `env/<cap>/<app>/<target>`, shards, Argo CD outputs, Warehouses, `pipelines/<cap>.json`: unchanged.
- Control-flow Stages: no `promotionTemplate`, no `spec.shard`, always in ProjectConfig `promotionPolicies` with `autoPromotionEnabled: true`; label `example.com/kind: control-flow`; targets keep `example.com/kind: target`.
- Names: fan-in/entry into stage D → `<app>-<D>`; fan-out after single stage S → `<app>-<S>-done`; several upstream stages shared by several downstream stages → `<app>-<S1>-<S2>…-done` (upstream stage names sorted, joined with `-`).
- A stage's `autoPromote` covers only its targets. Prod rule unchanged.
- Every Stage keeps the break-glass annotation and `kargo.akuity.io/color` of its `example.com/stage`.
- Promote role `promote-<app>` resourceNames = all of the app's Stage names (targets + control flow), sorted.
- Teams never write CUE; user-facing validation via `error("...")`. `cue fmt` clean.

## Review Focus

- App skipping a stage (dependsOn resolved through the skipped stage): graph must use the app-resolved `p.stages[sn].dependsOn`, never raw `stages[sn].dependsOn`. Pinned by synthetic case in Task 1 (app without perf → prod deps resolve to staging only).
- Upstream target feeding two groups (stage S in two different dependency sets): its downstream set spans groups, so a multi-target D fed by it alone must still get an entry. Pinned by synthetic case in Task 1.
- Single-app capability with one stage and one target: no control flow, target `direct: true` from the Warehouse. Pinned by fixture `search/api`.
- Name collisions: target named `<stage>-done`. Pinned by lint case in Task 2.
- Freight in flight during migration: no test (live); documented in ADR (Task 3).

---

### Task 1: Graph computation + rendering

**Files:**
- Create: `platform/render/graph.cue`
- Modify: `platform/render/delivery.cue` (remove `_gateName`, gate Stage loop, `_kargoStages`/`_autoStages` gate parts, gate wording in comments; target `requestedFreight.sources` from graph; add control-flow Stage loop; ProjectConfig policies; RBAC resourceNames)
- Modify: `platform/render/scripts/test.sh` (replace gate assertions lines ~38-49; add synthetic cases)
- Golden: `platform/render/testdata/golden/kargo/*.yaml`

**Interfaces:**
- Produces in package `render`: `_graph: [app=string]: [node=string]: #GraphNode` with
  `#GraphNode: {kind: "control-flow" | "target", stage: string, target?: string, direct: bool, sources: [...string], all: bool}`
  (`direct`: requests from the Warehouse; `sources`: upstream node names, sorted; `all`: `availabilityStrategy: All`, true iff `len(sources) > 1` or the node is a control-flow join of several sources).
- Produces `_kargoStages: [app]: [...string]` (all node names) and `_autoStages: [...string]` (every control-flow node + targets of `autoPromote` stages), same names as today so RBAC/ProjectConfig code reads them.

- [ ] **Step 1: Write failing assertions in `test.sh`** (replace the gate block; `$k` = rendered `kargo/payments.yaml`, `$s` = `kargo/search.yaml`). Helper: `src() { yq "select(.kind==\"Stage\" and .metadata.name==\"$1\") | .spec.requestedFreight[0].sources | ((.direct // false) | tostring) + \":\" + (.availabilityStrategy // \"\") + \":\" + ((.stages // []) | join(\",\"))" "$2"; }`. Assert exact values:
  - Stage set `my-app-*` (sorted) == `my-app-dev my-app-dev-done my-app-onprem-dev my-app-onprem-prod my-app-onprem-stg my-app-prod my-app-staging my-app-us-dev my-app-us-perf my-app-us-prod my-app-us-stg`
  - `src my-app-dev` == `true::`; `src my-app-us-dev` == `false::my-app-dev`; `src my-app-dev-done` == `false:All:my-app-onprem-dev,my-app-us-dev`; `src my-app-staging` == `false::my-app-dev-done`; `src my-app-us-stg` == `false::my-app-staging`; `src my-app-us-perf` == `false::my-app-dev-done`; `src my-app-prod` == `false:All:my-app-onprem-stg,my-app-us-perf,my-app-us-stg`; `src my-app-onprem-prod` == `false::my-app-prod`
  - worker Stage set == `worker-us-dev worker-us-prod worker-us-stg`; `src worker-us-dev` == `true::`; `src worker-us-stg` == `false::worker-us-dev`; `src worker-us-prod` == `false::worker-us-stg`
  - search: `api-us-dev` only Stage, `src api-us-dev "$s"` == `true::`
  - kinds: control-flow names `my-app-dev my-app-dev-done my-app-prod my-app-staging` have `example.com/kind=control-flow`, no `promotionTemplate`, no `spec.shard`; `my-app-dev-done` has `example.com/stage=dev`, `my-app-staging` → `staging`
  - ProjectConfig policy names (sorted) == `my-app-dev my-app-dev-done my-app-onprem-dev my-app-prod my-app-staging my-app-us-dev payments-us-dev worker-us-dev` (dev targets auto + all control flow; no prod target)
  - `promote-worker` resourceNames == `worker-us-dev,worker-us-prod,worker-us-stg`
  - break-glass annotation on every `my-app` Stage == `["carol@example.com"]` (existing check, keep)
  - shards: `my-app-dev=- my-app-dev-done=- … my-app-us-prod=us` (control flow `-`, targets their region) — write the full sorted string
  - Synthetic (temp copy of testdata/root, render payments):
    a) skipped stage — `del(.stages.perf)` in my-app app.yml: `my-app-dev-done` absent; `src my-app-staging` == `false:All:my-app-onprem-dev,my-app-us-dev` (fan-in into staging, sole consumer stage); `src my-app-prod` == `false:All:my-app-onprem-stg,my-app-us-stg`.
    b) 1→N — search api gets prod (add `capabilities/search/stages/prod.yml` `dependsOn: [dev]`, targets `us-prod`, `onprem-prod` copied from payments fixture, app.yml `.stages.prod.targets` for both): no control flow in search; `src api-us-prod` == `false::api-us-dev`, `src api-onprem-prod` == `false::api-us-dev`.
    c) upstream target in two groups + M→1 — worker app.yml gets `.stages.perf.targets.us-perf = {}` and `.stages.staging.targets.onprem-stg = {}`: `src worker-staging` == `false::worker-us-dev` (entry: worker-us-dev also feeds us-perf); `src worker-us-stg` == `false::worker-staging`; `src worker-us-perf` == `false::worker-us-dev`; `src worker-us-prod` == `false:All:worker-onprem-stg,worker-us-perf,worker-us-stg`.
    d) multi-upstream shared naming — add stage `qa` (`dependsOn: [staging, perf]`) and my-app `.stages.qa.targets` `us-qa`, `onprem-qa` (target dirs copied from us-stg/onprem-stg): `src my-app-perf-staging-done` == `false:All:my-app-onprem-stg,my-app-us-perf,my-app-us-stg`; `src my-app-prod` == `false::my-app-perf-staging-done`; `src my-app-qa` == `false::my-app-perf-staging-done`.
    e) one-click invariant on the base fixture: for every `example.com/stage` with >1 target Stage, some Stage name has exactly that target set as its children (children = Stages listing it in `sources.stages`).
- [ ] **Step 2: Run** `mise run test` → FAIL on the first new assertion (gate Stages still rendered).
- [ ] **Step 3: Implement `graph.cue`** per spec Rule/Naming/One-click (algorithm, since CUE comprehensions don't determine it):
  1. For each app `an`, stage `D`: `deps(D)` = `p.stages[D].dependsOn` (sorted); `key(D)` = deps joined `,` (`""` = first stage). Group stages by key.
  2. `U(key)` = Warehouse if key == `""`, else all targets of stages in deps (as target node names `<app>-<t>`). `C(key)` = all targets of stages in the group.
  3. Shared node for key iff `|U|>1 && |C|>1`: name per Naming (group single stage D → `<app>-<D>`; deps single S → `<app>-<S>-done`; else `<app>-<sorted deps joined ->-done`); sources = U, all = true; stage label = D (single-stage group) else first sorted dep.
  4. `feeder(D)`: shared node if present; else if `|U|==1` the single U node (Warehouse or target); else (M→1) D's single target requests from all U with All.
  5. Entry for D iff `len(targets(D))>1` and feeder is a single node F and (F is Warehouse, or F is a shared node whose group has >1 stage, or F is an upstream target whose base downstream set — targets of every stage whose deps contain F's stage and whose feeder is F — is not exactly targets(D)): node `<app>-<D>`, kind control-flow, stage D, sources [F] (or direct if Warehouse); D's targets then request from the entry.
  6. Targets: kind target, stage/target set, sources/direct per 4–5.
- [ ] **Step 4: Rewrite `delivery.cue`** to render one Stage per `_graph` node (control flow: labels/annotations, `requestedFreight: [{origin: Warehouse an, sources: direct or {stages, availabilityStrategy: "All" if all}}]`; targets: today's spec with sources from the graph), `_kargoStages`/`_autoStages` from the graph. Update top comment block (promotion model) to describe control flow only where needed.
- [ ] **Step 5: Run** `cue fmt ./...` in platform/render, `UPDATE=1 mise run test && mise run test` → all ok; inspect `jj diff --stat platform/render/testdata/golden` (kargo YAMLs only).
- [ ] **Step 6: Commit** `render: control-flow Stages only where needed (fan-in/out, one-click entry)`.

### Task 2: Lint target named `<stage>-done`

**Files:** Modify `platform/render/input.cue:65-69`; Modify `platform/render/scripts/test.sh` (new case beside "target/stage name collision").

- [ ] **Step 1: Test:** temp copy, `cp -R targets/onprem-dev targets/dev-done` in payments; `lint.sh` output must match `^capabilities/payments/targets/dev-done/target\.yml:[0-9]+:[0-9]+: .*target name dev-done is <stage>-done; Kargo control-flow stage names <app>-dev-done would collide`.
- [ ] **Step 2: Run** `mise run test` → FAIL.
- [ ] **Step 3: Implement** in `input.cue`, same raw-file-name loop as the stage-name rule: `if tn` ends with `-done` and `stages[strings.TrimSuffix(tn, "-done")] != _|_` → `targets: (tn): error("target name \(tn) is <stage>-done; Kargo control-flow stage names <app>-\(tn) would collide")`. Update the comment above (control-flow names).
- [ ] **Step 4: Run** `mise run test` → all ok; `cue fmt` clean; `mise run schemas && jj diff --stat platform/render/schemas` → no change.
- [ ] **Step 5: Commit** `render: lint target named <stage>-done`.

### Task 3: Docs

**Files:** Create `docs/adr/0019-control-flow-only-where-needed.md` (format like 0017/0018: Status supersedes ADR 0012's gate-per-stage graph, keeps target-per-Stage; decision = Rule/Naming/one-click/auto; Considered Options: gates everywhere (0012), no control flow at all (crossing M×N edges), joins without one-click entries (no stage-wide release), manual control-flow (extra clicks); Consequences: prod clicks, migration/in-flight Freight, Warehouse can't release to several Stages). Modify: ADR 0012 Status line (superseded in part by 0019), `README.md` flow step 4 ("gate"/"promote onward from a gate" → control-flow Stages + "Promote to downstream" on the stage's entry/fan-in node), `GLOSSARY.md` Stage entry (+ "Control-flow Stage" term), `docs/sync-boundaries.md:15`, `docs/operations.md:39`, `docs/break-glass.md:21`, `docs/gotchas.md:75` (gate → control-flow).

- [ ] **Step 1:** Write; `grep -rn '\bgate\b\|gates\b' --include='*.md' README.md GLOSSARY.md docs | grep -v -e docs/superpowers -e docs/adr/00` → only intentional mentions (e.g. "break-glass guard").
- [ ] **Step 2:** `mise run test` green.
- [ ] **Step 3: Commit** `docs: ADR 0019 control-flow Stages only where needed`.

### Task 4: Live check (after user push)

- [ ] **Step 1:** User pushes; CI renders. `mise run up` (or existing clusters: Argo CD syncs `rendered-kargo`).
- [ ] **Step 2:** `kubectl --context kind-kargo-demo -n kargo-payments get stages -o name | sort` → matches Task 1 Stage sets; no old gates (`my-app-perf`, `worker-dev`, `worker-staging`, `worker-prod`).
- [ ] **Step 3:** Assemble my-app Freight (kubectl, as in docs/operations.md), confirm auto flow: dev entry → dev targets → `my-app-dev-done` → `my-app-staging` + us-perf available; "Promote to downstream" on `my-app-staging` promotes exactly us-stg + onprem-stg (via Kargo UI or API as admin).
- [ ] **Step 4:** Record any surprise in `docs/gotchas.md`; commit fixes.
