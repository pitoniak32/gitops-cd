# gitops-cd

Rendered-manifest continuous delivery, end to end on a laptop: teams describe apps in YAML per
**capability**, a CUE renderer validates and renders Kubernetes manifests plus Kargo/Argo CD
config, and a local kind cluster runs Argo CD + [Kargo](https://kargo.io) to promote them.

## Quickstart

```sh
mise install   # pinned tools (kind, helm, kubectl, go, fnox, mkcert, oras, terraform, cue, jq, yq; gcd itself needs only go)
# first time:
#   - push this repo to github.com/pitoniak32/gitops-cd and wait for CI's first render: it creates branch
#     rendered (rendered/ syncs from there); `up` times out without it
#   - a GitHub App installed on that repo with Contents read/write (Kargo pushes
#     env/<cap>/<app>/<target> branches); store it for fnox:
#     fnox set GITHUB_APP_ID / GITHUB_APP_INSTALLATION_ID / GITHUB_APP_PRIVATE_KEY
mise run up    # tear down + fresh hub + region clusters + infrastructure + bootstrap (~9 min)
mise run open  # open every UI below in the browser (--print: just list the URLs)
mise run down  # delete everything
```

| UI | URL | Shows | Login |
|---|---|---|---|
| Kargo | https://kargo.localhost | all projects, promotions (one control plane on the hub) | Keycloak user |
| Argo CD (hub) | https://argocd.localhost/applications | infrastructure apps only, no workloads (admin) | "Log in via Keycloak" (no local accounts) |
| Argo CD (us-east4) | https://argocd-us-east4.localhost/applications | workload apps of targets on cluster `us-east4` | "Log in via Keycloak" |
| Argo CD (dc1) | https://argocd-dc1.localhost/applications | workload apps of targets on cluster `dc1` | "Log in via Keycloak" |
| Keycloak | http://keycloak.localhost/admin/master/console/ | users, OIDC clients | `admin` / `admin` |

One regional Argo CD per line in `platform/cluster/regions` (`https://argocd-<region>.localhost`); `mise run open` picks new ones up.
Kargo's Stage view links each target to its region's Argo CD.

HTTPS uses a local mkcert CA (`mise run certs`, run by `infra`); the first run asks for your macOS password to trust it.

Users (password = username):

| User (email `<user>@example.com`) | Listed in | Kargo | Argo CD |
|---|---|---|---|
| `doug` | payments `promoters` | view `kargo-payments-<app>`; assemble Freight, promote verified Freight | view `payments` apps |
| `alice` | search `promoters` | view `kargo-search-<app>`; assemble Freight, promote verified Freight | view `search` apps |
| `bob` | payments `viewers` | view `kargo-payments-<app>` | view `payments` apps |
| `oncall` | payments `breakGlass` | view `kargo-payments-<app>`; promote, and approve unverified Freight (break-glass, ADR 0013) | view `payments` apps |
| `admin` | infra values (ADR 0009) | admin: view/promote/approve all projects (edits still gitops-only) | admin: everything, incl. infrastructure apps |

Nobody can create, edit or delete resources through the Kargo or Argo CD UIs; changes go through git.
The only things created through the Kargo UI are Promotions and Freight (assembled from discovered commits/images).
Access is by email (OIDC `email` claim), never IdP groups (ADR 0011). Users see only capabilities whose
`owners.yml` lists them as `promoters` or `viewers`, per app: in Argo CD those apps (read-only, generated
AppProject `viewer-<app>` roles; syncing is done by Kargo), in Kargo those apps' projects (one per app,
`kargo-<cap>-<app>`, ADR 0029; generated `viewers` and per-stage `promote-<stage>` roles).
Kargo's project list still shows other projects' names (listing needs cluster-scoped read); use the
"My projects" filter. Infrastructure apps aren't visible in the Argo CD UI (use kubectl).

## Layout

```
capabilities/<cap>/           one business capability (YAML only)
  owners.yml                  codeOwners, promoters per stage, viewers, breakGlass, apps overrides (platform-approved)
  stages/<stage>.yml          promotion graph: dependsOn, autoPromote
  targets/<target>/target.yml cluster / namespace / identity, reused by apps
  targets/<target>/environment-variables/<name>.yml
                              env vars for that target; apps opt in: envFrom: [<name>]
  apps/<app>/app.yml          image, replicas, env, envFrom, resources, per stage → target
teams/<team>.yml              githubTeam, for CODEOWNERS (team or platform approves)
rendered/                     generated; lives on branch `rendered` (ADR 0022), gitignored on main
  apps/<cap>/<app>/<stage>/<target>/{deployment,manifests}.yaml
  argocd/deploys/<region>/<cap>/<app>-<target>.json  params for the workloads ApplicationSet
  argocd/projects/<region>/<cap>.yaml                 per-region AppProject
  infrastructure/regions/<region>.json                region list for hub-side ApplicationSets
  kargo/<cap>.yaml            Project, Warehouses, Stages, promote RBAC
  pipelines/<cap>.json        neutral promotion graph
  codeowners/<cap>            fragments → .github/CODEOWNERS
platform/                     platform team only; app teams never edit
  render/                     CUE renderer (package `render`), schemas, tests
  cue.mod/                    CUE module root
  bootstrap/root.yaml         root Application, same in every Argo CD: syncs platform/appsets/ (ADR 0017)
  appsets/                    ApplicationSets (cluster generator): addons/<addon>.yaml, rendered-kargo, projects, workloads
  infrastructure/<addon>/     add-on config read from git by its ApplicationSet (values.yaml, manifests/)
  cluster/kind.yaml           hub kind cluster (region.yaml + regions: regional clusters, no host ports)
  cli/                        Go CLI `gcd` (ADR 0018): up, down, infra, regions, certs, creds, kargo-shards, kargo-restore, open, render, lint, codeowners, schemas
  mise-tasks/                 one-line wrappers for gcd + render, lint, test, schemas
```

