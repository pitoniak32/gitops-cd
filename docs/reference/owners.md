# owners

`owners.yml` says who reviews, promotes and views a system's apps. There is exactly one per
system, at `systems/<sys>/owners.yml` ([ADR 0007](../adr/0007-systems-and-ownership.md)). All
Kargo and Argo CD access is generated from it, matched on the login's `email`
([ADR 0011](../adr/0011-access-by-email.md)). Changes always need platform approval.

How-to: [promote and access](../guide/promote-access.md).

## Defining owners

<!-- embed path=systems/payments/owners.yml -->
```yaml
codeOwners: [team1]
promoters:
  dev: [doug@example.com]
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
viewers: [bob@example.com]
breakGlass: [grace@example.com]
# per-app overrides of the lists above
apps:
  worker:
    codeOwners: [team1, team2]
    viewers: [bob@example.com, carol@example.com]
    promoters:
      dev: [alice@example.com, doug@example.com]
      prod: [alice@example.com]
```

Source: [`payments/owners.yml`](../../platform/render/testdata/root/systems/payments/owners.yml)
<!-- /embed -->

The file has system-wide roles at the top and per-app overrides under `apps`.

## Roles

| Key | Takes | Grants |
|---|---|---|
| `codeOwners` (required) | team names from `teams/` | approve PRs to the system (generated CODEOWNERS) |
| `promoters` (required) | per stage, emails | promote that stage's targets in Kargo; read the app |
| `viewers` | emails | read-only access to the apps in Kargo and Argo CD |
| `breakGlass` | emails | approve Freight that skipped upstream verification ([ADR 0013](../adr/0013-break-glass-approval.md)) |

`codeOwners` takes team file names (`team1` for `teams/team1.yml`), not GitHub team slugs.

Emails are lowercase `@example.com` addresses.

### Promoters

`promoters` maps a stage name, a file in `stages/`, to the emails that may promote into it. Keys are
stage names (`prod`), not environment types (`prd`). Every stage an app uses needs promoters, either
here or in that app's override:

```yaml path=systems/payments/owners.yml expect="needs promoters.dev"
codeOwners: [team1]
promoters:
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
```

## Per-app overrides

`apps.<app>` overrides `codeOwners`, `promoters` and `viewers` for one app (an app directory of this
system). Each set field **replaces** the system's value; `promoters` replaces per stage:

<!-- embed path=systems/payments/owners.yml lines=10-16 -->
```yaml
apps:
  worker:
    codeOwners: [team1, team2]
    viewers: [bob@example.com, carol@example.com]
    promoters:
      dev: [alice@example.com, doug@example.com]
      prod: [alice@example.com]
```

Source: [`payments/owners.yml:10-16`](../../platform/render/testdata/root/systems/payments/owners.yml#L10-L16)
<!-- /embed -->

Here `worker`'s `prod` promoters are only `alice@example.com`, its `staging` and `perf` promoters stay
`doug@example.com`, and its viewers are `bob@example.com` and `carol@example.com`. `breakGlass` can't be
overridden per app. `gcd show <sys>/<app>` prints an app's effective owners.

Every field: `gcd schema owners`.
