# gitops-cd

System YAML (`systems/`) + team registry (`teams/`) → Go renderer (`gcd`, `platform/cli/internal/system`) → generated manifests (`rendered/`), promoted by Kargo, synced by Argo CD, all running locally on kind: a hub (Kargo control plane) + one cluster per region with its own Argo CD and Kargo shard (ADR 0015). Every Argo CD runs one root app of ApplicationSets; cluster generator labels pick what each deploys (ADR 0017). Overview and logins: `README.md`. Terms: `GLOSSARY.md`. Decisions: `docs/adr/`.

## Layout
App teams own `systems/`; everything platform-owned (renderer, cluster, add-ons, tasks) lives in `platform/`. `teams/`, `rendered/`, `docs/` stay at root (ADR 0010).
- `systems/<sys>/{owners.yml,stages,targets,apps}`: YAML only; folder name = system name. Target = dir `targets/<t>/target.yml` (name `<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]`, lint-enforced, ADR 0025; required `environmentType: dev|npr|prd`, `environment` free name; `platform: K8S` default; `TERRAFORM` for terraform apps: `module:` instead of `image:`, flat `gcpProject`/`region`/`backend`, render `terraform.yaml`, gha only, ADR 0021; `CLOUD_RUN` for image apps, flat `gcpProject`/`region`, render Knative `service.yaml`, gha only, ADR 0024) + optional `environment-variables/<name>.yml` (flat env map; apps opt in with `envFrom: [<name>]`, ADR 0014). One `owners.yml` per system (ADR 0007): `codeOwners`, `promoters: {<stage>: [emails]}`, `viewers`, `breakGlass`, `apps: {<app>: {codeOwners?, promoters?, viewers?}}` overrides (no app owners files).
- `teams/<team>.yml`: githubTeam only (CODEOWNERS); that team or platform approves it. No access rights.
- `owners.yml` (system; `apps:` overrides, ADR 0007): `codeOwners` (teams → CODEOWNERS), `promoters` (per stage) + `viewers` + `breakGlass` (emails `@example.com` → Kargo/Argo CD access); platform-approved.
- `platform/cli/internal/system/`: the renderer, plain Go (ADR 0030, no CUE): `config.go` platform-owned config (cluster registry, replica policy, pre-deploy checks per environmentType ADR 0028, `platforms` table = all per-platform differences), `load.go` (`routes`: the one file-layout table), `schema.go` + `validate.go` (per-file + cross-file rules → located `Diag`s; reads the YAML as written), `model.go` (layered merge → deploys, owners; derived checks), `graph.go` (pipeline, Kargo Stage graph ADR 0012, name lengths), `kargo.go` (Warehouse: git + image subscriptions, Manual Freight; target Stages get `spec.shard` = cluster region; promote RBAC), `output.go` (workload files, Argo CD params `rendered/argocd/deploys/<region>/<sys>/` + AppProjects `rendered/argocd/projects/<region>/`, CODEOWNERS fragment), `platformfiles.go` (shared `ClusterPromotionTask`s `rendered/kargo/_promotion-tasks.yaml` ADR 0027, `rendered/infrastructure/regions/`, `mise run regions` config). `internal/emit`: ordered YAML/JSON writer. `internal/{render,lint}`: `gcd render`, `codeowners`, `lint` (parallel; `lint.Changed` maps changed files → scope; `internal/progress` bars on a terminal only); test-only `yamledit`, `appsets`.
- `platform/render/`: `testdata/` fixtures + golden, `rendered-README.md`. No editor schemas: lint is the only validation (ADR 0030).
- `docs/`: `README.md` index by reader; `guide/` app-team guide + `reference/` one page per file kind + `evaluate/` why gitops-cd for evaluators (all hand-written; every yaml fence annotated `path=` / `expect="…"` / `fragment`, or inside `<!-- embed path=<fixture file> [lines="A-B,N,(A-B;text='comment')"] -->` … `<!-- /embed -->`, whose body `gcd docs` writes; never edit an embed body); `adr/`, ops docs.
- `rendered/`: generated; CI commits it to the orphan branch `rendered` on each push to main (ADR 0002), gitignored on main (`gcd render` writes it locally). Argo CD + Kargo read branch `rendered`. Never edit. `platform/bootstrap/root.yaml`: root app, identical in every Argo CD, syncing `platform/appsets/`.
- `platform/appsets/`: ApplicationSets (ADR 0017). Cluster labels `example.com/role: hub|region`, `example.com/region: <r>` on each Argo CD's in-cluster Secret. `addons/<addon>.yaml` (git-sourced chart + `$values` / kustomize path; region bits in `valuesObject` via `{{.values.region}}`), `kargo.yaml` (hub `rendered-kargo`), `projects.yaml`, `workloads.yaml`. Static checks: `platform/cli/internal/appsets` tests.
- `platform/cli/`: Go CLI `gcd` (ADR 0018): cluster setup (`up`, `down`, `infra`, `regions`, `certs`, `creds`, `kargo-shards`, `kargo-restore`, `open`, DNS) + the renderer (`render`, `lint`, `codeowners`). Built to `.bin/gcd` by mise task `cli`; the setup mise tasks are one-line `exec .bin/gcd <cmd>`.
- `.github/CODEOWNERS`: generated from `.github/CODEOWNERS.tmpl` + owners; regenerate with `mise run codeowners` and commit it in the PR (CI checks; never hand-edit).
- `platform/infrastructure/<addon>/`: add-on config read from git by its AppSet (`values.yaml`, `manifests/`). `platform/cluster/`: `kind.yaml` (hub), `region.yaml` (region template), `regions` (generated by `mise run regions`; read by up/down/infra).