## Editing (any editor, Mac or Windows)

Each YAML file's first line points at `platform/render/schemas/`; editors with yaml-language-server
(VS Code + Red Hat YAML, Zed, nvim, Helix, Sublime LSP-yaml) validate and complete as you type.
See `capabilities/payments/apps/my-app/app.yml` for an example of every field.

## Ownership

| Action | Who |
|---|---|
| Approve changes under `apps/<app>/` | the app's `codeOwners` |
| Approve changes under `stages/`, `targets/` | union of the apps' `codeOwners` |
| Approve changes to any `owners.yml` | @acme/platform |
| Approve changes to `teams/<team>.yml` | that team or @acme/platform |
| Promote verified Freight to a stage of an app | that stage's `promoters` (emails; Kargo role per app+stage) |
| Approve Freight that skipped verification ("Manual approval required") | the app's `breakGlass` emails + admins (guard, `docs/break-glass.md`) |
| View the capability in Kargo / Argo CD | all stages' `promoters` + `viewers` |

All in the capability's `owners.yml`; `apps.<app>` overrides: `codeOwners`/`viewers` replace the list, `promoters` merge per stage (ADR 0023).
Stages that deploy to a `prod` target can't `autoPromote`. `.github/CODEOWNERS` is generated
from `.github/CODEOWNERS.tmpl` + `owners.yml` by `mise run codeowners`; commit it with the owners change (CI checks).

## Flow

1. PR: CI lints every capability (errors annotated on the changed lines), renders, and comments the `rendered/` diff.
2. Merge: CI renders and commits `rendered/` to branch `rendered` (`render: <sha>`); main gets no bot commit (ADR 0022).
3. Every Argo CD runs the same root app (`platform/bootstrap/root.yaml` → `platform/appsets/`); cluster-generator labels pick the rest (ADR 0017). Hub: `rendered-kargo` syncs `rendered/kargo` from branch `rendered`. Each region: `argocd-projects` syncs its AppProjects and the `workloads` ApplicationSet makes one Application per app+target from `rendered/argocd/deploys/<region>/`. The hub runs no workloads.
4. Kargo: each app's Warehouse watches `rendered/apps/<cap>/<app>/` and the app's image repo (SemVer tags).
   Nothing is promoted automatically from it: a promoter opens the Warehouse in the Kargo UI and
   **assembles Freight** = a config commit + an image tag (ADR 0016). There's one Kargo Stage per target
   (`<target>`, in the app's project `kargo-<cap>-<app>`, ADR 0029); promoting a target copies its folder to branch `env/<cap>/<app>/<target>`, pins
   the image digest in its `deployment.yaml`, and Argo CD syncs it from there (ADR 0012), or, for
   `deployWith: gha` targets, Kargo dispatches a GitHub Actions workflow and waits for it (ADR 0020). Terraform
   apps (`module:` instead of `image:`, TERRAFORM targets) render `terraform.yaml`, pin the module digest in it
   and always dispatch `.github/workflows/terraform.yaml` (plan + apply; ADR 0021). Image apps may also
   have CLOUD_RUN targets: they render a Knative `service.yaml`, pin the image digest in it and always dispatch
   `.github/workflows/cloud-run.yaml`; no Argo CD, no shard (ADR 0024). Control-flow
   Stages (no steps, always auto) appear only where they help (ADR 0019): `<S>-done` where
   several targets fan out to several, `<D>` where several fan into a multi-target stage or a stage
   needs a one-click release. To release a whole stage, use **Promote to downstream** on its `<D>`
   node, or on its single upstream target when nothing else hangs off it.

## How the cluster works

- **Cluster**: kind (`platform/cluster/kind.yaml`), host `127.0.0.1`/`::1` ports 80/443 → Traefik. Kubeconfig is project-local (`.kubeconfig`).
- **Infrastructure**: `mise run infra` helm-installs Argo CD once per cluster (hub + regions), applies the root app, and waits for each add-on Application (generated by `platform/appsets/addons/`, config read from `platform/infrastructure/<addon>/` on `main`) to be Synced + Healthy. Regions bootstrap in parallel with the hub; host steps wait only for the apps they need (`traefik` before `certs`, `kargo` before `creds`). Argo CD then manages everything, including itself. It also applies git creds (`mise run creds`), TLS (`certs`) and the Kargo shard token handoff (`kargo-shards`). Infra changes need a push to `main`.
- **Auth**: Keycloak realm in `platform/infrastructure/keycloak/manifests/kargo-realm.json` (users with emails, `kargo` + `argocd` + generated `argocd-<region>` public PKCE clients; no groups), re-imported on every Keycloak start.
- **GitOps-only**: promote rights come from the generated `promote-<stage>` roles in each app's project (`email` claim vs `owners.yml` promoters); only the admin emails in the Kargo/Argo CD values get admin (ADR 0009). An admission policy (`platform/infrastructure/kargo-config/manifests/gitops-only.yaml`) blocks the Kargo API from creating/editing/deleting anything except Promotions.
- **Secrets on kind**: the kind clusters (`us-east4`, `dc1`) have no secret store, so `secret:` env values are validated but not rendered.

What Argo CD reconciles from git vs what needs `mise run infra` / `up`: `docs/sync-boundaries.md`. Runbooks (break-glass, resetting Kargo state): `docs/operations.md`.
Rendering details and merge rules: `platform/render/README.md`. Decisions: `docs/adr/`. Gotchas: `docs/gotchas.md`.
