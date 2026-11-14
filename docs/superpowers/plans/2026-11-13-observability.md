# Platform Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Metrics, logs and traces of the platform (hub + every region), viewable locally in one Grafana at `https://grafana.localhost`.

**Architecture:** Hub add-on `observability` runs the single-container `grafana/otel-lgtm` image (Grafana + Prometheus + Loki + Tempo, OTLP in, ephemeral storage). Every cluster (hub + regions) runs Grafana Alloy: scrapes pod metrics ports + cAdvisor, tails pod logs, receives OTLP traces, and **pushes** everything as OTLP/HTTP to `http://otlp.localhost` (hub Traefik `web` → lgtm `:4318`). Push keeps ADR 0015: the hub holds no region credentials. Grafana login = Keycloak, admins only (ADR 0009/0011: no global viewers).

**Tech Stack:** `grafana/otel-lgtm` image, Grafana Alloy Helm chart (`https://grafana.github.io/helm-charts`, chart `alloy`), Traefik IngressRoute, Keycloak public client + PKCE, Go (`gcd` dns/certs/open/infra).

**Spec:** `docs/adr/0031-platform-observability.md` (written in Task 1; this plan's Architecture + Global Constraints are its content).

## Global Constraints

- Platform-only: no app workload instrumentation, no `systems/` schema changes.
- Hub never gets region creds; regions push (ADR 0015).
- Grafana: Keycloak OIDC only, public client `grafana`, `pkce.code.challenge.method: S256`, no login form, **anonymous off** (otel-lgtm defaults it on), Admin = `admin@example.com` (same hardcoded list as Argo CD/Kargo, ADR 0009), everyone else denied (`role_attribute_strict`).
- Every cluster's data carries label/attribute `cluster` = `hub` or the region name (`dc1`, `us-east4`).
- Pin everything: lgtm image tag (or digest), Alloy chart `targetRevision`, exact versions (latest stable at implementation time; record them in the ADR).
- Storage ephemeral (emptyDir); lost on pod restart. Fine locally.
- Ingest `otlp.localhost` is plain HTTP, unauthenticated, `web` entrypoint only. Local only.
- Every `gcd` change idempotent; `gcd test` green; clean `gcd up` proves it.
- VCS: jj. No pushes (user pushes `main`; add-ons sync from `main`).

## Review Focus

1. **Anonymous Grafana access**: otel-lgtm ships `GF_AUTH_ANONYMOUS_ENABLED=true` + Admin role → anyone at `grafana.localhost` is admin. Expect: unauthenticated request redirects to Keycloak. Pinned by Task 3 verification + Task 6 curl check.
2. **Non-admin Keycloak user** logs in → expect denied, not Viewer. Task 6 step.
3. **Docker restart changes hub IP** → region `otlp.localhost` must follow `hub-traefik` like keycloak does (same rewrite target, re-resolved by `gcd infra`/`region-dns`). Task 2 test asserts both hosts rewritten to the same target in one rollout.
4. **Region data mislabeled / unlabeled** (`cluster` missing → all regions merge). Task 6 query `count by (cluster) (up)` returns `hub`, `dc1`, `us-east4`.
5. **Memory**: lgtm + 3× Alloy on top of existing clusters. Task 6 records `docker stats` totals in the ADR; resource requests/limits set in Tasks 3–4.

---

### Task 1: ADR 0031

**Files:**
- Create: `docs/adr/0031-platform-observability.md`
- Modify: `docs/README.md` (ADR index, if it lists ADRs), `GLOSSARY.md` (only if a term is new: none expected)

- [ ] **Step 1:** Write the ADR in the existing ADR format (copy headings from `docs/adr/0030-renderer-in-go.md`): context (k8s has no built-in collection/traces; components already expose Prometheus + OTLP), decision = Architecture + Global Constraints above, alternatives rejected (kube-prometheus-stack per cluster: ~1–2 GB each; hub pulling from regions: breaks ADR 0015; per-system Grafana access: out of scope until app telemetry exists), consequences (ephemeral data, unauthenticated local ingest, admins only).
- [ ] **Step 2:** `jj commit -m "docs(adr): 0031 platform observability"`

### Task 2: gcd: `otlp.localhost` DNS, `grafana.localhost` cert + open, wait list

**Files:**
- Modify: `platform/cli/internal/dns/dns.go`, `platform/cli/internal/dns/dns_test.go`
- Modify: `platform/cli/internal/certs/certs.go:89`, `platform/cli/internal/certs/certs_test.go:58`
- Modify: `platform/cli/internal/cmd/open.go:14-18`, `platform/cli/internal/cmd/open_test.go`
- Modify: `platform/cli/internal/infra/infra.go:24`

**Interfaces:**
- Produces: in-cluster names `keycloak.localhost` and `otlp.localhost` resolve to hub Traefik in every cluster (hub: `traefik.traefik.svc.cluster.local`; regions: `hub-traefik.kube-system.svc.cluster.local`).
- Produces: `infra.HubWaitRest` includes `"observability"` and `"alloy"` (Application names from Tasks 3–4).

- [ ] **Step 1: Failing tests**
  - `TestEnsureRewriteRestartsOnlyWhenChanged` → call `ensureRewrite(ctx, c, []string{"keycloak.localhost", "otlp.localhost"}, "t.svc", log)` twice; assert `waits == 1` and the Corefile contains both `rewrite name keycloak.localhost t.svc` and `rewrite name otlp.localhost t.svc`.
  - `TestRewriteCorefile` unchanged (single host).
  - certs test host list adds `"grafana.localhost"`.
  - `open_test` want list: `https://grafana.localhost` before the keycloak URL.
- [ ] **Step 2:** `cd platform/cli && go test ./internal/dns/ ./internal/certs/ ./internal/cmd/` → FAIL (signature / missing host).
- [ ] **Step 3: Implement**
  - `dns.go`: `const keycloakHost` → `var inClusterHosts = []string{"keycloak.localhost", "otlp.localhost"}`; `ensureRewrite(ctx, c *kube.Clients, hosts []string, target string, log io.Writer) error` applies `RewriteCorefile` per host, updates the ConfigMap + restarts CoreDNS once if any changed. `Hub` and `Regions` pass `inClusterHosts`. Update package doc comment.
  - `certs.go`: add `"grafana.localhost"` to `hosts`.
  - `open.go`: append `https://grafana.localhost` before keycloak.
  - `infra.go`: `HubWaitRest` append `"observability", "alloy"` after `"break-glass"`.
- [ ] **Step 4:** `gcd test` → PASS.
- [ ] **Step 5:** `mise run build`; `jj commit -m "feat(gcd): otlp.localhost dns, grafana cert/open, wait observability"`

### Task 3: Hub add-on `observability` (lgtm + Grafana UI + ingest route + Keycloak client)

**Files:**
- Create: `platform/infrastructure/observability/manifests/{kustomization.yaml,lgtm.yaml,ingress.yaml}`
- Create: `platform/infrastructure/observability/manifests/dashboards/{argocd.json,traefik.json}` (vendored: Argo CD `examples/dashboard.json` at the Argo CD version in use; Traefik official dashboard from grafana.com, id 17346)
- Create: `platform/appsets/addons/observability.yaml` (copy `keycloak.yaml`: hub selector, kustomize path, namespace `observability`)
- Modify: `platform/infrastructure/keycloak/manifests/kargo-realm.json` (client `grafana`)

**Interfaces:**
- Produces: Service `observability/lgtm` ports `grafana` 3000, `otlp-http` 4318.
- Produces: `http://otlp.localhost` (IngressRoute, entryPoints `[web]`) → `lgtm:4318`; `https://grafana.localhost` (entryPoints `[websecure]`) → `lgtm:3000`.
- Produces: Application `observability` on the hub.

- [ ] **Step 1:** `lgtm.yaml`: Deployment (1 replica, pinned `grafana/otel-lgtm:<tag>`, requests 512Mi/250m, limit 2Gi, emptyDir at the image's data dir) + Service. Grafana env:
  - `GF_SERVER_ROOT_URL=https://grafana.localhost`
  - `GF_AUTH_ANONYMOUS_ENABLED=false`, `GF_AUTH_DISABLE_LOGIN_FORM=true`
  - `GF_AUTH_GENERIC_OAUTH_ENABLED=true`, `_NAME=Keycloak`, `_CLIENT_ID=grafana`, `_USE_PKCE=true`, `_SCOPES=openid profile email`
  - `_AUTH_URL=http://keycloak.localhost/realms/kargo/protocol/openid-connect/auth`, `_TOKEN_URL=…/token`, `_API_URL=…/userinfo` (hub CoreDNS rewrite makes the same host work in-cluster)
  - `_ROLE_ATTRIBUTE_PATH=contains(['admin@example.com'], email) && 'Admin'`, `_ROLE_ATTRIBUTE_STRICT=true`, `_AUTO_LOGIN=true`
  - dashboards: ConfigMap generated by kustomize `configMapGenerator` from `dashboards/*.json`, mounted where the image's Grafana provisioning picks it up (verify path in the pinned image: `/otel-lgtm/grafana/conf/provisioning/dashboards/` + a provider file, or a dashboards provider ConfigMap). Dashboards' datasource variables → the image's Prometheus datasource uid.
- [ ] **Step 2:** `ingress.yaml`: two `traefik.io/v1alpha1` IngressRoutes per Interfaces.
- [ ] **Step 3:** Keycloak client `grafana` (copy the `argocd` client: public, standardFlow, no direct grants, `redirectUris: ["https://grafana.localhost/login/generic_oauth"]`, `webOrigins: ["https://grafana.localhost"]`, `pkce.code.challenge.method: S256`, `post.logout.redirect.uris: https://grafana.localhost/*`).
- [ ] **Step 4:** `kubectl kustomize platform/infrastructure/observability/manifests >/dev/null` → exit 0; `cd platform/cli && go test ./internal/appsets/` → PASS (addon-has-appset, basics, retry, pins).
- [ ] **Step 5:** `jj commit -m "feat(observability): hub lgtm add-on, grafana.localhost, keycloak client"`

### Task 4: Alloy collector on every cluster

**Files:**
- Create: `platform/infrastructure/alloy/values.yaml`
- Create: `platform/appsets/addons/alloy.yaml` (hub selector; `valuesObject` sets env `CLUSTER=hub`)
- Create: `platform/appsets/addons/alloy-region.yaml` (copy `kargo-shard.yaml` selector incl. `matchExpressions [{key: example.com/region, operator: Exists}]` + `values.region`; `valuesObject` env `CLUSTER={{.values.region}}`)

**Interfaces:**
- Consumes: `http://otlp.localhost` (Task 2 DNS, Task 3 route).
- Produces: Service `alloy/alloy` with OTLP gRPC `4317` + HTTP `4318` in every cluster (Task 5 points Argo CD/Traefik here). Application name `alloy` in each cluster (both AppSets template `metadata.name: alloy`).

- [ ] **Step 1:** `values.yaml`: chart pinned in both AppSets (same `targetRevision`), `alloy.extraPorts` 4317/4318, requests 128Mi/50m, limit 512Mi, chart RBAC on. `alloy.configMap.content` pipeline:
  - `discovery.kubernetes "pods"` → relabel keep container ports named `metrics|http-metrics` → `prometheus.scrape` → `otelcol.receiver.prometheus`
  - `discovery.kubernetes "nodes"` → cAdvisor via apiserver proxy (`/api/v1/nodes/<node>/proxy/metrics/cadvisor`, SA token) → same receiver
  - `loki.source.kubernetes` (all pods) → `otelcol.receiver.loki`
  - `otelcol.receiver.otlp` (grpc `0.0.0.0:4317`, http `0.0.0.0:4318`)
  - all → `otelcol.processor.resourcedetection`/`attributes` inserting `cluster = sys.env("CLUSTER")` → `otelcol.processor.batch` → `otelcol.exporter.otlphttp` endpoint `http://otlp.localhost`, tls insecure.
- [ ] **Step 2:** `cd platform/cli && go test ./internal/appsets/` → PASS (region-selector, chart-first, pins). `helm template alloy grafana/alloy --version <pin> -f platform/infrastructure/alloy/values.yaml >/dev/null` → exit 0.
- [ ] **Step 3:** `jj commit -m "feat(observability): alloy collector on hub + regions"`

### Task 5: Platform components emit traces + metrics

**Files:**
- Modify: `platform/infrastructure/argocd/values.yaml`, `platform/infrastructure/argocd-region/values.yaml`
- Modify: `platform/infrastructure/traefik/values.yaml`
- Modify (only if their charts gate metrics ports behind a flag): `platform/infrastructure/kargo/values.yaml`, `kargo-shard/values.yaml`, `cert-manager/values.yaml`

**Interfaces:**
- Consumes: `alloy.alloy.svc.cluster.local:4317` (Task 4).

- [ ] **Step 1:** Argo CD (both values files): `configs.params` `otlp.address: alloy.alloy.svc.cluster.local:4317`, `otlp.insecure: "true"`; ensure controller/server/repoServer/applicationSet pods expose a container port named `metrics` (chart default; enable `*.metrics.enabled` only if needed).
- [ ] **Step 2:** Traefik: `tracing.otlp.enabled: true`, `tracing.otlp.grpc.enabled: true`, `endpoint: alloy.alloy.svc.cluster.local:4317`, `insecure: true`; metrics port (default `metrics` 9100) left on.
- [ ] **Step 3:** `helm template` each changed chart with its pinned version + values → exit 0; `gcd test` → PASS (argocd values are also the bootstrap values; `TestAppSets` reads them).
- [ ] **Step 4:** `jj commit -m "feat(observability): argocd + traefik otlp traces"`

### Task 6: Docs + clean `gcd up` verification

**Files:**
- Modify: `README.md` (UI table: Grafana row `https://grafana.localhost` | platform metrics/logs/traces | Keycloak, admins only)
- Modify: `docs/sync-boundaries.md` (CoreDNS line: `otlp.localhost`), `docs/gotchas.md` (any new non-obvious failure found here)
- Modify: `CLAUDE.md` Layout (`platform/infrastructure/{observability,alloy}`) if the add-on list is enumerated there
- Modify: `docs/adr/0031-platform-observability.md` (pinned versions, measured memory)

- [ ] **Step 1:** User pushes `main` (add-ons are git-sourced from `main`); then `gcd down && gcd up` → completes, waits include `observability`, `alloy`.
- [ ] **Step 2:** `curl -sI https://grafana.localhost/` → `302` to `/login` → Keycloak (no anonymous session). Log in as `admin@example.com` → Admin. Log in as a non-admin realm user → denied.
- [ ] **Step 3:** Grafana Explore:
  - Prometheus `count by (cluster) (up)` → series for `hub`, `dc1`, `us-east4`
  - Loki `{cluster="dc1"}` → log lines
  - Tempo search `service.name=argocd-server` → traces (open an Argo CD UI page first)
  - Dashboards Argo CD + Traefik render data.
- [ ] **Step 4:** `docker restart region-dc1-control-plane kargo-demo-control-plane`, `gcd infra` → `{cluster="dc1"}` data resumes.
- [ ] **Step 5:** `docker stats --no-stream` totals → record in ADR. Update docs per Files.
- [ ] **Step 6:** `gcd test` → PASS; `jj commit -m "docs(observability): grafana UI, sync boundaries, measured cost"`
