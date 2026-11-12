# UI access by email, per app and stage

We don't control which groups or roles an internal IdP gives users. So every Kargo and Argo CD grant matches the OIDC `email` claim against emails in `owners.yml` (ADR 0007). Only lowercase `@example.com` emails are allowed, because claim matching is exact. Keycloak has no groups or group mappers. Admins are the only exception (ADR 0009).

Rendered per app, in its Kargo project `kargo-<cap>-<app>` (ADR 0029):

- **`promote-<stage>`** (ServiceAccount + Role + RoleBinding) for each stage the app uses. Its claims are that stage's merged promoters plus `breakGlass`. It grants `promote` on the app's Stages that belong to that stage (target and control-flow Stages, ADR 0012), `promotions`, and `create` on `freights` (Freight assembly, ADR 0016). Kargo grants the union of every matching SA, so a dev+prod promoter gets both.
- **`viewers`**: Kargo's `kargo-viewer` on the project. Its claims are the app's promoters (for the stages it uses, after overrides), its viewers (app override, else the capability's) and `breakGlass`.
- **`users-<cap>`** (global): lists projects for everyone in the capability.
- **Argo CD**: in the capability's AppProject per region, the role `viewer-<app>` gives the same emails read-only access to exactly that app's Applications.

Kubernetes RBAC can't select Stages by label (`promote` takes `resourceNames` only), so the renderer lists Stage names per stage.

## Considered Options

- **IdP groups or roles**: not under our control in an internal IdP.
- **Team members listed in `teams/`**: access would be one hop from the capability, and it would tie access to the code-review unit.
- **A Kargo role per email**: more ServiceAccounts for no gain.
- **Requiring `email_verified`**: an internal IdP may not send it.

## Consequences

- Someone who joins or leaves needs an `owners.yml` PR in every capability they use, and platform approves it.
- A renamed email loses access until git is updated.
- ServiceAccounts scale with apps × stages.
