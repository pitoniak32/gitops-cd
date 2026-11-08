# Regional root Applications
Status: needs-triage
Blocked by: 01, 03

Each region gets a root Application syncing `rendered/argocd/<region>/` from GitHub `main`. Update `docs/sync-boundaries.md`.

Done when: after a push, each region's Argo CD shows only its own targets' apps, and promotions sync them.

## Comments
- Impl 2026-11-08. `platform/bootstrap/rendered.yaml` now hub-only (`rendered-kargo`). New template `platform/bootstrap/region.yaml` (`__REGION__` -> `rendered/argocd/<r>`), applied by `infra` after the regions loop with `--context kind-region-<r>` (needs regional Argo CD first). `infra` also drops the old hub `rendered-argocd` (patches finalizer onto it + its tracked children, then deletes: cascade). Re-run safe (guarded by existence).
- Verified: server dry-run of rendered output per region, region root apps, hub rendered.yaml. Not e2e (needs push). Docs: sync-boundaries, README, gotchas.
