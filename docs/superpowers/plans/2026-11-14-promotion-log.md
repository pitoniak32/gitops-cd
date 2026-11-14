# Promotion Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every Kargo promotion, whatever deploys it (Argo CD or GitHub Actions), POSTs one JSON record to a hub add-on `promotion-log`, which logs it as one JSON line; Alloy ships it to Loki; the CD pipeline dashboard reads it with LogQL.

**Architecture:** A final `http` step in both shared ClusterPromotionTasks (`promote-argocd`, `promote-gha`; ADR 0027) with `if: ${{ always() }}` + `continueOnError: true`, so it records failures too and can never fail a deploy. Steps run in the Kargo controller that owns the target (region shards for K8S targets, the hub default controller for others), so the endpoint is `http://promotions.localhost/promotions`: CoreDNS rewrite to the hub Traefik in every cluster (like `otlp.localhost`), IngressRoute on `web` → Service `promotion-log` on the hub. The service validates the record, adds `receivedAt`, writes `{"msg":"promotion",…}` to stdout, returns 204. No new storage: pod logs → Alloy → Loki (existing pipeline).

**Tech Stack:** Go (stdlib only), local image built + `kind load`ed by `gcd infra` (same mechanism as `github-actions-token`, ADR 0020), Kargo `http` step, LogQL.

**Spec:** this plan + ADR 0032 (written in Task 1). Replaces the reverted Kargo-events counter.

## Global Constraints

- Logging must never block or fail a promotion: `continueOnError: true`, short retry timeout (`retry.timeout: 30s`), `successExpression: response.status == 204`.
- Record fields (JSON, camelCase): `promotion`, `project`, `system`, `app`, `stage` (pipeline stage), `target`, `platform` (`k8s|terraform|cloudrun`, = Stage label `example.com/platform`), `deployWith` (`argocd|gha`), `freight`, `freightAlias`, `warehouse`, `artifact` (`<repo>:<tag>@<digest>`), `commit` (env-branch commit; empty if the promotion failed before git-commit), `actor` (`ctx.meta.promotion.actor`), `automatic` (actor is a `kargo-controller` service account), `rollback`, `result` (`Succeeded|Failed`), `ghaRunURL` (gha only, else empty), `uiURL` (`ctx.uiBaseUrl` + project/stage).
- Endpoint unauthenticated, plain HTTP, local only (same stance as `otlp.localhost`, ADR 0031).
- Pinned: image tag = hash of sources (`TestManifestTag`-style check); Go version = `.mise.toml`.
- Aborted promotions run no steps: not recorded (documented).
- Renderer output order via `emit.M`; golden updated with `UPDATE=1`.

## Review Focus

1. Endpoint down / slow → promotion still Succeeded, step marked errored but ignored.
2. Promotion fails mid-way (e.g. git-push or GHA run fails) → record with `result: Failed`, `commit`/`ghaRunURL` possibly empty, no expression error (use `?? ''`).
3. Automatic promotion (dev auto-promotion) → `automatic: true`, actor = controller SA.
4. Malformed / oversized body → 400, nothing logged; body limit 64 KiB.
5. Record reaches Loki from a region-run promotion (us-east4 shard) via `promotions.localhost`.

---

### Task 1: ADR 0032
- Create `docs/adr/0032-promotion-log.md` (why http step over events counter: per-promotion record with deployWith/platform/actor/result, no counter resets; trade-offs: aborted not recorded, endpoint in path but non-blocking). Link from ADR 0031 consequences.

### Task 2: `promotion-log` service
- Create `platform/infrastructure/promotion-log/{go.mod,main.go,main_test.go,Dockerfile,manifests/{kustomization.yaml,promotion-log.yaml}}`.
- `main.go`: `POST /promotions` → decode into `record` struct (fields above, `DisallowUnknownFields` off, required: `promotion`, `project`, `target`, `result`), set `ReceivedAt`, `json.NewEncoder(os.Stdout)` with `msg:"promotion"`, 204; `GET /healthz` 200; anything else 405/404; `http.MaxBytesReader` 64 KiB.
- Tests (`main_test.go`, httptest): valid → 204 + one stdout line with all fields + `msg`; missing `target` → 400, no line; oversized → 413/400; GET /promotions → 405.
- Manifests: Deployment (image `gitops-cd/promotion-log:<hash>`, `imagePullPolicy: Never`, requests 10m/16Mi, limit 64Mi, readiness `/healthz`), Service :80→8080, IngressRoute `web` Host(`promotions.localhost`) with `observability: {tracing: false, accessLogs: false}`.
- `platform/appsets/addons/promotion-log.yaml` (copy `github-actions-token.yaml`).

### Task 3: gcd: build/load, DNS, wait, tag check
- Generalize `internal/tokenminter` → `internal/localimage` with an image table (`github-actions-token`, `promotion-log`): `Load` builds + loads each; tag-hash test covers both manifests.
- `dns.inClusterHosts` += `promotions.localhost`; test asserts three rewrites, one rollout.
- `infra.HubWaitRest` += `promotion-log`.

### Task 4: Renderer: http step in both promotion tasks
- `platformfiles.go`: new var `platform` (Stage's platform label) for both tasks; append `recordStep(deployWith)` to `promote-argocd` and `promote-gha` (gha adds `ghaRunURL` from `task.outputs.dispatch.runURL ?? ''`).
- `kargo.go`: pass `platform` var per target Stage.
- Tests: render assertion that both tasks end with the http step having `if: ${{ always() }}`, `continueOnError: true`, URL `http://promotions.localhost/promotions`; golden `UPDATE=1`.

### Task 5: Dashboard
- Replace nothing; add section "Promotion log (every deploy method)" from Loki: table of recent promotions (time, stage, target, platform, deployWith, result, actor/automatic, artifact tag, link), counts per target by result (`count_over_time`), who promoted.

### Task 6: Verify + docs
- `gcd test`; push (user); live: promote a K8S target (region shard) + a gha target (hub) + an auto-promotion; each appears once in Loki with correct fields; stop `promotion-log` → promotion still Succeeded.
- Docs: README UI note, `docs/sync-boundaries.md` (CoreDNS line), gotchas (aborted not recorded), CLAUDE.md layout line.

## Unresolved questions
- Field set OK, anything missing (e.g. pre-deploy check results, verification)?
- Generalize `tokenminter` → `localimage`, or keep 2 separate loaders?
- Dashboard: keep Kargo-metric panels alongside, or replace counts with log-based ones?
