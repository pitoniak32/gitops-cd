# Regional Argo CD UI, OIDC, deep links
Status: needs-triage
Blocked by: 01, 02, 03, 04 (do after the core works)

- **Routing:** hub Traefik IngressRoutes for `argocd-<region>.localhost` to the region NodePort.
- **Keycloak:** a public PKCE client per region, plus keycloak.localhost resolution from the regions (spec "Risks").
- **Access:** email-only RBAC (ADR 0011) and admin emails (ADR 0009).
- **Deep links:** Kargo `api.argocd.urls`.

Done when: a promoter can log in to their region's Argo CD read-only, and Kargo deep links open the right instance.

## Comments
- Impl 2026-11-08. Hub addon `argocd-region-hub` (templated `__SHARD__`, Application `argocd-ui-<r>`): ExternalName Service `region-<r>-control-plane:30081` + IngressRoute `argocd-<r>.localhost`; traefik `allowExternalNameServices: true`. Regional argocd-server NodePort 30081, OIDC/RBAC mirror hub, `argocd-region/values.yaml` templated (`infra-render` + `infra` helm bootstrap substitute). New task `region-dns` (called by `infra`): CoreDNS rewrite `keycloak.localhost` -> selectorless Service `kube-system/hub-traefik` (80 -> EndpointSlice hubIP:30080, IP re-resolved each run). `infra-render` generates Keycloak `argocd-<r>` clients and Kargo `api.argocd.urls` from `platform/cluster/regions`; `certs` adds hostnames. Default shard "" unmapped. Verified: clean `up`, `infra` re-run, 200 on both UIs, oidcConfig clientIDs, issuer from region pods, /auth/login redirect to keycloak. Not tested: interactive browser login. See docs/gotchas.md.