## Commands (`mise tasks` lists all)
Setup tasks call `.bin/gcd` (`mise run cli` builds it; tasks depend on it). Lint and render have no mise task: run `gcd lint` / `gcd render` (`.bin` is on PATH via mise; rebuild with `mise run cli` after changing `platform/cli`).
- `mise run up` / `down`: fresh hub + region kind clusters + infrastructure + bootstrap / delete everything.
- `mise run open [--print]`: open all UIs (Kargo, hub + regional Argo CD, Keycloak); URL list in `README.md`.
- `mise run infra`: helm-bootstrap Argo CD (hub + regions), apply root app, wait for add-ons in order, git creds, certs, Kargo shard tokens.
- `mise run regions`: regenerate region-derived config from the cluster registry in `system/config.go` (`platform/cluster/regions`, Keycloak `argocd-<r>` clients, Kargo `api.argocd.urls`); CI checks drift.
- `mise run kargo-restore [sys…]`: after a cluster rebuild, restore each target Stage's current Freight from its `env/*` branch head (idempotent; `docs/operations.md`).
- `gcd lint [sys…] [--changed [--base <rev>]] [--all]` (gcd itself, on PATH via mise; no mise task): full validation → `file:line:col: message`; systems in parallel; `--changed` (PR CI) = only what changes since the branch fork point (or `--base`) can affect (`lint.Changed`; files from `repo.ChangedFiles`); `--all` overrides any narrowing (lint + render). In GitHub Actions with no scope flags, gcd picks scope from the event (PR → `--changed` vs base, label `lint-all` → all; else all) and prints `::error` annotations (`ghactions`).
- `gcd schema [kind] [-s] [--json]`: annotated YAML skeleton per kind (owners, stage, app, target, env-vars): placeholders, required/pattern/description comments; `-s/--sources` adds fixture examples. `gcd show <sys>[/<app>] [--stage s] [--target t] [-s] [--json]`: effective merged config as YAML (`owners`, then `<stage>.<target>` per app; `owners` is a reserved stage name, lint-enforced); `-s/--sources` comments each value with its `file:line` (relative to `systems/<sys>/`) or default note. `gcd show … --rendered [--include-platform]`: rendered manifests of the selected deploys (+ Argo CD params, Kargo Stage). `gcd show [<sys>[/<app>]] --diff [--base <rev>] [--stat]`: rendered diff of working tree vs base (replaces `gcd diff`); no system = every system whose inputs changed (all rendered files); system = selected deploys; `--stat` per-file counts. `gcd show --diff --against <rev> [--stat]`: fresh in-memory render vs the `rendered/` tree committed at `<rev>` (e.g. the rendered branch; CI uses it for the PR comment; no `rendered/` at rev = empty); no system = everything `gcd render` writes under `rendered/`; excludes `--base`. Base for `show --diff` and `lint --changed` defaults to the branch fork point (jj `fork_point(trunk() | @)`, git merge-base with `origin/main`, else `main`); explicit `--base` = jj revset / git rev (`show --diff` uses it as given, `lint --changed` forks from it). Works with jj or git. `gcd lint --format text|json|github|auto`; show/render print validation errors like lint. `--color auto|always|never` (all commands; auto: TTY and no `NO_COLOR`; `--json` never colored). `--root <dir>` is global (default this repo; sandbox: `platform/render/testdata/root`; `--diff`/`--changed` need a jj or git repo).
- `gcd render [sys…] [--out dir] [--platform] [--all]`: render all systems (or the named ones; `--out` default `rendered`) + regenerate CODEOWNERS.
- `gcd docs [--format …]`: rewrite embed blocks in `docs/guide/` + `docs/reference/` + `docs/evaluate/` from the fixture, lint the fixture, then lint every annotated yaml block (each over its own fixture copy; problems like lint). Run after fixture, schema or doc edits; commit the pages. `TestRepoDocs` guards it (`cli.yaml`). Embeds pick lines by number: check them after editing a fixture file.
- `mise run test` = `test-cli` (gofmt + `go vet` + `go test ./...` in `platform/cli`: renderer golden, CODEOWNERS, lint cases, render assertions; `UPDATE=1` accepts golden) + `test-tf` (`platform/tests/tf-module-push.sh`).
- CI, one workflow per component: `config.yaml` (every PR/main push: build gcd, lint, CODEOWNERS, render, PR comment = `gcd show --diff --against` the rendered branch, push branch `rendered`), `cli.yaml` (only on `platform/cli`, renderer fixtures, appsets/bootstrap/infrastructure/cluster, `.mise.toml` changes: `test-cli` + regions drift), `tf-module.yaml` (tf-module-push script/example). Shared setup: `.github/actions/setup-go`. A new test reading another path: add it to `cli.yaml` `paths`.

