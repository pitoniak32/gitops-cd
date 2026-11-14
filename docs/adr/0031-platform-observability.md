# Platform observability: one LGTM store on the hub, Alloy pushing from every cluster

Kubernetes collects nothing by itself: components expose Prometheus `/metrics`, `kubectl logs` reads one container, events expire after about an hour, and apiserver/kubelet tracing only exists with a tracing config and a collector. The platform components already speak the formats (Argo CD, Kargo, Traefik, cert-manager, Argo Rollouts expose Prometheus metrics; Argo CD and Traefik export OTLP traces), but nothing gathers them, and with a hub plus one cluster per region (ADR 0015) there is no single place to look.

Decision:

- **Store and UI on the hub**: add-on `observability` runs the single-container `grafana/otel-lgtm` image (Grafana, Prometheus, Loki, Tempo behind an OTLP collector). Storage is an `emptyDir`: data is lost when the pod restarts. Fine for a local demo.
- **Collector on every cluster**: add-ons `alloy` (hub) and `alloy-region` (each region) run Grafana Alloy. It scrapes pod ports named `metrics`/`http-metrics` and cAdvisor, tails pod logs through the API, and receives OTLP traces on Service `alloy.alloy.svc:4317/4318`. It sets resource attribute `k8s.cluster.name` (`hub` or the region) and **pushes** OTLP/HTTP to `http://otlp.localhost`.
- **Push, not pull**: `otlp.localhost` resolves to the hub Traefik in every cluster (CoreDNS rewrite, same path as `keycloak.localhost`), and an IngressRoute on the `web` entrypoint forwards to the store. The hub never holds region credentials (ADR 0015). Ingest is plain HTTP without authentication: local only.
- **Login**: `https://grafana.localhost`, Keycloak public client `grafana` (PKCE). Anonymous access and the login form are off. Only the hardcoded admin emails (ADR 0009) get in, as Admin; everyone else is denied. The data spans every system, and access is per system (ADR 0011), so there are no global viewers.
- **Traces**: Argo CD (hub + regions) and Traefik export OTLP to their cluster's Alloy.
- **Labels**: query by `k8s_cluster_name` (Prometheus promotes `k8s.cluster.name`; Loki indexes it).
- Pins: `grafana/otel-lgtm:0.38.1`, Alloy chart `1.15.0`.

## Considered Options

- **kube-prometheus-stack per cluster**: the standard, but about 1–2 GB per cluster on a laptop already running several kind clusters, and one Grafana per cluster.
- **Hub pulls (scrapes) the regions**: needs region credentials or exposed endpoints on the hub; breaks ADR 0015.
- **Separate charts (Grafana, Prometheus/VictoriaMetrics, Loki, Tempo)**: persistent and tunable, four pins and much more config. Revisit if data must survive restarts.
- **Per-system Grafana access** from `owners.yml`: no app telemetry yet, and Grafana has no cheap per-system data isolation. Revisit with app instrumentation.

## Consequences

- Measured cost (hub + one region): lgtm ~700 MiB, Alloy ~200–300 MiB per cluster, so about 1.4 GB for hub + 2 regions. The lgtm image is ~3.5 GB, so the first pull is slow. On a ~12 GB Docker VM the hub node was already swapping: give Docker more memory, or drop a region.
- One Grafana for the whole platform; regions show up as `k8s_cluster_name` values.
- Data is ephemeral, and only admins see it.
- No kube-state-metrics (no object-state dashboards) and no apiserver/kubelet traces yet.
- A new region needs nothing extra: `alloy-region` follows the cluster labels and `gcd region-dns` adds the rewrite.
