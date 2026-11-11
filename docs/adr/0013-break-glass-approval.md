# Break-glass approval of unverified Freight

Amended by ADR 0023: break-glass emails are in every per-stage `promote-<app>-<stage>` role.

Status: accepted (started as a POC). How it works: `docs/break-glass.md`.

Promoters should promote Freight that is verified upstream. Approving Freight that skipped verification (Kargo's "Manual approval required" popup) should be limited to a short list of people for critical promotions.

Kargo 1.12 can't make that split. `POST /v1beta1/projects/<p>/freight/<f>/approve?stage=<s>` checks the same `promote` verb on the Stage that promotion does (tested: a viewer gets "promote is not permitted", a promoter gets 200). The kargo-api ServiceAccount then writes Freight status itself, so an admission policy can't tell who the user was either.

So the check sits in front of Kargo:

- `owners.yml` gets `breakGlass: [emails]` (optional; an app file replaces it per field). The renderer:
  - annotates every gate and target Stage of the app with `example.com/break-glass: '[...]'`;
  - adds break-glass emails to `promote-<app>`, because after approving they still have to promote;
  - adds them to the viewers.
- `platform/infrastructure/break-glass/` is a Traefik ForwardAuth guard: a Python stdlib script in a pinned `python` image, reading Stages only. Its own IngressRoute (priority 1000) sends only the approve endpoint (and the legacy Connect `ApproveFreight` path) through the guard. All other traffic is unchanged.
- The guard allows the request if the token's email is in the Stage's annotation, or in `ADMIN_EMAILS`. Otherwise it returns 403 with a reason. Each decision is one JSON log line (Slack notification later).
  - The token signature isn't checked, because kargo-api verifies it right after the guard.
  - If the Stage lookup fails, the guard denies.

## Considered Options

- **Kargo RBAC**: not possible (same verb).
- **Admission policy that blocks every UI approval; platform approves with `kubectl`**: simpler, but break-glass wouldn't be an email list and couldn't be done from the UI.

## Consequences

- Anyone with direct cluster access (`kubectl`, a port-forward to kargo-api) bypasses the guard. Today that's only platform.
- Admin emails now live in three places: Kargo values, Argo CD values, and the guard's `ADMIN_EMAILS`.
- Until `rendered/` on `main` has the annotations, the guard denies everyone except admins (fail closed).

## Removing it

1. Back out the change (`jj backout -r <change>`).
2. `kubectl -n argocd delete app break-glass`. The finalizer deletes the guard, the Middleware and the approve route, so approvals go straight to kargo-api again.
3. `mise run infra`: re-applies the Keycloak realm without `oncall`.
4. Push. CI re-renders without the annotations.
