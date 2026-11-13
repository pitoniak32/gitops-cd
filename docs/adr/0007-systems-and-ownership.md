# Systems as the config unit; one owners.yml per system

Config is grouped by business system (`systems/<sys>/`), not by team, and several teams can own apps in one system. `teams/<team>.yml` holds only `githubTeam`. It grants no access, so that team or platform can approve changes to it. `systems/<sys>/owners.yml` is the only ownership file, so a reviewer sees all of a system's access in one place:

```yaml
codeOwners: [team1]                 # GitHub teams → CODEOWNERS (code review only)
promoters:                          # stage → emails (ADR 0011)
  dev: [doug@example.com, amy@example.com]
  prod: [lead@example.com]
viewers: [bob@example.com]
breakGlass: [grace@example.com]    # system-wide only (ADR 0013)
apps:
  checkout:                         # must be an app in apps/
    codeOwners: [team2]             # replaces the list
    promoters:
      prod: [checkout-lead@example.com]  # replaces prod only; other stages inherited
    viewers: [carol@example.com]    # replaces the list
```

- `.github/CODEOWNERS` is generated from `.github/CODEOWNERS.tmpl` and the owners files: per app, `apps.<app>.codeOwners`, else `codeOwners`. The `stages/` and `targets/` approvers are the union. Platform-only lines stay last in the template, because the last matching line wins.
- Lint errors: an unknown stage or app key, a stage an app uses that has no promoter (including `autoPromote` stages), an empty `codeOwners`, a repeated team or email, `breakGlass` under `apps.<app>`, and a leftover `apps/<app>/owners.yml`.

## Considered Options

- **Ownership declared in team files**: it scatters "who owns X" across files and needs conflict checks.
- **Per-app owners files**: one system's access would be spread over several files.
- **Whole-map replace for app `promoters`**: the app would have to repeat every stage just to change prod.
- **A `promotionLevel` on stages, with promoters keyed by level**: indirection that isn't needed for 2–4 stages. Easy to add later.
- **Per-stage edit control** (a different team approves prod config): with rendered manifests, a merge only creates Freight, so the promote right on prod is the real control.

## Consequences

- `owners.yml` and `teams/` are platform-approved, because whoever can edit `owners.yml` controls promotion.
- Adding a stage to a system needs a matching `promoters` entry, or lint fails.
- Ownership changes need `gcd codeowners` in the PR, and they take effect from the next PR.
