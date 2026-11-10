# Render on the PR, not after merge

Status: accepted. Replaces the post-merge render commit of ADR 0002 and ADR 0007 (CODEOWNERS regenerated on main).

CI rendered `rendered/` and `.github/CODEOWNERS` after merge and pushed a separate `render: <sha>` commit to `main`. That commit landed unreviewed, after the PR was already merged, and doubled every change in `main`'s history.

Now the generated files are part of the PR:

- **Drafts** (every PR starts as one): each push renders the affected capabilities and comments the `rendered/` diff against the base. Nothing is committed. `draft-first.yaml` converts a PR opened as ready back to a draft, since GitHub can't require drafts.
- **Ready for review**: the same run commits the render (`render: <sha>`, `RENDER_BOT_TOKEN`, so it re-triggers CI) onto the PR branch, on the transition to ready and on every later push, only when the output changed. Reviewers approve the generated files with their source.
- **What gets re-rendered** (`platform/render/scripts/affected.sh`, from `git diff base...HEAD`):
  - `capabilities/<cap>/`, or that capability's `rendered/` output (bot commits or hand edits): that capability. A deleted capability is pruned.
  - `teams/`, renderer CUE, `cue.mod`, render scripts, `rendered/infrastructure/`: everything.
  - Anything else: nothing.
  - CODEOWNERS is reassembled every run.
  - `render-affected.sh` gives the same result as a full render whenever the branch's `rendered/` matched its base. The tests check this.
- **Required check `ci`** with "require branches to be up to date" on `main`; squash merge only. A PR behind `main` must update, which re-renders, so the squashed commit always carries a current render. Drafts pass the check (they can't merge); a ready PR whose render can't be pushed (fork) fails it.
- **`main`**: CI does a full render as a drift alarm and fails on any difference. It commits nothing.
- A Freight's config commit is now the PR's squash commit.

## Considered Options

- **Keep the post-merge commit**: the extra unreviewed commit on `main` is the problem.
- **Commit on every push, drafts included**: noisy force-fetches while iterating; drafts are where you iterate.
- **GitHub Environment approval per render commit**: a click per push; draft vs ready already says "commit now".
- **`render` label or `/render` comment**: goes stale on the next push.
- **Merge queue**: would render against the real merge result without rebases, but isn't available for repositories owned by a personal account.
- **Re-render every capability on every PR**: correct but slow as capabilities grow; `affected.sh` plus the up-to-date rule keeps the selective render equal to a full one.

## Consequences

- The bot pushes to your branch: `jj git fetch` (or `git pull`) before pushing again, or the push is rejected.
- PR diffs include generated files; review them like code.
- Running `mise run render` locally and committing is fine; CI then has nothing to commit.
- Fork PRs must commit their own render (no secret for the bot push).
- `draft-first.yaml` runs from the base branch (`pull_request_target`), so it takes effect once merged.
- If `main` ever drifts (protection bypassed by an admin), the push run fails; fix with a PR that runs `mise run render`.
