# Control-flow Stages only where they add value

Status: accepted. Replaces ADR 0012's gate per stage; keeps one Kargo Stage per target. Amended by ADR 0029: names lose the `<app>-` prefix (`<D>`, `<S>-done`).

ADR 0012 put a gate `<app>-<stage>` (a control-flow Stage, no promotion steps) in front of every gitops stage. Where a stage has one target and one upstream target, that gate only added a node and, in prod, a click. Where several dev targets feed several staging and perf targets, the gates still left crossing many-to-many edges.

Now the renderer (`platform/render/graph.cue`) emits control flow only where it does a job. Per app, a stage's targets are grouped by the stage's dependency set. U is the upstream targets of that set (the Warehouse for a first stage), and C is all targets of the stages that share that set.

- **Direct** when U or C is a single node: 1→1, 1→N, and M→1 (the one consumer waits with `availabilityStrategy: All`).
- **Shared control-flow Stage** when U and C both have more than one node. It waits for All of U, and every consumer requests from it. Stages with the same dependency set share it (fan-out after dev into staging + perf). A stage depending on several stages gets one (fan-in into prod after staging + perf).
- **One click per stage**: Kargo's "Promote to downstream" promotes to a node's direct downstream Stages. Every stage with more than one target must be exactly one node's downstream set. If its feeder also feeds other stages, or is the Warehouse (which can't promote to several Stages at once), the stage gets an entry control-flow Stage.
- **Names**:
  - Fan-in or entry into stage D: `<app>-<D>`.
  - Fan-out after one stage S: `<app>-<S>-done`.
  - Several upstream stages shared by several downstream stages: `<app>-<S1>-<S2>…-done`.
  - Lint rejects targets named like a stage or `<stage>-done`.
- **Promotion policy**: control-flow Stages always auto-promote, because they only mean "upstream verified". A stage's `autoPromote` covers just its targets, and prod targets stay manual.
- **RBAC**: promoters get `promote` on targets and control-flow Stages.

The demo graphs:

- `worker`: Warehouse → us-dev → us-stg → us-prod, with no control flow.
- `search/api`: Warehouse → us-dev → us-prod, onprem-prod, with no control flow.
- `my-app`:
  - Warehouse → `my-app-dev` (entry) → us-dev, onprem-dev.
  - Both dev targets → `my-app-dev-done` (fan-out), which feeds `my-app-staging` (entry) → us-stg, onprem-stg, and also us-perf.
  - Staging and perf targets → `my-app-prod` (fan-in) → us-prod, onprem-prod.

## Considered Options

- **Gate per stage (ADR 0012).** Adds nodes and prod clicks for no reason, and keeps crossing edges between multi-target stages.
- **No control flow at all.** Many-to-many becomes M×N crossing edges (dev → staging + perf, staging + perf → prod).
- **Joins only, no entry Stages.** No crossing edges, but a shared fan-out node can't release one stage in a single click. "Promote to downstream" on `my-app-dev-done` would also promote perf.
- **Manual control-flow Stages that follow the stage's `autoPromote`.** Prod gets a "release" click before each target click, so 1 + N clicks again. Per-target control stays on the targets either way.

## Consequences

- Fewer Stages and edges; prod takes N manual promotions, down from 1 + N.
- Releasing a whole multi-target stage is "Promote to downstream" on its entry or fan-in node, or on its single feeder target when nothing else hangs off that target.
- The graph shape depends on target counts. Adding a second target to a stage can add an entry or shared node, and its name follows the rules above.
- Migration: on the next render, Argo CD prunes the old gate Stages and repoints targets. Freight history on removed gates is lost, while target Stages keep theirs. Freight already mid-pipeline may need one manual promote.
