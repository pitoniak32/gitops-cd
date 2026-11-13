# Break-glass: how it works

Why: ADR 0013. Runbook (audit, check, undo): `docs/operations.md`.

## The rule

| Who | Promote Freight verified upstream | Approve Freight that skipped verification ("Manual approval required") |
|---|---|---|
| `promoters` | yes | no (403) |
| `breakGlass` | yes | yes |
| admins (`ADMIN_EMAILS`) | yes | yes |
| `viewers` | no | no |

Approving only marks the Freight as allowed into that Stage; someone still has to promote it.

## Pieces

| Piece | Where | Does |
|---|---|---|
| `breakGlass: [emails]` | `owners.yml` (system-wide only) | the list |
| Stage annotation `example.com/break-glass: '["grace@example.com"]'` | rendered by `platform/cli/internal/system/kargo.go` on every control-flow and target Stage of the app | carries the list to the cluster (synced by Argo CD) |
| `promote-<stage>` SA claims (app project) | same render | break-glass emails can promote too (needed after approving) |
| IngressRoute `kargo-approve` (priority 1000) | `platform/infrastructure/break-glass/manifests/guard.yaml` | catches only the approve endpoint; beats the normal Kargo route |
| Middleware `forwardAuth` | same | asks the guard before forwarding |
| Guard (`guard.py`, Deployment `break-glass` in `kargo`) | same dir | decides allow/deny, logs |

## A request, step by step

```
browser ── POST /v1beta1/projects/kargo-payments-my-app/freight/<f>/approve?stage=prod
   │
Traefik: kargo-approve route (priority 1000) matches
   │  forwardAuth → GET http://break-glass.kargo.svc:8080/check
   │                 (Authorization + X-Forwarded-Uri copied)
   ▼
guard.py
   1. email = "email" claim of the bearer JWT (signature not checked; see below)
   2. email in ADMIN_EMAILS              → allow ("admin")
   3. no email / path or ?stage missing  → deny
   4. GET Stage kargo-payments-my-app/prod (its SA may only `get` stages)
        lookup fails                      → deny (fail closed)
        email in break-glass annotation   → allow ("break-glass")
        else                              → deny ("not break-glass for this stage")
   5. one JSON log line {decision, reason, email, project, freight, stage}
   │
   ├─ deny  → 403 {"error":"approving Freight that skipped verification is break-glass only …"}
   └─ allow → Traefik forwards to kargo-api
                 kargo-api verifies the token and checks `promote` on the Stage
                 → Freight status: approved for prod
```

Everything else (UI, promote, list, watch) goes through the normal Kargo route; the guard never sees it.

## Why it's safe enough

- **Unchecked JWT signature**: the guard only *narrows* access. A forged token can pass the guard but kargo-api then rejects it.
- **Fail closed**: unknown Stage, API error, missing annotation (e.g. before `rendered/` has it) → only admins pass.
- **Old API path**: the Connect `ApproveFreight` path also goes through the guard; it has no `?stage`, so only admins pass.
- **Bypass**: direct cluster access (`kubectl`, port-forward to kargo-api) skips Traefik. Today only platform has it.

## Changing who can break glass

- Edit `breakGlass` in `owners.yml` → PR (platform approves) → CI renders → Argo CD updates the Stage annotations. No restart; the guard reads the Stage on every request.
- Admins: `ADMIN_EMAILS` in `guard.yaml` (and Kargo + Argo CD values) → `gcd infra`.

## Limits

- Per app, not per stage: the same list applies to all of the app's Stages (dev too).
- No notification yet; the guard's log is the audit trail.
