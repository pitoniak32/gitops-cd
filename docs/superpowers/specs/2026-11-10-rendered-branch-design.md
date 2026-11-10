# Rendered branch — design

Date: 2026-11-10. Becomes ADR 0022. Amends ADR 0002 (where rendered/ lives) and ADR 0007 (CODEOWNERS regeneration).

## Goal

No CI commits on `main`. Today every merge is followed by a bot `render: <sha>` commit on `main`. Generated output moves to its own branch; `main` holds only human (squash-merged) commits.

## Decisions

- **Branch `rendered`**: orphan, contains only `rendered/` (same paths as today, so no path changes for Kargo/Argo CD). Only CI pushes to it (branch protection: no human pushes).
- **`main`**: `rendered/` deleted and gitignored; `mise run render` still writes it locally for inspection.
- **CI on push to `main`**: full render → worktree of `rendered` → replace `rendered/` → commit `render: <sha7>` (body: source commit URL) only if changed → push with `GITHUB_TOKEN`. Concurrency group per ref, push runs never cancelled (queued in order). First run creates the branch (orphan) if missing.
- **CI on PR**: lint/tests as today; render; comment the diff against `origin/rendered` (fallback: "no rendered branch yet"). New check: `.github/CODEOWNERS` must equal a fresh assembly, else fail "run `mise run codeowners`".
- **CODEOWNERS**: stays on `main` (GitHub reads it from the default branch). PR author regenerates it (`mise run codeowners`); CI only verifies. No bot ever writes `main`.
- **Readers switch `main` → `rendered`**: AppSets `kargo.yaml` (rendered-kargo), `projects.yaml`, `workloads.yaml` (git files gen), `addons/kargo-shard-hub.yaml` + `addons/argocd-region-hub.yaml` (regions git files gen); Kargo Warehouse git subscription `branch: rendered` (includePaths unchanged).
- **Unchanged**: promotion steps (`git-clone` checks out the Freight commit, now on `rendered`), `env/*` branches, `kargo-restore`, existing Freight (their `main` commits still contain `rendered/`).

## Components

- `.github/workflows/ci.yaml`: drop `paths-ignore`, CI-owned guard becomes "CODEOWNERS up to date" + "no `rendered/` on main"; render/diff/comment against `origin/rendered`; push job commits to `rendered`.
- `platform/mise-tasks/codeowners`: new; renders capability fragments to a temp dir, assembles `.github/CODEOWNERS`.
- `platform/appsets/*`: revisions above. `platform/appsets/check.sh`: every git generator/source whose path starts `rendered/` uses revision `rendered`; no other uses `rendered`.
- `platform/render/delivery.cue`: Warehouse `branch: "rendered"` (constant `renderedBranch` next to `renderedDir`).
- `.gitignore`: `/rendered/`.

## Error handling

- First push after merge: AppSets point at `rendered` before CI creates it → Argo CD reports a missing revision and retries (unlimited) until it exists.
- A render push racing another: serialized by concurrency; each run rebases nothing (it replaces the tree), and `git push` of a stale branch fails → run fails loudly; next run fixes.

## Testing

- golden: Warehouse branch `rendered`; `mise run test` green.
- `check.sh`: revision rule above.
- CODEOWNERS check: `mise run codeowners` on clean tree → no diff.
- After merge: CI creates `rendered` (one commit, only `rendered/`), `main` has no bot commit; clean `mise run up` reaches Synced/Healthy and Kargo Warehouses discover from `rendered`.
