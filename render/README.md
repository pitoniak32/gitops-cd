# platform

CUE that turns capability YAML (stages, targets, apps) into k8s manifests + a promotion pipeline.
Lives in the gitops monorepo; teams never edit it.

| file | role |
|---|---|
| `input.cue` | tags (`capability`, `repo`), routes YAML by path into `stages` / `targets` / `apps` / owners / `teams` |
| `clusters.cue` | **platform-owned** cluster registry + per-environment policy |
| `team.cue` | `#Stage`, `#Target`, DAG depth (cycle detection) |
| `app.cue` | `#App`, layered merge app → stage → target → `deploys` |
| `pipeline.cue` | neutral per-app promotion graph (`pipelines/<cap>.json`) for Kargo / Temporal |
| `k8s.cue` | ServiceAccount (WIF), Deployment, Service, ExternalSecret per (app, target) |
| `delivery.cue` | Kargo Project/ProjectConfig/Warehouse/Stages + Argo CD Applications |
| `files.cue` | every output file, keyed by rendered-repo path |
| `scripts/schemas.sh` | generic editor JSON Schemas → `schemas/` (committed, versioned with the platform) |
| `scripts/lint.sh` | full CUE validation as `file:line:col: message` (editors, CI annotations) |

## Capability input (paths under the repo root)
```
owners.yml                owners: [team, ..]  (apps/<app>/owners.yml replaces it)
../../teams/<team>.yml    githubTeam, idpGroup
stages/<stage>.yml        dependsOn: [..], autoPromote: bool
targets/<target>.yml      config: {cluster, namespace, environment, auth.wifEmail, billing, platform}
apps/<app>/app.yml        image, port, replicas, env, resources, stages.<s>.targets.<t>
```

## Merge rules
- `replicas`, `resources.*`: target > stage > app > default.
- `env`: merged per key, same precedence. Keys can't be removed at a lower layer.
- `image`, `port`: app level only (image promotes through stages via Kargo).
- An app may skip stages; deps pass through (no perf → prod depends on staging).
- Each target used once per app; two targets of one app can't share cluster+namespace.

## Output (`rendered/` in the monorepo)
```
apps/<cap>/<app>/<stage>/<target>/manifests.yaml
platform/kargo/<cap>.yaml      platform/argocd/<cap>.yaml      pipelines/<cap>.json
```

## Promotion
Warehouse per app watches `rendered/apps/<cap>/<app>/` on main → Freight = commit.
Promoting to stage S copies `rendered/apps/<cap>/<app>/S/` from that commit to branch
`env/<cap>/<app>/S`; Argo CD app `<cap>-<app>-<target>` tracks that branch at `<target>/`.
Multi-upstream stages use `availabilityStrategy: All`.

## Editor feedback (two layers)
- **Editor** — `schemas/*.json` from `cue def -t generic=true`: structure, types, formats, cluster/env enums.
  Stage/target names are only pattern-checked, so they don't depend on team files. Team YAML references
  `platform/schemas/*.json` by relative path, so any editor works with no local tooling.
- **CI** — `scripts/lint.sh <root> <cap>`: `cue vet -c -E` over the capability's files (+ `teams/`), as `file:line:col: message`
  (PR annotations). CUE paths without a YAML position are mapped back with yq. Linux CI only. Custom messages come from `error("...")` in the CUE (unknown stage/target/cluster,
  env/cluster mismatch, prod replicas). An app's computed checks (e.g. prod replicas) appear once its structural errors are fixed.

## Dev loop
`scripts/test.sh` (vet + render testdata/root (capability payments) + golden diff), `UPDATE=1 scripts/test.sh` to accept.
