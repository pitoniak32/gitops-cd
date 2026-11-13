# Promote and access

You want to decide who can promote, view or break glass for your apps, and then promote a release.

All access is generated from your capability's `owners.yml`. Nobody gets it by clicking in a UI or through
identity-provider groups: people are matched by the `email` of their login
([ADR 0011](../adr/0011-access-by-email.md)). Fields: [reference/owners.md](../reference/owners.md).

## `owners.yml`

<!-- embed path=capabilities/payments/owners.yml -->
```yaml
codeOwners: [team1]
promoters:
  dev: [doug@example.com]
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
viewers: [bob@example.com]
breakGlass: [oncall@example.com]
# per-app overrides of the lists above
apps:
  worker:
    codeOwners: [team1, team2]
    viewers: [bob@example.com, carol@example.com]
    promoters:
      dev: [alice@example.com, doug@example.com]
      prod: [alice@example.com]
```

Source: [`payments/owners.yml`](../../platform/render/testdata/root/capabilities/payments/owners.yml)
<!-- /embed -->

| Key | Who | Can |
|---|---|---|
| `codeOwners` | GitHub teams (from `teams/`) | approve PRs to the capability (generated CODEOWNERS) |
| `promoters.<stage>` | emails | promote that stage's targets in Kargo; see the app |
| `viewers` | emails | see the apps in Kargo and Argo CD, read-only |
| `breakGlass` | emails | also approve Freight that skipped verification ([below](#break-glass)) |

`promoters` keys are stage names (`prod`), not environment types (`prd`). Emails must be `@example.com`.

### Per-app overrides

`apps.<app>` overrides the capability-wide lists for one app. Each key **replaces** its list: in the
example, `worker`'s `prod` promoters are only `alice@example.com`, and its `staging` promoters
(not overridden) stay `doug@example.com`. To add one person, copy the current list first.
`gcd show <cap>/<app>` shows the effective owners at the top:

```sh
gcd show payments/worker
```

`owners.yml` changes always need platform approval. Run `mise run codeowners` and commit the regenerated
`.github/CODEOWNERS`.

## What you see

Each app is its own Kargo project, `kargo-<cap>-<app>`
([ADR 0029](../adr/0029-kargo-project-per-app.md)). In Argo CD, each target is an application
`<cap>-<app>-<target>`.

| You are | Kargo | Argo CD |
|---|---|---|
| promoter of stage S | the app's project; promote S's targets | the app's applications, read-only |
| viewer | the app's project, read-only | the app's applications, read-only |
| break-glass | as a promoter of every stage, plus approve unverified Freight | read-only |
| anyone else | nothing | nothing |

No one but platform admins can create, edit or delete anything in either UI. Every change goes through a PR.

## Promote a release

1. **Merge** your config change. CI renders it to the branch `rendered`.
2. **Assemble Freight**: in the Kargo UI, open your app's project, open the Warehouse and create Freight
   from a config commit plus an image (or module) tag. Nothing is assembled automatically
   ([ADR 0016](../adr/0016-image-discovery-freight-assembly.md)).
3. **Promote** to each target of your first stage (or let `autoPromote` do it). The target's manifests are
   copied to `env/<cap>/<app>/<target>` with the image digest pinned, and Argo CD syncs it. For `gha`
   targets, a workflow runs instead.
4. **Move down the graph**: once Freight is verified in a stage, promote it into the stages that depend on
   it. To release a whole multi-target stage at once, use **Promote to downstream** on the stage's
   control-flow node.

`prd` targets run the platform's pre-deploy checks first
([targets and stages](targets-stages.md#pre-deploy-checks)).

## Break-glass

Normally Freight must be verified in every upstream stage before it can enter a stage. Kargo calls a skip
"Manual approval required". Only `breakGlass` emails and admins can approve that; promoters get a 403
([ADR 0013](../adr/0013-break-glass-approval.md)). Approving only lets the Freight in, and someone still
has to promote it. Keep `breakGlass` to on-call people. Every approval is logged. How the guard works:
[break-glass](../break-glass.md).
