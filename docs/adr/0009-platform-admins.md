# Platform admins: a hardcoded email list

Admin emails (`admin@example.com`) map to Kargo admin (`api.oidc.admins.claims.email`) and Argo CD `role:admin` (`policy.csv`). This is the only global grant. All other access is generated per system (ADR 0011). It is email, not an IdP group, because an internal IdP's groups aren't under our control.

The list lives in three places, which must stay identical: `platform/infrastructure/kargo/values.yaml`, `platform/infrastructure/argocd/values.yaml` (also used by the regional Argo CDs) and the break-glass guard's `ADMIN_EMAILS` (ADR 0013).

## Considered Options

- **Built-in local admins** (Argo CD `admin`, Kargo `adminAccount`): shared passwords outside Keycloak.
- **Generate the list from `teams/`**: deferred. The infrastructure values are static and not rendered, and a short list is enough.
- **Exempt admins from `gitops-only.yaml`**: not possible. Every Kargo UI/API write runs as the `kargo-api` ServiceAccount, so the policy can't see the user.

## Consequences

- Kargo admins can view, promote and approve in every project. Creates, edits and deletes still go through git.
- Argo CD admins can do anything in the UI. Apps with `selfHeal` revert manual edits.
