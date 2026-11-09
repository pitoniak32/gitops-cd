# Control-flow Stages only where they add value

Date: 2026-11-09. Status: approved design, pre-plan.
Visual: https://claude.ai/artifact/WfhbyUtR3kXPkGhgCRaRgY (current vs proposed graphs).

## Goal

Today every gitops stage gets an entry gate `<app>-<stage>` (control-flow Kargo Stage, ADR 0012), even
where nothing fans in or out. Emit control flow only where it prevents crossing many-to-many edges;
connect targets directly everywhere else.

## Rule

For each app, group downstream targets by their **upstream set** U = the targets of the stages they depend
on (app-resolved `dependsOn`, as today; first stage: U = the Warehouse). Consumers C(U) = every downstream
target whose stage has exactly that dependency set.

- `|U| == 1` (incl. Warehouse) or `|C(U)| == 1` → direct: each consumer requests Freight from every node in
  U (`availabilityStrategy: All` when `|U| > 1`; Warehouse → `direct: true`). Covers 1→1, 1→N, M→1.
- `|U| > 1` and `|C(U)| > 1` → one control-flow Stage (no `promotionTemplate`) requesting from all of U with
  `availabilityStrategy: All`; every consumer requests from it alone.
  - Downstream stages sharing the same dependency set share the node (fan-out, e.g. staging + perf after dev).
  - A stage depending on several stages gets one node (fan-in, e.g. prod after staging + perf).

## Naming

- Consumers all in one downstream stage D → `<app>-<D>` (fan-in into D).
- Otherwise, dependency set is one stage S → `<app>-<S>-done` (fan-out after S).
- Otherwise (several upstream stages, several downstream stages sharing that set) →
  `<app>-<S1>-<S2>…-done` (sorted).
- Lint: a target may not be named like a stage (existing) or `<stage>-done` (new), so control-flow names
  never collide with target Stage names `<app>-<target>`.

## Behavior

- Control-flow Stages always auto-promote (ProjectConfig promotion policy per node). Manual control stays
  on targets; a stage's `autoPromote` now covers only its targets. Prod rule unchanged.
- Labels: control-flow `example.com/kind: control-flow`; `example.com/stage`: D for fan-in, S (first, sorted)
  for fan-out. Color: that stage's color. Break-glass annotation on every Stage, as today.
- Promote RBAC `promote-<app>`: target Stages + control-flow Stages of the app (resourceNames).
- Unchanged: Warehouses, target Stage promotion steps/branches/shards, Argo CD apps, `pipelines/<cap>.json`,
  `kargo-restore` (targets only).

## Examples (golden fixtures)

- worker `dev[us-dev] → staging[us-stg] → prod[us-prod]`: no control flow; us-stg ← us-dev, us-prod ← us-stg.
- search/api `dev[us-dev] → prod[us-prod, onprem-prod]`: no control flow; both prod targets ← api-us-dev.
- my-app `dev[2] → staging[2] + perf[1] → prod[2]`: `my-app-dev-done` ← {us-dev, onprem-dev} All;
  us-stg, onprem-stg, us-perf ← my-app-dev-done; `my-app-prod` ← {us-stg, onprem-stg, us-perf} All;
  us-prod, onprem-prod ← my-app-prod. dev targets ← Warehouse direct.

## Migration

Argo CD prunes the old gate Stages and updates target sources on the next render. Gate Freight history is
lost (control flow only); target history stays. Freight already in flight may need a manual promote once.

## Testing

- `test.sh` assertions replace the gate ones: exact Stage set per fixture app, sources per Stage (above),
  kind labels, auto-promotion policies include every control-flow Stage and no prod target, promote role
  resourceNames, no crossing: no target has >1 source when its stage has >1 target and its upstream set >1.
- Lint cases: target named `<stage>-done` rejected with a `file:line:col` message.
- Golden updated (`UPDATE=1`).
- Live check after push: Kargo UI graph matches the visual; a dev → prod promotion of my-app works.

## Docs

ADR 0019 (supersedes ADR 0012's gate-per-stage graph; keeps target-per-Stage). Update README (promotion
flow paragraph, "promote onward from a gate"), GLOSSARY (Stage, control flow), docs/sync-boundaries.md,
docs/operations.md (kargo-restore note), docs/break-glass.md, delivery.cue comments.
