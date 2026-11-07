# Capabilities and app ownership

## Goal

Split config by **capability** instead of by team. Ownership moves to a separate `teams/` registry, so several teams can own apps in one capability. Owners can approve code changes and promote. Prod can't be auto-promoted.

## Layout

```
teams/<team>.yml
capabilities/<cap>/
  owners.yml
  stages/<stage>.yml
  targets/<target>.yml
  apps/<app>/app.yml
  apps/<app>/owners.yml      # optional
rendered/apps/<cap>/<app>/<stage>/<target>/manifests.yaml
rendered/platform/{kargo,argocd}/<cap>.yaml
rendered/pipelines/<cap>.json
.github/CODEOWNERS           # generated
```

`stages/`, `targets/` and `app.yml` keep their current formats. "Team" as the config separator is renamed to "capability" throughout: the render tag, Kargo project, Argo CD app names, stage branches and rendered paths.

## Files

`teams/<team>.yml`. The team name is the file stem.
```yaml
githubTeam: acme/payments-sre   # CODEOWNERS
idpGroup: payments-sre          # Kargo promote RBAC (OIDC claim)
```

`capabilities/<cap>/owners.yml` and `apps/<app>/owners.yml`:
```yaml
owners: [payments-core, payments-sre]
```
An app's owners are its own `owners.yml` if it exists (this replaces, not merges), otherwise the capability's.

## Permissions

| Action | Who |
|---|---|
| Approve changes under `apps/<app>/` (except `owners.yml`) | the app's owners |
| Approve changes under `stages/`, `targets/` | the union of owners across the capability's apps |
| Approve changes to any `owners.yml` or `teams/` | @acme/platform |
| Promote or approve freight to any stage of an app | the app's owners |

## Rules (CUE, run in render, vet and CI lint)

- Every owner must be a known team, with errors listing the allowed teams.
- `owners` is non-empty, so every app resolves to at least one owner.
- A stage that deploys any app to a target with `environment: prod` must not set `autoPromote: true`. The error goes on that stage: `stage prod deploys to prod targets; autoPromote not allowed`.
- Editor JSON schemas: add `owners.json` and `team.json`, following the existing generic-schema approach (ADR 0005).

## Generated output

- **CODEOWNERS:** generated from the rules above, with the platform lines kept as a hand-written header. CI on main regenerates it in the same commit as the render, so no author has to run tooling locally. An ownership change takes effect from the next PR onward, and that's fine because platform reviews every `owners.yml` and `teams/` change. Hand edits to `.github/CODEOWNERS` are overwritten.
- **Kargo:** each capability gets its project `kargo-<cap>`. For each owning team, it also gets:
  - a `ServiceAccount` `owner-<team>` with annotation `rbac.kargo.akuity.io/claims: '{"groups":["<idpGroup>"]}'`
  - a `Role` granting `promote` on `stages` with `resourceNames` = `<app>-<stage>` for every app the team owns, plus `get`/`list`/`watch` on project resources
  - a `RoleBinding`

## Migration

- `teams/team1` moves to `capabilities/payments`.
- `teams/team2` moves to `capabilities/search`.
- Add `teams/team1.yml` and `teams/team2.yml` with placeholder `githubTeam` and `idpGroup` values.
- Each capability gets `owners.yml` listing its old team.
- Rendered paths, branches and Argo CD names change from team to capability. This is a one-time rename and the example repo has nothing live.
- Update the golden testdata, README, CLAUDE.md and GLOSSARY (add Capability, Owner; Team now means an owning group). Add ADR 0007.

## Testing

- Golden render test with the capability names.
- Lint tests for:
  - unknown owner team
  - empty owners
  - autoPromote on a prod stage
- A CODEOWNERS generation golden.

## Out of scope / future

- Per-stage ownership.
- Managers vs users, where managers can approve any change in a capability. The planned shape is `owners.yml` gaining `managers: [...]`, with managers added to every capability CODEOWNERS line.
- Argo CD sync/rollback RBAC and k8s runtime access.
- Slack and on-call fields in `teams/`.

## Unverified

- Kargo's `promote` verb and claims-annotation RBAC semantics are from Kargo docs as remembered and not tested against a cluster.
- Whether freight manual approval uses the same verb.
