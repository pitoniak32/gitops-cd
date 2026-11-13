# Break-glass approval of unverified Freight

Promoters should promote Freight that is verified upstream. Approving Freight that skipped verification (Kargo's "Manual approval required" popup) is for a short list of people, for critical promotions. How to use it: `docs/break-glass.md`.

Kargo can't make that split. `POST /v1beta1/projects/<p>/freight/<f>/approve?stage=<s>` checks the same `promote` verb as promotion does, and kargo-api then writes Freight status itself, so admission can't see the user. So the check sits in front of Kargo:

- `owners.yml` `breakGlass: [emails]` (system-wide). The renderer annotates every Stage with `example.com/break-glass: '[...]'` and adds the emails to every `promote-<stage>` role, because after approving they still have to promote. They also go into the viewers (ADR 0011).
- `platform/infrastructure/break-glass/` is a Traefik ForwardAuth guard: a Python stdlib script in a pinned `python` image that only reads Stages. Its IngressRoute (priority 1000) sends only the approve endpoint (and the legacy Connect `ApproveFreight` path) through the guard.
- The guard allows the request if the token's email is in the Stage's annotation or in `ADMIN_EMAILS`. Otherwise it returns 403 with a reason. Each decision is one JSON log line. It doesn't check the token signature (kargo-api does that right after), and it denies when the Stage lookup fails.

## Considered Options

- **Kargo RBAC**: not possible, because it's the same verb.
- **An admission policy that blocks every UI approval, with platform approving via `kubectl`**: simpler, but break-glass wouldn't be an email list and couldn't be done in the UI.

## Consequences

- Direct cluster access (`kubectl`, a port-forward to kargo-api) bypasses the guard. Today only platform has it.
- Admin emails are kept in three places (ADR 0009).
- Removal steps: `docs/operations.md`.
