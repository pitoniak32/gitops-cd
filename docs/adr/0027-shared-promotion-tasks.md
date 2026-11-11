# Shared promotion steps as ClusterPromotionTasks

Status: accepted; amended by ADR 0028 (ClusterPromotionTask `gha-check`, `pre-<n>` steps before `promote`). Changes how ADR 0012, 0020, 0021 and 0024 target Stages spell their steps; what they do is unchanged.

Every target Stage carried its own copy of the promotion steps: clone, clear, copy, yaml-update, commit, push, then `argocd-update` or the gha dispatch and wait. Only a few values differed per Stage: repo/src/branch/image, the pin file and key (per platform), the commit message and Argo CD app name, and the workflow. The copies made `rendered/kargo/<cap>.yaml` long and hard to diff, and hid what actually differs between Stages.

## Decision

- **Two `ClusterPromotionTask`s, rendered by CUE** (`platform/render/promotiontasks.cue`): `promote-argocd` and `promote-gha`. Both are written once to `rendered/kargo/_promotion-tasks.yaml` by `gcd render` with the other platform-level files (`gcd render --platform`). The hub `rendered-kargo` app syncs them, and they are golden-tested like the rest of the render. The `_` prefix keeps the file from colliding with a capability: a capability name can't start a Kargo project name with `_`.
- **A target Stage's `promotionTemplate`** is one step, `{as: promote, task: {kind: ClusterPromotionTask, name: promote-<deployWith>}}`, plus vars. The vars are `repo`, `src`, `branch`, `image`, `capability`, `app`, `stage`, `target`, `pinFile`, `pinKey` and `artifactKey` (`image` or `module`), and gha Stages add `workflowRepo`/`File`/`Ref`.
- **Vars are set at the Promotion level, not on the task step.** Kargo (1.12 `promotionTaskVarsToStepVars`) lets Promotion vars satisfy a task's vars, so `kargo-restore` still reads `repo`/`branch`/`image` from `promotionTemplate.spec.vars` by name. Task vars have no defaults, so a missing var fails the Promotion. `TestStagesSatisfyPromotionTasks` checks that every Stage sets every var.
- **Separate tasks per deployWith, not one with `if:`**. Inside a task, steps reference each other's outputs as `task.outputs.<alias>` (`commit`, `dispatch`). One task per deploy method keeps every step unconditional.
- **One gha wait timeout, `60m`, for all kinds.** `retry.timeout` isn't an expression. k8s and Cloud Run runs used 30m before; terraform applies need 60m.
- **The gha dispatch input key is computed**: `(vars.artifactKey): …` (an expr-lang map key in parentheses), so terraform sends `module` and image apps send `image`.
- **Cluster-scoped only.** There are no per-capability `PromotionTask` overrides; teams still write no CUE or steps (ADR 0006).

## Consequences

- A task change reaches every capability's next Promotion once CI renders it and Argo CD syncs it. Task changes are not promoted stage by stage. Promotions already running keep the steps they were inflated with.
- Kargo has no `PromotionTemplate`/`ClusterPromotionTemplate` resource. Each Stage keeps its own small `promotionTemplate` with its vars.
- `kargo-gitops-only` already blocks `kargo-api` from creating or editing ClusterPromotionTasks: they come only from git. Every controller, including the shards (whose hub SAs are bound to `kargo-controller`), can read them.
