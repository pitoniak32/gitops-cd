# UI access by email, not IdP groups

Amended by ADR 0023: owners.yml shape, `promote-<app>` → `promote-<app>-<stage>`.

In an internal identity provider we don't control which groups or roles users get. So every Kargo and Argo CD grant matches the OIDC `email` claim against emails listed in git:

- In `owners.yml` (capability level; an app file replaces each field it sets):
  - `owners`: GitHub teams, used for CODEOWNERS (code review) only.
  - `promoters`: emails that can promote the app to any stage and approve its Freight. This renders one Kargo ServiceAccount + Role `promote-<app>` per app.
  - `viewers`: emails with read-only access. Promoters and viewers together make up the `viewers` ServiceAccount (Kargo's `kargo-viewer` Role on the project), `users-<cap>` (lists projects) and the Argo CD AppProject `viewer` role (`scopes: [email]`).
- Only lowercase `@example.com` emails are allowed (`_emailDomain` in `owners.cue`), because claim matching is exact.
- `teams/<team>.yml` keeps only `githubTeam`. It grants no access, so that team or platform can approve changes to its own file (generated CODEOWNERS line). `owners.yml` stays platform-approved, since it controls promotion.
- Admins: emails hardcoded in the Kargo (`admins.claims.email`) and Argo CD (`policy.csv`) values (ADR 0009).
- Keycloak has no groups or group mappers left, which shows nothing depends on them.

Replaces the `idpGroup` / `groups` claim mapping of ADR 0007.

## Considered Options

- **Members listed in `teams/`** (team → emails, `owners.yml` lists teams): rejected. Access would be one hop away from the capability, and access and code review would be tied to the same unit.
- **Kargo role per email**: rejected. It means more ServiceAccounts with no gain. Per-app roles map 1:1 to the YAML.
- **Per-app viewers**: not possible in Kargo, where read access covers the whole project. Argo CD view is per capability as well, for consistency.
- **Requiring `email_verified`**: not required. An internal IdP may not send it.

## Consequences

- Someone who joins or leaves needs an `owners.yml` PR in every capability they use, and that PR needs platform approval.
- A renamed email loses access until git is updated.
