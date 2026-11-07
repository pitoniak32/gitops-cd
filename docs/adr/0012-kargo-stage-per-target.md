# One Kargo Stage per target, with a gate per stage

A gitops stage (`stages/<stage>.yml`) can deploy an app to several targets. Kargo used to have one Stage per (app, stage), which promoted all of that stage's targets in one go onto `env/<cap>/<app>/<stage>`. The Kargo UI never showed targets, so it didn't match the Argo CD apps (one per target).

Now, per app:

- **Gate** `<app>-<stage>`: a control-flow Stage (no `promotionTemplate`, so Freight is verified as soon as it arrives). The first stage's gate takes Freight directly from the Warehouse. Later gates take Freight once it's verified in **every** target stage of the upstream stage(s) (`availabilityStrategy: All`).
- **Target stage** `<app>-<target>`: takes Freight from its gate. It copies `rendered/apps/<cap>/<app>/<stage>/<target>/` onto `env/<cap>/<app>/<target>` and runs `argocd-update` on that one Application (`<cap>-<app>-<target>`, path `.`, authorized for that target stage).
- **autoPromote** on a stage applies to its gate and all of its targets. Prod (a stage with a prod target) stays manual on the gate and on each target.
- **Promoters** get `promote` on all of an app's gates and target stages.
- Lint rejects a target named like a stage, since `<app>-<name>` would collide.

## Considered Options

- **Flat target stages** (no gates; each target takes Freight from all upstream targets): rejected. It hides the gitops stage, and the graph gets N×M edges between stages.
- **Keep `env/<cap>/<app>/<stage>`, with each target stage writing only its own subfolder**: rejected. Concurrent target promotions would race on push to the shared branch.
- **Targets auto-promote from their gate** (the gate is the only manual step): not chosen. You'd lose per-target control in prod (e.g. promote one region first). This can be revisited as a per-stage option.

## Consequences

- Prod takes 1 + N manual promotions (the gate, then each target).
- Each target's Argo CD app shows sync `Unknown` until its first promotion, because its branch doesn't exist yet.
- The old `env/<cap>/<app>/<stage>` branches are orphaned and need deleting by hand.
- Moving a target to another stage keeps the branch name, so its history carries over.
