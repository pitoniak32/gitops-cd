# Per-stage promoters, one owners.yml per capability

Status: proposed. Amends ADR 0011 (owners.yml shape, `promote-<app>`) and ADR 0013 (break-glass in promote roles).

Promoters could promote an app to every stage. We want finer control, e.g. anyone on the team promotes to dev but only leads promote to prod. Kargo can't select Stages by label (`promote` is a plain Kubernetes RBAC verb: `resourceNames` only, and an empty list means every Stage). The renderer already lists Stage names, so the grouping lives in YAML and CUE expands it.

Access was also spread over `capabilities/<cap>/owners.yml` plus `apps/<app>/owners.yml`. Now everything is in one file, so a reviewer sees all of a capability's access in one place.

## Decision

`capabilities/<cap>/owners.yml` is the only owners file. `apps/<app>/owners.yml` is removed (lint error if present).

```yaml
codeOwners: [team1]                 # GitHub teams → CODEOWNERS (was `owners`)
promoters:                          # stage name → emails
  dev: [doug@example.com, amy@example.com]
  staging: [doug@example.com]
  prod: [lead@example.com]
viewers: [bob@example.com]
breakGlass: [oncall@example.com]    # capability-wide only
apps:
  checkout:                         # must be an app in apps/
    codeOwners: [team2]             # replaces the list
    promoters:
      prod: [checkout-lead@example.com]  # replaces prod only; other stages inherited
    viewers: [carol@example.com]    # replaces the list
```

- `owners` is renamed to `codeOwners`, to make clear it is code review (CODEOWNERS) and not deploy rights.
- Overrides under `apps.<app>`: `codeOwners` and `viewers` replace the whole list. `promoters` merges per stage: a stage key replaces that stage's list, and stages left out are inherited from the capability.
- Every stage an app uses needs at least one promoter after the merge, including `autoPromote` stages (manual re-promotion and Freight approval still need someone).
- `breakGlass` is top level only.

Rendering (Kargo project = capability):

- One ServiceAccount + Role + RoleBinding `promote-<app>-<stage>` per app and stage the app uses. This replaces `promote-<app>`.
  - Claims: that stage's merged promoters plus `breakGlass`.
  - `promote` on that app's Kargo Stages that belong to the stage: target Stages `<app>-<target>` and its control-flow Stages.
  - Same `promotions` and `freights: create` rules as before.
- Kargo grants the union of every SA whose claims match, so a dev+prod promoter gets both.
- `viewers` SA, `users-<cap>` and Argo CD AppProject viewer roles: same as before. The email set is all stages' promoters (capability and app overrides) + viewers + breakGlass.
- `example.com/break-glass` annotation: unchanged (capability `breakGlass` on every Stage). The guard still decides who may approve unverified Freight. The `promote` check behind it is now per stage, and break-glass users pass it because they are in every stage's SA.
- CODEOWNERS: per app, `apps.<app>.codeOwners` else `codeOwners`. `stages/` and `targets/` approvers stay the union.

Validation errors (`error(...)` on the owners.yml field, so lint gives `file:line:col`):

- unknown stage key or app key
- a stage the app uses with no promoter
- empty `codeOwners`
- repeated team or email
- `breakGlass` under `apps.<app>`
- leftover `apps/<app>/owners.yml`

## Considered Options

- **Label selectors on Stages**: not possible in Kubernetes RBAC or Kargo.
- **`promotionLevel` field on `stages/<s>.yml`, promoters keyed by level**: an extra layer of indirection for capabilities that only have 2–4 stages. Easy to add later if stages start sharing levels.
- **Keep `apps/<app>/owners.yml`**: access for one capability stays spread over several files.
- **Whole-map replace for app `promoters`**: consistent with the other fields, but the app would have to repeat every stage just to change prod.
- **List shorthand (`promoters: [emails]` = every stage)**: two shapes for one field. The map is explicit and short.
- **Per-app `breakGlass`**: not needed; break-glass is an on-call concern for the whole capability.

## Consequences

- Breaking change: every `owners.yml` is rewritten (`owners` → `codeOwners`, `promoters` becomes a map), and app owners files are moved into `apps:`.
- Adding a stage to a capability needs a matching `promoters` entry, or lint fails.
- More Kargo ServiceAccounts: apps × stages instead of apps.
- `freights: create` still can't be scoped (names are content hashes): any promoter in the project can assemble Freight for any app at any stage. Promoting it still needs that stage's role.