## Rules
- **Fully local and reproducible**: `mise run up` must go from nothing to working with no manual steps (after the repo is pushed: `rendered/` syncs from GitHub branch `rendered`). After changing cluster, infrastructure or auth, prove it with a clean `mise run up`; `mise run infra` alone doesn't catch bootstrap-order bugs.
- **GitOps-only**: no UI user gets create/edit/delete rights in Kargo or Argo CD. All UI access is generated per system from `owners.yml` emails, matched on the OIDC `email` claim; never IdP groups/roles (ADR 0011): Kargo project per app `kargo-<sys>-<app>` (ADR 0029) with `promote-<stage>` (that stage's promoters) + `viewers` (read project) and `users-<sys>` global SAs (list projects), Argo CD AppProject `viewer-<app>` roles (read-only); no global viewers or global policies. Sole exception: admin emails hardcoded in Kargo + Argo CD values + break-glass guard `ADMIN_EMAILS` (ADR 0009); Kargo edits still blocked by `gitops-only.yaml`.
- **Break-glass (ADR 0013, `docs/break-glass.md`)**: Kargo checks `promote` for approving unverified Freight too, so the approve endpoint goes through a Traefik ForwardAuth guard (`platform/infrastructure/break-glass/`) that allows only the Stage's `example.com/break-glass` emails (from `owners.yml` `breakGlass`) + admins.
- **Pin versions**: tools in `.mise.toml`, charts in `platform/appsets/addons/<addon>.yaml` `targetRevision`, infra images by tag or digest. App images: `app.yml` `image` is a repo only; Kargo discovers SemVer tags and Freight pins the digest (ADR 0016).
- **Idempotent tasks**: every command in `platform/cli` (and any remaining `platform/mise-tasks/*` script) must be safe to re-run. `go vet` + `go test` in `platform/cli` run in `mise run test` and CI (`cli.yaml`).
- **Secrets** come from fnox (`.fnox.toml`, macOS keychain); `mise run creds` puts GitHub App creds + the GHA dispatch PAT (`github-actions`, ADR 0020) in `kargo-shared-resources` (Argo CD needs none: public repo). Nothing secret in git.
- Teams write only YAML (ADR 0006). Every option is a YAML field: add it to the schema in `system/schema.go` (or `validate.go` for targets/owners), read it in `model.go`. User-facing validation messages are the `Diag` strings in `validate.go` / `model.go`.
- Output field order is whatever the Go builders write (`emit.M(...)` is ordered); the golden test guards it.
- **VCS is jj.** No git mutations, no pushes.

## Changing infrastructure
- Manual runbooks (break-glass, resetting Kargo state): `docs/operations.md`.
- What Argo CD pulls from git vs what needs `mise run infra` / `up`: `docs/sync-boundaries.md`. Add-ons are git-sourced from `main` (ADR 0017): **push** before they take effect.
- Edit `platform/infrastructure/<addon>/` or `platform/appsets/`, push to `main`; Argo CD syncs it. `mise run infra` only bootstraps/waits.
- **New addon**: `platform/infrastructure/<addon>/` (values/manifests) + `platform/appsets/addons/<addon>.yaml` (copy one with the same cluster selector), and add it to the hub wait list in `infra.HubWaitRest` (`platform/cli/internal/infra`) if hub-side. All add-ons start together: dependencies (CRDs, webhooks, OIDC issuer) are absorbed by unlimited sync retries; only host steps wait on specific apps.
- **Regional addons** (`argocd-region`, `kargo-shard`): cluster gen selecting `example.com/role: region`, region bits in `valuesObject` with `{{.values.region}}`. Hub side per region (`kargo-shard-hub`, `argocd-region-hub`): matrix hub × `rendered/infrastructure/regions/*.json` (`{{.region}}`). The hub never gets region credentials (ADR 0015).
- **New region**: add a cluster in `platform/cli/internal/system/config.go`, `mise run regions`, merge (CI renders), then a clean `mise run up`.
- **Expose a UI**: Traefik `IngressRoute`, not `Ingress`.
- **New OIDC client**: add it to `platform/infrastructure/keycloak/manifests/kargo-realm.json` as a public client with `pkce.code.challenge.method: S256` (it gets the `email` claim by default; don't add group mappers).

## Gotchas
Read `docs/gotchas.md` before debugging networking (ports, `*.localhost`, DNS), login/OIDC errors, Argo CD health or sync status, or Kargo API/RBAC behaviour. When you fix a new non-obvious failure, add it there.

## Agent skills

### Issue tracker

Local markdown under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`) as `Status:` lines. See `docs/agents/triage-labels.md`.

### System config

Editing `systems/` or `teams/`: `.claude/skills/system-config/SKILL.md`.

### Domain docs

Single-context: root `GLOSSARY.md` + `docs/adr/`. See `docs/agents/domain.md`.
