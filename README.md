# kargo-demo

Local, reproducible [Kargo](https://kargo.io) demo: a kind cluster where Argo CD installs the platform (Traefik, cert-manager, Argo Rollouts, Keycloak, Kargo) and Kargo promotes an example app, triggering GitHub Actions in [kargo-demo-gitops](https://github.com/pitoniak32/kargo-demo-gitops).

## Quickstart

```sh
mise install   # pinned tools
mise run up    # tear down + fresh cluster + platform + example project (~5 min)
mise run down  # delete everything
```

| URL | Login |
|---|---|
| https://kargo.localhost | Keycloak user |
| https://argocd.localhost | "Log in via Keycloak" (no local accounts) |
| http://keycloak.localhost | console: `admin` / `admin` |

Users (password = username):

| User | Kargo | Argo CD |
|---|---|---|
| `doug` | view + promote/approve in `example-project` | view + sync |
| `alice`, `bob` | view | view |

Nobody can create, edit or delete resources through the Kargo or Argo CD UIs; changes go through git/manifests.

## How it works

- **Cluster**: kind (`cluster/kind.yaml`), host `127.0.0.1`/`::1` ports 80/443 → Traefik. Kubeconfig is project-local (`.kubeconfig`).
- **Platform**: `mise run platform` helm-installs Argo CD once, then applies one Argo CD `Application` per `platform/<app>/` and waits for each to be Synced + Healthy. Argo CD then manages everything, including itself.
  - `app.yaml`: the Application (pinned chart)
  - `values.yaml`: helm values (optional)
  - `manifests/`: kustomize dir of plain manifests, deployed via the `bedag/raw` chart (optional)
  - No git source yet: `mise run render` inlines values/manifests into the Application. Point `source` at a repo later.
- **Auth**: Keycloak realm in `platform/keycloak/manifests/kargo-realm.json` (users, `kargo` + `argocd` public PKCE clients), re-imported on every Keycloak start.
- **GitOps-only**: no Kargo admin claim mapping; doug's promote rights come from `example-project/rbac.yml`. An admission policy (`platform/kargo-config/manifests/gitops-only.yaml`) blocks the Kargo API from creating/editing/deleting anything except Promotions, even for admins.
- **Example project**: `example-project/` applied by `mise run project`; secrets come from fnox (macOS keychain).

## Tasks

`mise tasks` lists them: `up`, `down`, `platform`, `project`, `render`.

## Gotchas

See [docs/gotchas.md](docs/gotchas.md): networking, Keycloak, Argo CD and Kargo quirks found while building this.
