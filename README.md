I want to make this project into an example of Kargo and how to use it with GitHub Actions

## Usage

1. `mise install` - get required tools
1. `mise run up` - tear down any existing demo cluster, create fresh kind one, install platform + example project
1. open https://kargo.localhost, log in via local keycloak:
   - `doug` / `doug` - promoter (example-project)
   - `alice` / `alice`, `bob` / `bob` - viewers
   - keycloak console: http://keycloak.localhost (`admin` / `admin`)
1. argo cd: https://argocd.localhost, "Log in via Keycloak" with same users (doug can sync, others read-only); no local login

Other tasks:

- `mise run down` - delete demo cluster
- `mise run platform` - bootstrap argo cd, which then installs/manages traefik, cert-manager, argo-rollouts, keycloak, kargo and itself (idempotent)
- `mise run render [app...]` - print the argo cd Applications that `platform` applies
- `mise run project` - (re)apply `example-project/` (secrets via fnox)

Notes:

- kubeconfig is project-local (`.kubeconfig`); `kubectl` inside this dir targets the demo cluster
- host 80/443 (loopback only) -> traefik NodePorts 30080/30443
- platform: `platform/<app>/` = `app.yaml` (argo cd Application, pinned chart) + optional `values.yaml` + optional `manifests/` (kustomize, wrapped in `bedag/raw` chart)
- no git source yet: values/manifests inlined into Applications at render time; swap `source` to a repo later
- auth is fully local: keycloak realm/client/users in `platform/keycloak/manifests/kargo-realm.json` (re-imported each keycloak start, no persistence)
- UI/API is promote/approve only, nobody (incl. admins) can create/edit/delete resources there: no OIDC admin mapping + `platform/kargo-config/manifests/gitops-only.yaml` admission policy on kargo-api
- issuer `http://keycloak.localhost/realms/kargo`; coredns rewrites `keycloak.localhost` -> traefik so kargo-api sees the same issuer as the browser
