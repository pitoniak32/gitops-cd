# Shared promotion steps as ClusterPromotionTasks

Target Stages differ only in a few values: repo, src, branch, artifact, pin file and key, app name, and workflow. So the steps live once, in three `ClusterPromotionTask`s rendered to `rendered/kargo/_promotion-tasks.yaml` (`platformfiles.go`), which the hub `rendered-kargo` app syncs:

- `promote-argocd`: clone, clear, copy, `yaml-update` pin, commit, push, `argocd-update`.
- `promote-gha`: the same through push, then dispatch + wait (ADR 0020).
- `gha-check`: dispatch + wait only, before anything is pushed (ADR 0028).

A target Stage's `promotionTemplate` is the optional pre-deploy check steps (alias = check name) and then `{as: promote, task: promote-<deployWith>}`, plus its vars: `repo`, `src`, `branch`, `image`, `system`, `app`, `stage`, `target`, `pinFile`, `pinKey`, `artifactKey` (`image` or `module`), and `workflowRepo`/`File`/`Ref` for `gha`.

- **Vars are set at the Promotion level.** Kargo lets Promotion vars satisfy a task's vars, so `kargo-restore` reads `repo`/`branch`/`image` from `promotionTemplate.spec.vars` by name. Task vars have no defaults, and a test checks that every Stage sets every var.
- **One task per deploy method, not one with `if:`.** Steps reference each other's outputs as `task.outputs.<alias>`, so every step stays unconditional.
- **One gha wait timeout, `60m`.** `retry.timeout` isn't an expression, and terraform applies need the longest.
- **The dispatch input key is computed**, `(vars.artifactKey): …`, so terraform sends `module` and image apps send `image`.
- **Cluster-scoped only.** There are no per-system overrides (ADR 0006). The `_` prefix keeps the file from colliding with a system.

## Considered Options

- **Steps copied into every Stage**: long, hard-to-diff Kargo files that hide what differs.
- **Composable tasks** (`git-promote`, `argocd-sync`, `gha-run`): a task can't reference another task, so the composition would move into every Stage.

## Consequences

- A task change reaches every system's next Promotion once it's rendered and synced. It isn't promoted stage by stage. Running Promotions keep the steps they were inflated with.
- `kargo-gitops-only` blocks kargo-api from editing tasks. Every controller, including the shards, can read them.
