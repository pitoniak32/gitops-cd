# gitops-cd

Rendered-manifest continuous delivery, end to end on a laptop: teams describe apps in YAML per
**system**, a Go renderer (`gcd`) validates and renders Kubernetes manifests plus Kargo/Argo CD
config, and a local kind cluster runs Argo CD + [Kargo](https://kargo.io) to promote them.

## Prerequisites

Everything else (kind, Helm, oras, the local CA, keychain secrets) is built into gcd; `gcd --help`
lists every command. App teams that only edit YAML need just Go and git or jj.

| Tool | Needed for | Install |
|---|---|---|
| [mise](https://mise.jdx.dev) + Go (pinned in `.mise.toml`) | building gcd (`mise run build`), `gcd test`; puts `.bin` on `PATH` | `brew install mise`, then `mise install` |
| git or [jj](https://jj-vcs.github.io/jj/) | `lint --changed`, `show --diff` | `brew install jj` |
| macOS | keychain secrets (`secret`, `creds`), trusting the local CA (`certs`), `open` | Linux works for lint, render, show and test only |
| Docker (Docker Desktop or similar) | the kind clusters: `up`, `down`, `infra` | [docker.com](https://www.docker.com/products/docker-desktop/) |
| [gh](https://cli.github.com), logged in | `kargo-restore`; `tf-module-push` (needs `write:packages`: `gh auth refresh -h github.com -s write:packages`) | `brew install gh` then `gh auth login` |
| GitHub App + PAT, in the keychain | `up` / `creds`: Kargo pushes `env/*` branches and dispatches workflows | see Quickstart |
| kubectl, k9s (optional) | looking at the clusters yourself; `eval "$(gcd env)"` points them at `.kubeconfig` | `brew install kubectl k9s` |
| [Rex](https://www.superlogical.com/rex) (optional) | tab status and progress while long commands run | any terminal works without it |

## Quickstart

```sh
mise install && mise run build   # Go, then gcd to .bin/gcd (on PATH while mise is active; rerun after changing platform/cli)
# first time:
#   - push this repo to github.com/pitoniak32/gitops-cd and wait for CI's first render: it creates branch
#     rendered (rendered/ syncs from there); `up` times out without it
#   - a GitHub App installed on that repo with Contents read/write (Kargo pushes
#     env/<sys>/<app>/<target> branches) and a PAT (ADR 0020); store them in the macOS keychain:
#     gcd secret set GITHUB_APP_ID   (also GITHUB_APP_INSTALLATION_ID, GITHUB_PAT;
#     gcd secret set GITHUB_APP_PRIVATE_KEY < key.pem)   gcd secret list checks them
gcd up    # tear down + fresh hub + region clusters + infrastructure + bootstrap (~9 min)
            # long gcd commands (up, lint, render, …) report their phase to the terminal tab (OSC 7501, e.g. Rex); GCD_NO_STATUS=1 turns it off
eval "$(gcd env)"   # KUBECONFIG=.kubeconfig for kubectl/k9s (gcd itself always uses it)
gcd open  # open every UI below in the browser (--print: just list the URLs)
gcd down  # delete everything
```

| UI | URL | Shows | Login |
|---|---|---|---|
| Kargo | https://kargo.localhost | all projects, promotions (one control plane on the hub) | Keycloak user |
| Argo CD (hub) | https://argocd.localhost/applications | infrastructure apps only, no workloads (admin) | "Log in via Keycloak" (no local accounts) |
| Argo CD (us-east4) | https://argocd-us-east4.localhost/applications | workload apps of targets on cluster `us-east4` | "Log in via Keycloak" |
| Argo CD (dc1) | https://argocd-dc1.localhost/applications | workload apps of targets on cluster `dc1` | "Log in via Keycloak" |
| Keycloak | http://keycloak.localhost/admin/master/console/ | users, OIDC clients | `admin` / `admin` |

One regional Argo CD per line in `platform/cluster/regions` (`https://argocd-<region>.localhost`); `gcd open` picks new ones up.
Kargo's Stage view links each target to its region's Argo CD.

HTTPS uses a local CA (`gcd certs`, run by `infra`; an existing mkcert CA is reused); the first run asks for your macOS password to trust it.

Users (password = username):

| User (email `<user>@example.com`) | Listed in | Kargo | Argo CD |
|---|---|---|---|
| `doug` | payments `promoters` | view `kargo-payments-<app>`; assemble Freight, promote verified Freight | view `payments` apps |
| `alice` | search `promoters` | view `kargo-search-<app>`; assemble Freight, promote verified Freight | view `search` apps |
| `bob` | payments `viewers` | view `kargo-payments-<app>` | view `payments` apps |
| `grace` | payments `breakGlass` | view `kargo-payments-<app>`; promote, and approve unverified Freight (break-glass, ADR 0013) | view `payments` apps |
| `admin` | infra values (ADR 0009) | admin: view/promote/approve all projects (edits still gitops-only) | admin: everything, incl. infrastructure apps |

Nobody can create, edit or delete resources through the Kargo or Argo CD UIs; changes go through git.
The only things created through the Kargo UI are Promotions and Freight (assembled from discovered commits/images).
Access is by email (OIDC `email` claim), never IdP groups (ADR 0011). Users see only systems whose
`owners.yml` lists them as `promoters` or `viewers`, per app: in Argo CD those apps (read-only, generated
AppProject `viewer-<app>` roles; syncing is done by Kargo), in Kargo those apps' projects (one per app,
`kargo-<sys>-<app>`, ADR 0029; generated `viewers` and per-stage `promote-<stage>` roles).
Kargo's project list still shows other projects' names (listing needs cluster-scoped read); use the
"My projects" filter. Infrastructure apps aren't visible in the Argo CD UI (use kubectl).

## Layout

```
systems/<sys>/           one business system (YAML only)
  owners.yml                  codeOwners, promoters per stage, viewers, breakGlass, apps overrides (platform-approved)
  stages/<stage>.yml          promotion graph: dependsOn, autoPromote (default manual; app.yml can override per app)
  targets/<target>/target.yml cluster / namespace / identity, reused by apps
  targets/<target>/environment-variables/<name>.yml
                              env vars for that target; apps opt in: envFrom: [<name>]
  apps/<app>/app.yml          image, replicas, env, envFrom, resources, per stage → target
teams/<team>.yml              githubTeam, for CODEOWNERS (team or platform approves)
rendered/                     generated; lives on branch `rendered` (ADR 0002), gitignored on main
  apps/<sys>/<app>/<stage>/<target>/{deployment,manifests}.yaml
  argocd/deploys/<region>/<sys>/<app>-<target>.json  params for the workloads ApplicationSet
  argocd/projects/<region>/<sys>.yaml                 per-region AppProject
  infrastructure/regions/<region>.json                region list for hub-side ApplicationSets
  kargo/<sys>.yaml            Project, Warehouses, Stages, promote RBAC
  pipelines/<sys>.json        neutral promotion graph
  codeowners/<sys>            fragments → .github/CODEOWNERS
platform/                     platform team only; app teams never edit
  render/                     renderer test fixtures + golden, rendered/ README
  bootstrap/root.yaml         root Application, same in every Argo CD: syncs platform/appsets/ (ADR 0017)
  appsets/                    ApplicationSets (cluster generator): addons/<addon>.yaml, rendered-kargo, projects, workloads
  infrastructure/<addon>/     add-on config read from git by its ApplicationSet (values.yaml, manifests/)
  cluster/kind.yaml           hub kind cluster (region.yaml + regions: regional clusters, no host ports)
  cli/                        Go CLI `gcd` (ADR 0018): setup (up, down, infra, certs, creds, secret, …), render, lint, test; the renderer is `internal/system` (ADR 0030)
```

## Editing

Plain YAML, no editor schema: `gcd lint` (also in CI, as PR annotations) reports every problem as
`file:line:col: message`. `gcd lint --changed` checks only what your branch (and uncommitted work) can affect. See `systems/payments/apps/my-app/app.yml` for an example of every field.

## Ownership

| Action | Who |
|---|---|
| Approve changes under `apps/<app>/` | the app's `codeOwners` |
| Approve changes under `stages/`, `targets/` | union of the apps' `codeOwners` |
| Approve changes to any `owners.yml` | @acme/platform |
| Approve changes to `teams/<team>.yml` | that team or @acme/platform |
| Promote verified Freight to a stage of an app | that stage's `promoters` (emails; Kargo role per app+stage) |
| Approve Freight that skipped verification ("Manual approval required") | the app's `breakGlass` emails + admins (guard, `docs/break-glass.md`) |
| View the system in Kargo / Argo CD | all stages' `promoters` + `viewers` |

All in the system's `owners.yml`; `apps.<app>` overrides: `codeOwners`/`viewers` replace the list, `promoters` merge per stage (ADR 0007).
Stages that deploy to a `prod` target can't `autoPromote`. `.github/CODEOWNERS` is generated
from `.github/CODEOWNERS.tmpl` + `owners.yml` by `gcd codeowners`; commit it with the owners change (CI checks).

## Flow

1. PR: CI lints every system (errors annotated on the changed lines), renders, and comments the `rendered/` diff.
2. Merge: CI renders and commits `rendered/` to branch `rendered` (`render: <sha>`); main gets no bot commit (ADR 0002).
3. Every Argo CD runs the same root app (`platform/bootstrap/root.yaml` → `platform/appsets/`); cluster-generator labels pick the rest (ADR 0017). Hub: `rendered-kargo` syncs `rendered/kargo` from branch `rendered`. Each region: `argocd-projects` syncs its AppProjects and the `workloads` ApplicationSet makes one Application per app+target from `rendered/argocd/deploys/<region>/`. The hub runs no workloads.
4. Kargo: each app's Warehouse watches `rendered/apps/<sys>/<app>/` and the app's image repo (SemVer tags).
   Nothing is promoted automatically from it: a promoter opens the Warehouse in the Kargo UI and
   **assembles Freight** = a config commit + an image tag (ADR 0016). There's one Kargo Stage per target
   (`<target>`, in the app's project `kargo-<sys>-<app>`, ADR 0029); promoting a target copies its folder to branch `env/<sys>/<app>/<target>`, pins
   the image digest in its `deployment.yaml`, and Argo CD syncs it from there (ADR 0012), or, for
   `deployWith: gha` targets, Kargo dispatches a GitHub Actions workflow and waits for it (ADR 0020). Terraform
   apps (`module:` instead of `image:`, TERRAFORM targets) render `terraform.yaml`, pin the module digest in it
   and always dispatch `.github/workflows/terraform.yaml` (plan + apply; ADR 0021). Image apps may also
   have CLOUD_RUN targets: they render a Knative `service.yaml`, pin the image digest in it and always dispatch
   `.github/workflows/cloud-run.yaml`; no Argo CD, no shard (ADR 0024). Control-flow
   Stages (no steps, always auto) appear only where they help (ADR 0012): `<S>-done` where
   several targets fan out to several, `<D>` where several fan into a multi-target stage or a stage
   needs a one-click release. To release a whole stage, use **Promote to downstream** on its `<D>`
   node, or on its single upstream target when nothing else hangs off it.

## How the cluster works

- **Cluster**: kind (`platform/cluster/kind.yaml`), host `127.0.0.1`/`::1` ports 80/443 → Traefik. Kubeconfig is project-local (`.kubeconfig`).
- **Infrastructure**: `gcd infra` helm-installs Argo CD once per cluster (hub + regions), applies the root app, and waits for each add-on Application (generated by `platform/appsets/addons/`, config read from `platform/infrastructure/<addon>/` on `main`) to be Synced + Healthy. Regions bootstrap in parallel with the hub; host steps wait only for the apps they need (`traefik` before `certs`, `kargo` before `creds`). Argo CD then manages everything, including itself. It also applies git creds (`gcd creds`), TLS (`certs`) and the Kargo shard token handoff (`kargo-shards`). Infra changes need a push to `main`.
- **Auth**: Keycloak realm in `platform/infrastructure/keycloak/manifests/kargo-realm.json` (users with emails, `kargo` + `argocd` + generated `argocd-<region>` public PKCE clients; no groups), re-imported on every Keycloak start.
- **GitOps-only**: promote rights come from the generated `promote-<stage>` roles in each app's project (`email` claim vs `owners.yml` promoters); only the admin emails in the Kargo/Argo CD values get admin (ADR 0009). An admission policy (`platform/infrastructure/kargo-config/manifests/gitops-only.yaml`) blocks the Kargo API from creating/editing/deleting anything except Promotions.
- **Secrets on kind**: the kind clusters (`us-east4`, `dc1`) have no secret store, so `secret:` env values are validated but not rendered.

What Argo CD reconciles from git vs what needs `gcd infra` / `up`: `docs/sync-boundaries.md`. Runbooks (break-glass, resetting Kargo state): `docs/operations.md`.
Docs index (app-team guide, field reference): `docs/README.md`.
Rendering details and merge rules: `platform/render/README.md`. Decisions: `docs/adr/`. Gotchas: `docs/gotchas.md`.
