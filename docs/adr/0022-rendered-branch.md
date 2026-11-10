# Rendered output on its own branch

Status: accepted. Amends ADR 0002 (where `rendered/` lives) and ADR 0007 (who regenerates CODEOWNERS).

After every merge, CI pushed a separate `render: <sha>` commit to `main`. Every change showed up twice in `main`'s history, and the second commit was never reviewed.

Now `main` gets no bot commits:

- **Branch `rendered`**: an orphan branch holding only `rendered/` (same paths as before), plus a README copied from `platform/render/rendered-README.md`. CI pushes to it on every push to `main`, with `GITHUB_TOKEN`. Pushes are serialized by the workflow's concurrency group, and CI commits only when the render changed: `render: <sha7>`, with the source commit URL in the body. The first run creates the branch. Branch protection: no human pushes.
- **Readers**: the AppSets `kargo.yaml`, `projects.yaml`, `workloads.yaml`, `addons/kargo-shard-hub.yaml` and `addons/argocd-region-hub.yaml` read `rendered/` at revision `rendered`, and `check.sh` enforces it. The Kargo Warehouse git subscription uses `branch: rendered` (`renderedBranch` in `input.cue`). Promotion is unchanged: `git-clone` checks out the Freight commit, which now lives on `rendered`.
- **`main`**: `rendered/` is gitignored. `mise run render` writes it locally for inspection. A PR that adds `rendered/` fails CI.
- **PRs**: CI renders and comments the diff against the `rendered` branch.
- **CODEOWNERS** stays on `main`, because GitHub reads it only from the default branch. Authors run `mise run codeowners` and commit it; CI fails if it's stale.

## Considered Options

- **Keep the post-merge commit on `main`**: the reason for this ADR.
- **Commit the render on the PR (bot pushes to the PR branch when ready for review)**: needs a PAT so the bot's commit re-triggers CI. It also needs required checks plus "up to date" branch protection to close the merge window between "ready" and the render commit. Too many moving parts.
- **Render locally, CI only verifies**: everyone needs the full toolchain for every change. Kept only for CODEOWNERS, which changes rarely.
- **Bot commits CODEOWNERS to `main` only when it changes**: rare, but still a bot commit on `main`.

## Consequences

- A Freight's config commit is a `rendered`-branch commit. Its `render:` body links back to the `main` commit it came from.
- Existing Freight and `env/*` branches stay valid: their old `main` commits still contain `rendered/`.
- Bootstrap: until CI first creates `rendered`, the AppSets and Applications report a missing revision and retry.
- Ownership changes need `mise run codeowners`, which needs local tooling (`mise install`).
- The PR diff is against the latest render of `main`, not the PR's merge base. Same as before.
