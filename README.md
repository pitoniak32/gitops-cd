I want to make this project into an example of Kargo and how to use it with GitHub Actions

## Usage

1. `mise install` - get required tools
1. `mise run up` - tear down any existing demo cluster, create fresh kind one, install platform + example project
1. open https://kargo.localhost

Other tasks:

- `mise run down` - delete demo cluster
- `mise run platform` - (re)install traefik, cert-manager, argo-rollouts, kargo (idempotent)
- `mise run project` - (re)apply `example-project/` (secrets via fnox)

Notes:

- kubeconfig is project-local (`.kubeconfig`); `kubectl` inside this dir targets the demo cluster
- host 80/443 (loopback only) -> traefik NodePorts 30080/30443
- chart versions pinned in `mise-tasks/platform`
