# Onboard a capability

You want to put a new capability under `capabilities/` and deploy its first app to one dev target.

A capability is five kinds of file. The examples below are the sandbox's smallest capability, `search`,
one app on one dev target. Copy them and rename. Every field is listed in [reference/](../reference/).

## 1. Register your team

One file per GitHub team, used only to generate CODEOWNERS. It grants no UI access
([ADR 0007](../adr/0007-capabilities-and-ownership.md)). Skip this step if your team already has a
file in `teams/`.

```yaml path=teams/team3.yml
githubTeam: acme/team3
```

The file name (`team3`) is how `owners.yml` refers to the team. That team or the platform team approves it.

## 2. Say who owns it: `owners.yml`

<!-- embed path=capabilities/search/owners.yml -->
```yaml
codeOwners: [team2]
promoters: {dev: [alice@example.com]}
```

Source: [`search/owners.yml`](../../platform/render/testdata/root/capabilities/search/owners.yml)
<!-- /embed -->

- `codeOwners`: teams (file names from `teams/`) that approve PRs to this capability.
- `promoters`: per stage, the emails that may promote there. Keys are stage names.
- Optional: `viewers`, `breakGlass`, and per-app overrides under `apps:`
  ([promote and access](promote-access.md)).

`owners.yml` changes always need platform approval.

## 3. Add a stage

One file per stage in `stages/`; the file name is the stage name.

<!-- embed path=capabilities/search/stages/dev.yml -->
```yaml
dependsOn: []
autoPromote: true
```

Source: [`search/stages/dev.yml`](../../platform/render/testdata/root/capabilities/search/stages/dev.yml)
<!-- /embed -->

A first stage depends on nothing. `autoPromote: true` promotes new Freight automatically once it's
assembled; it's not allowed on stages that reach a `prd` target. More stages: [targets and stages](targets-stages.md).

## 4. Add a target

One directory per target in `targets/`; the directory name must follow
`<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]` ([ADR 0025](../adr/0025-target-naming-convention.md)).

<!-- embed path=capabilities/search/targets/us-east4-dev-k8s/target.yml -->
```yaml
config:
  cluster: us-east4
  namespace: search-us-dev
  environment: dev
  environmentType: dev
  auth:
    wifEmail: search-dev@search-dev.iam.gserviceaccount.com
  billing: dev
  platform: K8S
```

Source: [`search/targets/us-east4-dev-k8s/target.yml`](../../platform/render/testdata/root/capabilities/search/targets/us-east4-dev-k8s/target.yml)
<!-- /embed -->

`cluster` must be one the platform registers, accepting this `environmentType`. A target does nothing
until an app lists it.

## 5. Add an app

One directory per app in `apps/`; the directory name is the app name.

<!-- embed path=capabilities/search/apps/api/app.yml -->
```yaml
image: ghcr.io/pitoniak32/nginx
sourceRepo: pitoniak32/gitops-cd
port: 80
stages:
  dev:
    targets:
      us-east4-dev-k8s: {}
```

Source: [`search/apps/api/app.yml`](../../platform/render/testdata/root/capabilities/search/apps/api/app.yml)
<!-- /embed -->

- `image`: repository only, no tag. Kargo discovers SemVer tags; you pick one when you promote.
- `sourceRepo`: where the app's code lives (pre-deploy checks run there).
- `stages.<stage>.targets`: which targets the app deploys to, per stage.

More settings (replicas, env, secrets, resources): [configure apps](apps.md).

## 6. Check and open the PR

```sh
gcd lint search              # no output = valid
gcd show search/api -s       # effective config, with where each value comes from
mise run codeowners          # owners.yml or teams/ changed: regenerate .github/CODEOWNERS
```

Commit `.github/CODEOWNERS` with your change, and say in the PR description that it needs platform
approval (new `owners.yml`). After merge, open the Kargo UI and promote your first Freight
([promote and access](promote-access.md)). Details of the loop: [working loop](workflow.md).
