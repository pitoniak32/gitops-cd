# kind clusters: hub + region-us + region-onprem
Status: needs-triage

`mise run up`/`down` create and delete all three clusters on the `kargo-demo` network. Only the hub binds host 80/443. Each region bootstraps its own minimal Argo CD (no dex, notifications or appset), self-managed from the local checkout like the hub. The tasks are idempotent.

Done when: `mise run up` from nothing gives three Ready clusters, each with a healthy Argo CD.

## Comments
- Done 2026-11-08. `platform/cluster/regions` (one name per line) drives up/down/infra; `region.yaml` is a kind template (name set via yq). New addon `platform/infrastructure/argocd-region/` (dex/notifications off, appset replicas 0, chart 10.14.0 = hub). `infra` bootstraps each region from the host (`helm --kube-context`), then applies the inlined Application; hub has no region creds. `infra` now starts with `use-context kind-kargo-demo` (kind leaves last-created context current).
- Verified: clean `up` (~8 min), `infra` re-run unchanged. Memory: hub 4.5 GiB, regions 1.7-1.8 GiB each.
