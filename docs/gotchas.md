# Gotchas

Non-obvious failures hit while building this, with the fix in place. Check here before changing networking, auth, or the infra task.

## Networking

- **Tailscale holds `*:443` on the host.** kind binding `0.0.0.0:443` fails with "address already in use". Fix: `platform/cluster/kind.yaml` binds `127.0.0.1` and `::1` only.
- **`::1` broken on kind's default network.** The `kind` docker network is dual-stack, so Docker Desktop forwards `::1` to the node's IPv6, where the IPv4-only cluster has no NodePort. Browsers resolve `*.localhost` to `::1` first. Fix: `mise run up` creates an IPv4-only `kargo-demo` network (`KIND_EXPERIMENTAL_DOCKER_NETWORK`).
- **`keycloak.localhost` inside the cluster.** The issuer must be the same URL for the browser and in-cluster clients (kargo-api, argocd-server). A CoreDNS `rewrite` (in `platform/mise-tasks/infra`) points it at the Traefik service.
- **curl in pods ignores that rewrite**: curl resolves `*.localhost` to loopback itself. Test in-cluster DNS with `busybox` `nslookup`/`wget`.

## Keycloak

- **Realm file must be named `<realm>-realm.json`** (`kargo-realm.json`), or Keycloak exits with "File name / realm name mismatch".
- **Imported users get no default roles.** Users need `realmRoles: [default-roles-kargo]` (includes `offline_access`). Without it the Kargo UI's token exchange fails, and because Keycloak's error response has no CORS header, the browser shows a misleading **CORS error**. Check Keycloak logs for the real error.
- **Issuer is plain `http://`**, so kargo-api/argocd-server need no custom CA. Both UIs accept it.
- **Dev mode, no persistence.** The realm is re-imported on every start; the kustomize `configMapGenerator` hash restarts Keycloak whenever the realm file changes. Edits made in the console are lost.
- **Scripted logins with curl**: Keycloak sets `Secure` cookies, which curl drops over `http://` (browsers keep them on `*.localhost`). Pass the cookies manually with `-H Cookie:`. The `kargo` client allows the password grant for quick token tests.

## Argo CD

