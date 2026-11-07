# kargo-demo

Local Kargo demo on kind. Argo CD manages the whole platform; Kargo promotes `example-project`. Overview and logins: `README.md`.

## Rules

- **Fully local and reproducible**: `mise run up` must go from nothing to working with no manual steps. After changing cluster, platform or auth, prove it with a clean `mise run up`; `mise run platform` alone doesn't catch bootstrap-order bugs.
- **GitOps-only**: no UI user gets create/edit/delete rights in Kargo or Argo CD. Grant operational rights (promote, approve, sync) per project via manifests, never via admin claims.
- **Pin versions**: tools in `.mise.toml`, charts in `platform/<app>/app.yaml` `targetRevision`, images by tag.
- **Idempotent tasks**: every `mise-tasks/*` script must be safe to re-run.
- **Secrets** come from fnox (`.fnox.toml`, macOS keychain). `example-project/gh_app.yml` is gitignored and local only.
- **VCS is jj.** No git mutations, no pushes.

## Changing the platform

- Edit `platform/<app>/` (`app.yaml`, `values.yaml`, `manifests/`), then `mise run platform`. Inspect output with `mise run render <app>`.
- **New app**: create `platform/<app>/` from an existing `app.yaml`, then add it to the ordered list in `mise-tasks/platform`. Order matters: CRDs, webhooks and the OIDC issuer must exist before the apps that use them. `argocd` stays last.
- **Expose a UI**: Traefik `IngressRoute`, not `Ingress`.
- **New OIDC client**: add it to `platform/keycloak/manifests/kargo-realm.json` as a public client with `pkce.code.challenge.method: S256`.

## Gotchas

Read `docs/gotchas.md` before debugging networking (ports, `*.localhost`, DNS), login/OIDC errors, Argo CD health or sync status, or Kargo API/RBAC behaviour. When you fix a new non-obvious failure, add it there.
