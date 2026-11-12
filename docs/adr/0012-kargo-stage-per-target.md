# Kargo graph: one Stage per target, control flow only where needed

A gitops stage (`stages/<stage>.yml`) can deploy an app to several targets. Per app (one Kargo project, ADR 0029), every target is a Kargo **target Stage** `<target>`. It copies `rendered/apps/<cap>/<app>/<stage>/<target>/` onto `env/<cap>/<app>/<target>` and deploys that branch: Argo CD Application `<cap>-<app>-<target>`, or a workflow (ADR 0020). That matches Argo CD's one app per target and gives per-target control in prod (e.g. promote one region first). A Stage runs on one shard (ADR 0015), so a stage spanning regions needs a Stage per region anyway.

Control-flow Stages (no promotion steps) appear only where they do a job (`graph.go`). A stage's targets are grouped by the stage's dependency set. U is the upstream targets of that set (the Warehouse for a first stage), and C is all targets of the stages that share that set.

- **Direct** when U or C is a single node: 1→1, 1→N, and M→1 (the consumer waits with `availabilityStrategy: All`).
- **Shared control-flow Stage** when both U and C have more than one node. It waits for All of U, and every consumer requests from it.
- **One click per stage**: Kargo's "Promote to downstream" promotes a node's direct downstream Stages. A stage with several targets gets an **entry** control-flow Stage when its feeder also feeds other stages or is the Warehouse (which can't promote to several Stages at once).
- **Names**: fan-in or entry into stage D is `<D>`; fan-out after stage S is `<S>-done`; several upstream stages shared by several downstream stages is `<S1>-<S2>…-done`. Lint rejects a target named like a stage or `<stage>-done`.
- **Policy**: control-flow Stages always auto-promote, because they only mean "upstream verified". A stage's `autoPromote` covers its targets. A stage with an `environmentType: prd` target can't set `autoPromote`.

Example (`payments/my-app`): Warehouse → `dev` (entry) → both dev targets → `dev-done` (fan-out) → `staging` (entry, both npr targets) and the perf target → `prod` (fan-in) → both prd targets.

## Considered Options

- **One Stage per (app, stage) promoting all its targets**: a cleaner graph, but it can't span shards, and the Kargo UI hides targets.
- **A gate before every stage**: adds nodes and one more prod click, and still leaves crossing many-to-many edges.
- **No control flow**: M×N crossing edges (dev → staging + perf, staging + perf → prod).
- **Joins only, no entry Stages**: "Promote to downstream" on a fan-out node would also release sibling stages (e.g. perf with staging).
- **One branch per stage, each target writing its own subfolder**: concurrent target promotions race on push.

## Consequences

- Prod takes N manual promotions, one per target. To release a whole multi-target stage, use "Promote to downstream" on its entry or fan-in node.
- The graph shape depends on target counts. Adding a second target to a stage can add an entry or shared node.
- A target's Argo CD app shows sync `Unknown` until its first promotion creates its branch.
- Moving a target to another stage keeps its branch and history. Removing or renaming a target orphans its `env/*` branch (`docs/operations.md`).
