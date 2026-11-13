# Demo notes

Keep these in mind while recording. Details in `docs/gotchas.md`.

## Before recording

- [ ] Clean `gcd up`, then wait **at least 5 min** before recording (workload apps lag, below).
- [ ] `gcd creds`: refresh the GitHub App key + GHA PAT. Pre-deploy checks and the GHA, Terraform and Cloud Run targets call GitHub for real; an expired token fails live.
- [ ] `gcd kargo-restore`: Stages are empty after `up` (history lives only in hub etcd).
- [ ] Dry run: assemble Freight once, promote dev → prod for one app. Catches stuck Stages (failed auto-promotions aren't retried).
- [ ] Logins ready (password = username): `doug` (payments promoter), `bob` (viewer), `grace` (break-glass), `alice` (search), `admin`.
- [ ] Browser trusts the local CA (`gcd certs`) (no TLS warning on `*.localhost`; restart the browser if an old tab warns).

## Gaps: say them before someone asks

- **"Verified" just means it deployed.** No AnalysisTemplates: verification = Argo CD sync + health. GHA, Terraform and Cloud Run Stages always show Healthy after a successful run (ADR 0020). Framing: "verification hook is where you'd plug in smoke tests / SLO checks".
- **Secrets aren't delivered on kind.** `secret:` values (`APP_SECRET`, `LD_SDK_KEY`) are validated, not rendered: no secret store locally.
- **Kargo history is lost on cluster rebuild.** Git keeps deployed state; `kargo-restore` rebuilds Stage state from `env/*` branches.
- **Kargo lists all project names to every user.** Contents are restricted; use the "My projects" filter. Expect the question in the access segment.
- **Local only.** kind clusters, Keycloak in dev mode, local-CA certs. Real-cluster concerns (secret store, HA, real IdP) are out of scope.

## Timing / "looks broken but isn't"

- **Workload apps missing right after `up`**: AppSet generated them before the AppProject existed; requeues in ~3 min. Skip the wait:
  `kubectl --context kind-region-<r> -n argocd annotate applicationset workloads argocd.argoproj.io/application-set-refresh=true`
- **Argo CD sync `Unknown` / ComparisonError "revision not found"** until a target's first promotion (its `env/<sys>/<app>/<target>` branch doesn't exist yet). Normal for prod. Promote first or explain it.
- **Old Freight can't be promoted** (pre-ADR 0016: no image, old layout). Assemble fresh Freight on camera: newest commit + image tag.
- **No auto-promotion until Freight exists.** Nothing reaches dev until someone assembles Freight, even with `autoPromote`.
- **"Auto-promotion paused"** after a manual rollback to older Freight. Intended: show it on purpose. "Resume auto-promotion" in the UI releases it.
- **Approve endpoint only for break-glass + admins**: if `grace` can't approve, the Stage lacks the `example.com/break-glass` annotation (render not on branch `rendered` yet).
- **Config changes need a merge + CI render** before Kargo sees them: allow ~1-2 min after merge for the `render: <sha>` commit on branch `rendered`, then the Warehouse picks it up.

## Auto-promotion (manual by default)

- Default: manual. `stages/<s>.yml` `autoPromote` sets it for every app; `app.yml` `stages.<s>.autoPromote` overrides for one app.
- Demo: payments `dev` stage is manual; `my-app` opts in (`systems/payments/apps/my-app/app.yml`), `worker` doesn't. Search `dev` stage sets it at stage level.
- Show `gcd show payments/my-app --stage dev -s` → `autoPromote: true  # apps/my-app/app.yml:…` vs worker `# default: manual`.
- Show lint refusing it on prod: set `autoPromote: true` under `prod` in an app.yml → `stage prod deploys to prod targets; autoPromote not allowed`.

## Suggested order

1. PR with a deliberate lint error → annotations on the changed line → fix.
2. Rendered-diff comment on the PR → merge.
3. `gcd show payments/my-app -s`: merged config with sources.
4. Kargo as `doug`: assemble Freight → my-app dev auto-promotes, worker dev waits for a click.
5. Prod: pre-deploy check runs, then deploys; Argo CD regional UI shows it.
6. Break-glass as `grace`: approve Freight that skipped a stage.
7. `bob`: read-only in Kargo + Argo CD; nobody can edit in the UIs.
8. Rollback → "Auto-promotion paused".
