# render

CUE that turns capability YAML (stages, targets, apps) into k8s manifests + a promotion pipeline.
Lives in the gitops monorepo; teams never edit it.

| file | role |
|---|---|
| `input.cue` | tags (`capability`, `repo`), routes YAML by path into `stages` / `targets` / `apps` / owners / `teams` |
| `clusters.cue` | **platform-owned** cluster registry + per-environment-type policy |
| `team.cue` | `#Stage`, `#Target`, DAG depth (cycle detection) |
| `app.cue` | `#App`, layered merge app → stage → target → `deploys` |
| `pipeline.cue` | neutral per-app promotion graph (`pipelines/<cap>.json`) for Kargo / Temporal |
| `k8s.cue` | ServiceAccount (WIF), Deployment, Service, ExternalSecret per (app, target) |
| `delivery.cue` | Kargo Project/ProjectConfig/Warehouse/Stages + Argo CD Applications |
| `files.cue` | every output file, keyed by rendered-repo path |
| `gcd render` / `gcd codeowners` (`platform/cli/internal/{cueload,render}`, ADR 0026) | `mise run render` / `codeowners`: validate + write `files`, region files, CODEOWNERS |
| `gcd lint` (`platform/cli/internal/{lint,yamlpos}`) | `mise run lint`: full CUE validation as `file:line:col: message` (editors, CI annotations) |
| `gcd schemas` (`platform/cli/internal/schemas`) | generic editor JSON Schemas → `schemas/` (committed, versioned with the platform) |

## Capability input (paths under the repo root)
```
owners.yml                codeOwners: [team, ..], promoters: {<stage>: [email, ..]}, viewers: [email, ..],
                          breakGlass: [email, ..], apps: {<app>: {codeOwners?, promoters?, viewers?}}
../../teams/<team>.yml    githubTeam
stages/<stage>.yml        dependsOn: [..], autoPromote: bool
targets/<t>/target.yml    config: {platform (K8S|TERRAFORM|CLOUD_RUN), environmentType (dev|npr|prd), auth.wifEmail, billing,
                          K8S: cluster, namespace; TERRAFORM: region, gcpProject, backend.{bucket, prefix};
                          CLOUD_RUN: region, gcpProject (ADR 0024)}
                          dir name: <region>-<env>-<platform>[-<qualifier>] (k8s|tf|cr), ADR 0025
targets/<t>/environment-variables/<name>.yml   KEY: value (flat)
apps/<app>/app.yml        image (repo only), imageConstraint, port, replicas, env, envFrom, resources, stages.<s>.targets.<t>
                          terraform app (ADR 0021): module (OCI repo) + moduleConstraint instead of image; env, envFrom,
                          workflow only; renders terraform.yaml per target
                          image apps may also use CLOUD_RUN targets: renders service.yaml (Knative Service), gha only,
                          replicas → minScale, default workflow cloud-run.yaml (ADR 0024)
```

## Merge rules
- `replicas`, `resources.*`: target > stage > app > default.
- `env`: merged per key, same precedence. Keys can't be removed at a lower layer.
- `envFrom: [<name>]` (any layer, unioned): adds `targets/<t>/environment-variables/<name>.yml` of each target, below all `env`; between files the most specific layer's `envFrom` wins. Missing file, or one KEY with different values from two files of the same layer = lint error (ADR 0014).
- `image`, `imageConstraint`, `port`: app level only. `image` is a repo without tag/digest; Kargo discovers SemVer tags (within `imageConstraint`) and the image promotes through stages in Freight (ADR 0016).
- An app may skip stages; deps pass through (no perf → prod depends on staging).
- Each target used once per app; two targets of one app can't share cluster+namespace.

## Output (`rendered/` in the monorepo)
```
apps/<cap>/<app>/<stage>/<target>/{deployment.yaml,manifests.yaml}
kargo/<cap>.yaml      pipelines/<cap>.json
argocd/deploys/<region>/<cap>/<app>-<target>.json   argocd/projects/<region>/<cap>.yaml
infrastructure/regions/<region>.json                 (gcd render, clusters.cue only)
```

## Promotion
Warehouse per app watches `rendered/apps/<cap>/<app>/` on main and the app's image repo (SemVer);
Freight creation is Manual: promoters assemble Freight = (commit, image) in the Kargo UI.
Promoting target T copies `rendered/apps/<cap>/<app>/<stage>/T/` from the Freight commit to branch
`env/<cap>/<app>/T`, sets the Freight's image (`yaml-update` on `deployment.yaml`, `<repo>:<tag>@<digest>`),
and syncs Argo CD app `<cap>-<app>-T` (path `.`, plain YAML). Gates and `availabilityStrategy: All`: ADR 0012.

## Editor feedback (two layers)
- **Editor** — `schemas/*.json` from the CUE defs (`gcd schemas`, generic=true): structure, types, formats, cluster/env enums.
  Stage/target names are only pattern-checked, so they don't depend on team files. Team YAML references
  `platform/render/schemas/*.json` by relative path, so any editor works with no local tooling.
- **CI** — `mise run lint` (`gcd lint [cap…]`, `--root` for another tree): the CUE Go library validates each capability's files (+ `teams/`) like `cue vet -c -E`, as `file:line:col: message`
  (PR annotations). CUE paths without a YAML position are mapped back to the YAML key (`internal/yamlpos`). Custom messages come from `error("...")` in the CUE (unknown stage/target/cluster,
  environment type/cluster mismatch, prod replicas). An app's computed checks (e.g. prod replicas) appear once its structural errors are fixed.

## Dev loop
`mise run test` (Go tests in `platform/cli/internal/{render,lint}`: golden diff of testdata/root, render assertions, lint cases), `UPDATE=1 mise run test` to accept.
