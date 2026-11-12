# render

Test fixtures and the golden tree for the renderer. The renderer itself is Go:
`platform/cli/internal/capability` turns capability YAML (stages, targets, apps) into k8s manifests + a
promotion pipeline (ADR 0030). Teams never edit either.

| where | role |
|---|---|
| `capability/config.go` | **platform-owned** config: cluster registry, per-environment-type replica policy, pre-deploy checks, the platform table (everything that differs per target platform) |
| `capability/load.go` | the one table of input file routes; reads the YAML |
| `capability/schema.go`, `validate.go` | what each file may contain + cross-file rules, every problem a located diagnostic |
| `capability/model.go` | layered merge app → stage → target → deploy, owners |
| `capability/graph.go` | neutral per-app promotion graph (`pipelines/<cap>.json`), Kargo Stage graph (ADR 0019) |
| `capability/kargo.go`, `output.go` | Kargo Project/ProjectConfig/Warehouse/Stages/RBAC, Argo CD params + AppProjects, k8s / Cloud Run / terraform files, CODEOWNERS fragment |
| `capability/platformfiles.go` | **platform-level** `ClusterPromotionTask`s (ADR 0027), region files, `mise run regions` config |
| `emit` | ordered maps → YAML/JSON in the committed formatting |
| `gcd render` / `gcd codeowners` (`internal/render`) | `mise run render` / `codeowners`: validate + write the files, CODEOWNERS |
| `gcd lint` (`internal/lint`) | `mise run lint`: full validation as `file:line:col: message` (editors, CI annotations) |
| `testdata/` | fixture capabilities (`root/`) + golden output |

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
apps/<app>/app.yml        sourceRepo (<owner>/<name>, required), image (repo only), imageConstraint, port, replicas, env, envFrom, resources, stages.<s>.targets.<t>
                          terraform app (ADR 0021): module (OCI repo) + moduleConstraint instead of image; env, envFrom,
                          workflow only; renders terraform.yaml per target
                          image apps may also use CLOUD_RUN targets: renders service.yaml (Knative Service), gha only,
                          replicas → minScale, default workflow cloud-run.yaml (ADR 0024)
```

## Merge rules
- `replicas`, `resources.*`, `deployWith`, `workflow.*`: target > stage > app > platform default (`_platforms[].defaults`) > default. A platform or artifact can force `deployWith` (CLOUD_RUN, terraform apps: gha).
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
infrastructure/regions/<region>.json                 (gcd render, cluster registry only)
kargo/_promotion-tasks.yaml                          (gcd render, platform-level)
```

## Promotion
Warehouse per app watches `rendered/apps/<cap>/<app>/` on main and the app's image repo (SemVer);
Freight creation is Manual: promoters assemble Freight = (commit, image) in the Kargo UI.
Promoting target T copies `rendered/apps/<cap>/<app>/<stage>/T/` from the Freight commit to branch
`env/<cap>/<app>/T`, sets the Freight's image (`yaml-update` on `deployment.yaml`, `<repo>:<tag>@<digest>`),
and syncs Argo CD app `<cap>-<app>-T` (path `.`, plain YAML). Gates and `availabilityStrategy: All`: ADR 0012.
The steps live in ClusterPromotionTask `promote-<deployWith>`; each target Stage's template is one `task`
step plus vars (ADR 0027).
Platform pre-deploy checks (`config.go` `PreDeploy`, ADR 0028): per environment type, `{File, Ref, Timeout}`
(teams can't set it). Target Stages of that type run `pre-<n>` steps (ClusterPromotionTask `gha-check`,
workflow vars on the step) before `promote`: dispatch `file` to the app's `sourceRepo` with the Freight's
commit and artifact, wait for success (`timeout`, max 2h); anything else fails the Promotion before the push.

## Feedback
`mise run lint` (`gcd lint [cap…]`, `--root` for another tree; also CI, as PR annotations): validates each
capability's files (+ `teams/`) as `file:line:col: message`, pointing at the offending key or list item.
There is no editor schema (ADR 0030). Two rounds: the files as written (structure, names, per-platform
fields, owners), then, once those are clean, what only shows in derived values (merged replicas vs policy,
envFrom, prod autoPromote, stage cycles, generated name lengths).

## Dev loop
`mise run test` (Go tests in `platform/cli/internal/{capability,render,lint}`: golden diff of testdata/root, render assertions, lint cases), `UPDATE=1 mise run test` to accept.
