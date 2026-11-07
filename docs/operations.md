# Operations

Runbook for the things you do by hand. For which changes need `mise run infra` / `up` and which Argo CD picks up by itself, see `docs/sync-boundaries.md`.

## Break-glass approval (ADR 0013)

How it works: `docs/break-glass.md`.

Normal promotions: promoters promote Freight that is verified upstream. When Freight skipped verification, Kargo shows **"Manual approval required"**. Only the app's `breakGlass` emails (and admins) can approve it.

- **Who**: `owners.yml` → `breakGlass: [emails]` (capability level, or per app; platform-approved). Break-glass users can also promote and view.
- **How**: in the Kargo UI pick the Freight → promote → **Approve** in the popup → promote. Anyone else gets `403 … break-glass only (owners.yml breakGlass): <reason>`.
- **Audit**: one JSON line per decision:
  ```sh
  kubectl -n kargo logs deploy/break-glass | grep '"decision"'
  ```
- **Check a Stage's list**:
  ```sh
  kubectl -n kargo-<cap> get stage <app>-<stage-or-target> -o jsonpath='{.metadata.annotations.example\.com/break-glass}'
  ```
- **Undo an approval** (cluster admin):
  ```sh
  kubectl -n kargo-<cap> patch freight <id> --subresource=status --type=json \
    -p '[{"op":"remove","path":"/status/approvedFor/<stage>"}]'
  ```
- **Remove it**: see "Removing it" in `docs/adr/0013-break-glass-approval.md`.

## Resetting a capability's promotion state

State lives in three places: Kargo (`kargo-<cap>`: Freight, Promotions, Stage status), GitHub (`env/<cap>/<app>/<target>` branches) and the deployed workloads (`<cap>-<target>` namespaces). Pick the lightest option that does the job.

**A. Kargo only.** Workloads and branches stay.
```sh
kubectl -n kargo-<cap> delete promotions.kargo.akuity.io,freight.kargo.akuity.io,stages.kargo.akuity.io,warehouses.kargo.akuity.io --all
```
Argo CD self-heal recreates the Stages and Warehouses from `rendered/` with empty status. The Warehouse turns the latest commit into new Freight, and auto-promote stages (dev) promote it again. `kubectl` as cluster admin isn't blocked by `gitops-only.yaml`, which only applies to the kargo-api ServiceAccount.

**B. Full reset: A, plus branches and workloads.**
```sh
gh api -X DELETE repos/pitoniak32/gitops-cd/git/refs/heads/env/<cap>/<app>/<target>   # per target
kubectl delete ns <cap>-<target>                                                       # per target
```
Afterwards the targets show sync `Unknown` until they're promoted again (`docs/gotchas.md`).

**C. Remove the capability.** Delete `capabilities/<cap>/` and merge. CI drops it from `rendered/`, and Argo CD prunes the Kargo project, the AppProject and the Applications. The workloads aren't deleted with the Applications (no cascade finalizer), so do the namespace and branch steps from B as well.
