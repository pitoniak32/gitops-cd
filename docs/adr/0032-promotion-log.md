# Promotion log: an http step in every promotion, logged by a hub add-on

Teams want one record per promotion, whatever deploys it (Argo CD or GitHub Actions): who, what, where, how it went. Kargo's own metrics are labelled only by project (ADR 0031), and counting Kargo's Promotion Events in Alloy (tried, reverted) needed parsing of target names and message text, double-counted auto-promotions, and lost counts on restart.

Decision:

- **Record from the promotion itself**: both shared ClusterPromotionTasks (`promote-argocd`, `promote-gha`; ADR 0027) end with an `http` step that POSTs a JSON record to `http://promotions.localhost/promotions`. The step has `if: ${{ always() }}` (it runs after a failed step too, so failures are recorded) and `continueOnError: true` with a 30s timeout (an unreachable endpoint never fails or blocks a deploy).
- **Record**: `promotion`, `project`, `system`, `app`, `stage` (pipeline stage), `target`, `platform`, `deployWith`, `freight`, `freightAlias`, `warehouse`, `artifact`, `commit` (env branch), `actor`, `automatic` (actor is a Kargo controller: auto-promotion), `rollback`, `result` (`Succeeded`/`Failed`), `ghaRunURL`, `uiURL`. The renderer knows platform and deploy method, so nothing is parsed from names.
- **Endpoint**: hub add-on `promotion-log`, a stdlib Go service that validates the record and writes it as one JSON log line (`"msg":"promotion"`). Alloy already ships pod logs to Loki, so there is no new store; the CD pipeline dashboard queries it with LogQL.
- **Reachability**: steps run in the Kargo controller that owns the target: a region shard for K8S targets, the hub default controller otherwise. `promotions.localhost` resolves to the hub Traefik in every cluster (CoreDNS rewrite, like `otlp.localhost`), whose `web` IngressRoute forwards to the service. The hub holds no region credentials (ADR 0015).
- **Image**: built from `platform/infrastructure/promotion-log/` and loaded into the hub node by `gcd infra`, tag = hash of its sources, like the token minter (ADR 0020).

## Considered Options

- **Count Kargo Promotion Events in Alloy**: no change to promotions, but names and messages had to be parsed, each auto-promotion emits two "created" events, and counters reset on restart.
- **Kargo metrics only**: per project, no target, platform, deploy method or actor.
- **Push to Loki's API directly from the step**: no new service, but the step would build Loki's push format and nanosecond timestamps in expressions, and the record would have no validation.

## Consequences

- Every promotion of every system is recorded the moment it ends, from the platform-owned task; teams change nothing.
- Aborted promotions run no steps and are not recorded.
- The endpoint is unauthenticated plain HTTP, local only (as `otlp.localhost`, ADR 0031): any pod could post a fake record.
- Retention is Loki's, which is ephemeral here (ADR 0031).
