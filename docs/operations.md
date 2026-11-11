# Operations

Runbook for the things you do by hand. For which changes need `mise run infra` / `up` and which Argo CD picks up by itself, see `docs/sync-boundaries.md`.

## Break-glass approval (ADR 0013)

How it works: `docs/break-glass.md`.

Normal promotions: promoters promote Freight that is verified upstream. When Freight skipped verification, Kargo shows **"Manual approval required"**. Only the app's `breakGlass` emails (and admins) can approve it.

- **Who**: `owners.yml` → `breakGlass: [emails]` (capability level only; platform-approved). Break-glass users can also promote and view.
- **How**: in the Kargo UI pick the Freight → promote → **Approve** in the popup → promote. Anyone else gets `403 … break-glass only (owners.yml breakGlass): <reason>`.
- **Audit**: one JSON line per decision:
  ```sh
  kubectl -n kargo logs deploy/break-glass | grep '"decision"'
  ```
- **Check a Stage's list**:
  ```sh
  kubectl -n kargo-<cap>-<app> get stage <stage-or-target> -o jsonpath='{.metadata.annotations.example\.com/break-glass}'
  ```
- **Undo an approval** (cluster admin):
  ```sh
  kubectl -n kargo-<cap>-<app> patch freight <id> --subresource=status --type=json \
    -p '[{"op":"remove","path":"/status/approvedFor/<stage>"}]'
  ```
- **Remove it**: see "Removing it" in `docs/adr/0013-break-glass-approval.md`.

## Restoring Kargo state after a cluster rebuild

Kargo keeps "which Freight is current in each Stage" only in the hub's etcd, so `mise run down`/`up` leaves every Stage empty while the workloads keep running from their `env/*` branches. To restore what teams see in Kargo:

```sh
mise run kargo-restore            # all capabilities; or: mise run kargo-restore payments
```

For each target Stage with no current Freight it reads the branch head (Kargo's commit message names the source commit; `deployment.yaml`, `service.yaml` for Cloud Run apps, or `terraform.yaml` `module` for terraform apps, pins `<repo>:<tag>@<digest>`), recreates that Freight (same artifacts give the same Freight ID; the alias is new), approves it for the Stage and promotes it. The promotion re-copies identical manifests: no new commit, no rollout. Re-running is a no-op. Terraform targets (ADR 0021) and Cloud Run targets (ADR 0024) are the exception: the promotion re-dispatches their workflow (`terraform.yaml` re-runs plan + apply; `cloud-run.yaml` re-runs the deploy), like any gha target.

- **Skipped, with a reason**: targets never promoted (no branch), branches from before ADR 0016 (no pinned image), heads that aren't Kargo promotion commits. Promote fresh Freight to those normally; the next rebuild can restore them.
- **Control-flow Stages** (`<stage>`, `<stage>-done`) stay empty until new Freight flows through; they auto-promote.
- **Auto-promoting targets** (dev) may show two Promotions for the same Freight: Kargo's own auto-promotion fires once the Freight is approved. Both succeed; harmless.
- **Auto-promoting targets restored to older Freight** can end up "Auto-promotion paused": restore's Promotion is a manual one, and Kargo holds auto-promotion when that isn't the newest Freight (`docs/gotchas.md`). Resume it in the UI once the next Freight should flow.

- **Prod targets** run their pre-deploy checks again (ADR 0028): a failing check stops the restore of that Stage.

## Pre-deploy checks (ADR 0028)

Configured in `platform/render/prechecks.cue` (platform only); each prd target Stage runs `pre-<n>` steps (ClusterPromotionTask `gha-check`) that dispatch the check to the app's `sourceRepo` and wait. The demo check is `.github/workflows/precheck.yaml` in this repo.

- **Make the demo check fail**: `gh variable set PRECHECK_FAIL --body true` (back: `--body false`). The next prod Promotion fails at `pre-1::wait`, and the target branch head doesn't move (`git ls-remote origin env/<cap>/<app>/<target>`).
- **Verify the per-check timeout** after a Kargo upgrade: set a check's `timeout: "1m"` and make the workflow sleep 2 minutes. The Promotion must fail at ~1m with "exceeded 1m". `cannot fetch <x> from <nil>` means an http step expression refers to something other than `response` outside `${{ }}`. An unknown-function error naming `now`, `date` or `duration` means Kargo dropped those functions: switch to one `gha-check-<t>` task per fixed timeout.

## Resetting a capability's promotion state

State lives in three places: Kargo (`kargo-<cap>-<app>` per app: Freight, Promotions, Stage status), GitHub (`env/<cap>/<app>/<target>` branches) and the deployed workloads (`<cap>-<target>` namespaces). Pick the lightest option that does the job.

**A. Kargo only.** Workloads and branches stay.
```sh
kubectl -n kargo-<cap>-<app> delete promotions.kargo.akuity.io,freight.kargo.akuity.io,stages.kargo.akuity.io,warehouses.kargo.akuity.io --all
```
Argo CD self-heal recreates the Stages and Warehouses from `rendered/` with empty status. Warehouses are Manual (ADR 0016): assemble new Freight in the UI, then auto-promote stages (dev) promote it again. `kubectl` as cluster admin isn't blocked by `gitops-only.yaml`, which only applies to the kargo-api ServiceAccount.

**B. Full reset: A, plus branches and workloads.**
```sh
gh api -X DELETE repos/pitoniak32/gitops-cd/git/refs/heads/env/<cap>/<app>/<target>   # per target
kubectl delete ns <cap>-<target>                                                       # per target
```
Afterwards the targets show sync `Unknown` until they're promoted again (`docs/gotchas.md`).

**C. Remove the capability.** Delete `capabilities/<cap>/` and merge. CI drops it from `rendered/`, and Argo CD prunes the Kargo project, the AppProject and the Applications. The workloads aren't deleted with the Applications (no cascade finalizer), so do the namespace and branch steps from B as well.

## Rename targets / regions

Renaming a target or region (ADR 0025) makes all its Kargo Stages, Argo CD apps and `env/*` branches new; `kargo-restore` can't map old Freight to new names.
1. `mise run down && mise run up`, after the rename is merged and CI has rendered. `down` also deletes stale `region-*` kind clusters (any name) so the docker network is freed.
2. Delete the old targets' `env/*` branches:
   ```sh
   gh api repos/<owner>/<repo>/git/matching-refs/heads/env/    # list
   gh api -X DELETE repos/<owner>/<repo>/git/refs/heads/env/<cap>/<app>/<old-target>   # per old branch
   ```
3. Promote again from the Warehouse (assemble new Freight; dev auto-promotes).

**Moving a target between platforms** (e.g. K8S to CLOUD_RUN, ADR 0024): the old Argo CD Application is deleted but its workload stays (`preserveResourcesOnDeletion`), so delete the workload by hand. The old `env/<cap>/<app>/<old-target>` branch is orphaned: delete it (as above). Downstream Stages need the new Stage promoted before existing Freight can move on.
