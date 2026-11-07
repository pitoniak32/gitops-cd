# Platform admins: a hardcoded email list with admin in Kargo and Argo CD

Admin emails (`admin@example.com`) map to Kargo admin (`api.oidc.admins.claims.email`) and Argo CD `role:admin` (`policy.csv`, `g, <email>, role:admin`). This is the only global grant. All other access stays generated per capability (ADR 0007, ADR 0011). This was originally Keycloak group `platform-admins`; it moved to email because an internal IdP's groups aren't under our control (ADR 0011).

## Considered Options

- **Built-in local admins** (Argo CD `admin`, Kargo `adminAccount`): rejected. They are shared passwords outside Keycloak, and one switch gives everyone admin.
- **Generate from `teams/`** (e.g. `platform: true`): deferred. The infrastructure values are static and not rendered from the registry; a short hardcoded list is enough for now. Keep the Kargo and Argo CD lists identical.
- **Exempt admins from `gitops-only.yaml`**: not possible. Every Kargo UI/API write runs as the `kargo-api` ServiceAccount, so the policy can't see who the user is.

## Consequences

- Kargo admins can view, promote and approve in every project. Create/edit/delete still go through gitops.
- Argo CD admins can do anything in the UI, including infrastructure apps. Apps with `selfHeal` revert manual edits.
- Adding an admin is a platform-approved change to the email list in both `platform/infrastructure/kargo/values.yaml` and `platform/infrastructure/argocd/values.yaml`.
