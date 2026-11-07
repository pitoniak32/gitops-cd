# Platform admins: one OIDC group with admin in Kargo and Argo CD

Keycloak group `platform-admins` maps to Kargo admin (`api.oidc.admins.claims.groups`) and Argo CD `role:admin` (`policy.csv`). It is the only global grant. Team access stays generated per capability (ADR 0007). Realm user `admin` is in this group.

## Considered Options

- **Built-in local admins** (Argo CD `admin`, Kargo `adminAccount`): rejected. They are shared passwords outside Keycloak, and one switch gives everyone admin.
- **Generate from `teams/`** (e.g. `platform: true`): deferred. The infrastructure values are static and not rendered from the registry; one hardcoded group is enough for now.
- **Exempt admins from `gitops-only.yaml`**: not possible. Every Kargo UI/API write runs as the `kargo-api` ServiceAccount, so the policy can't see who the user is.

## Consequences

- Kargo admins can view, promote and approve in every project. Create/edit/delete still go through gitops.
- Argo CD admins can do anything in the UI, including infrastructure apps. Apps with `selfHeal` revert manual edits.
- Membership in `platform-admins` is a platform-approved change to `kargo-realm.json`.