- **Ingress never turns Healthy.** NodePort Traefik publishes no address, so Argo CD leaves an `Ingress` Progressing forever. Expose UIs with Traefik `IngressRoute` instead (Argo CD's is in `extraObjects`).
- **Bootstrap skips `extraObjects`.** At first install the Traefik CRDs don't exist yet; the self-managing `argocd` app adds the IngressRoute later. Otherwise keep `platform/infrastructure/argocd/values.yaml` identical for bootstrap and self-management.
- **Stale status after apply.** Right after `kubectl apply` of a changed Application, Argo CD can still report the old `Synced`. `platform/mise-tasks/infra` requests a refresh and waits for it to be processed before waiting on Synced/Healthy.
- **Stale health after Argo CD restarts itself.** Changing Argo CD's own config can leave another app stuck `Progressing`. Fix: `kubectl -n argocd annotate app <app> argocd.argoproj.io/refresh=hard --overwrite`.
- **Kargo chart is OCI** (`ghcr.io/akuity/kargo-charts`); it needs the `enableOCI` repository entry in Argo CD values.

## Kargo

- **REST API** is `/v1beta1/...` (e.g. `GET /v1beta1/projects`, `POST /v1beta1/resources`). Connect-style paths return 405.
- **Role claims** map via annotation `rbac.kargo.akuity.io/claims: '{"email":[...]}'` on the project's `kargo-admin` / `kargo-promoter` / `kargo-viewer` ServiceAccount.
- **Default `additionalScopes` is `[groups]`**, which the Keycloak realm doesn't define; it's overridden to `[]`.
- **Creating a resource with no `metadata.name` returns 500, not 403** (Kargo bug: name checked before authz). Permissions still hold.
- **The gitops-only policy keys on the `kargo-api` ServiceAccount.** All UI/API writes are made by that SA, so the policy covers every user, admins included. Writes from `kubectl` as cluster admin are the intended path and are not affected.
- **Example promotions call GitHub for real** (`gha-trigger` step). Use a throwaway Stage pointed at an in-cluster echo server when testing step behaviour.

## gitops-cd (rendered capabilities)

- **Push before `up`.** `platform/bootstrap/rendered.yaml` syncs `rendered/` from GitHub `main`, and Kargo pushes `env/<cap>/<app>/<target>` branches there. Without the repo pushed, `rendered-argocd`/`rendered-kargo` show ComparisonError and promotions fail at `git-push`.
- **Kargo git creds are shared.** The Kargo chart creates `kargo-shared-resources` (`global.sharedResources.namespace`); a `kargo.akuity.io/cred-type: git` Secret there serves every project. `mise run creds` writes it from fnox using the GitHub App (`githubAppID`, `githubAppInstallationID`, `githubAppPrivateKey`). Argo CD gets no repo secret: the repo is public, and a bad credential there breaks Argo CD's reads (`failed to list refs: authentication required`).
- **Don't define `clientScopes` in the realm JSON.** That replaces Keycloak's defaults (profile, email…) and drops the `email` claim all access depends on (ADR 0011). Requesting a scope the realm lacks (e.g. Kargo's default `groups`) fails login, so Kargo keeps `additionalScopes: []`.
- **Emails must match exactly.** Kargo claim mapping and Argo CD `scopes: [email]` compare strings as-is, so `owners.yml` only accepts lowercase emails; the IdP must issue lowercase `email` claims too.
- **Sync `Unknown` until a target's first promotion.** Each Argo CD app tracks `env/<cap>/<app>/<target>`, which Kargo creates on the first promotion of that target stage. Until then: `ComparisonError: unable to resolve ... revision not found`, sync `Unknown`, health `Healthy` (no resources). Expected for prod, which never auto-promotes. Destination `Unknown` in the UI for non-admins is separate: teams have no `clusters, get`.
- **Old stage branches are orphaned.** Before ADR 0012 branches were `env/<cap>/<app>/<stage>`; nothing uses them now. Delete them on GitHub by hand (Kargo/CI never delete branches).
- **Render the Kargo project namespace.** Argo CD dry-runs every resource before applying any, so leaving `kargo-<cap>` for Kargo to create from the Project fails the whole first sync forever. The renderer emits `Namespace kargo-<cap>` labeled `kargo.akuity.io/project: "true"` (Kargo adopts it); `rendered-kargo` keeps a sync retry as a backstop. (`kubectl apply --dry-run=server` still reports NotFound for these; Argo CD doesn't.)
- **AppProjects whitelist `Namespace`.** Generated Applications use `CreateNamespace=true`, which needs the cluster-scoped `Namespace` kind allowed in the AppProject.
- **Kargo project names are visible to all users with any project.** `GET /projects` needs cluster-scoped `list projects` (`kargo-user`), and Kargo 1.12 only filters by access with `?mine=true` (UI "My projects"). Contents stay limited to projects where the user is mapped to a ServiceAccount (`viewers`, `promote-<app>`).
- **Kargo global ServiceAccounts.** `users-<cap>` SAs live in `kargo-shared-resources`, which must be listed in `api.oidc.globalServiceAccounts.namespaces`, or their claims never match.
- **GitHub App 401 "A JSON web token could not be decoded"** (Warehouse: `error minting installation access token`): the private key doesn't belong to the App ID (wrong App, or a revoked/regenerated key). Generate a new key in the App settings and `fnox set GITHUB_APP_PRIVATE_KEY`.
- **Approving Freight checks `promote`, not a separate verb.** `POST /v1beta1/projects/<p>/freight/<f>/approve?stage=<s>` (the UI's "Manual approval required") is allowed for anyone with `promote` on the Stage, and the kargo-api ServiceAccount writes the status itself. RBAC and admission can't restrict it; the break-glass guard does (ADR 0013). If no one but admins can approve, the Stage has no `example.com/break-glass` annotation yet (not rendered on `main`).
- **Promote needs two grants.** Kargo's promote API checks `promote` on the Stage *and* creates the Promotion as the user, so owner roles also need `create` on `promotions` (as Kargo's built-in `kargo-promoter` Role has). Missing it: `promotions.kargo.akuity.io is forbidden: create is not permitted`.
- **Failed auto-promotions aren't retried.** Kargo auto-promotes a given Freight once; if that Promotion errors (e.g. bad git creds), promote it manually or wait for new Freight.
- **Browser TLS warning on `*.localhost`.** Traefik's default cert is self-signed unless the `local-tls` secret (`mise run certs`, mkcert) exists. First `mkcert -install` asks for your macOS password to trust the CA in the keychain (Chrome/Brave; Firefox would need `nss`). Restart the browser if an old tab still warns. `*.localhost` wildcards are rejected by browsers, so hosts are listed explicitly: a new UI host must be added in `platform/mise-tasks/certs`.
