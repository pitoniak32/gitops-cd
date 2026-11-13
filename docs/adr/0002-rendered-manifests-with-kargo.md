# Rendered manifests on branch `rendered`, promoted by Kargo

CI renders every app, stage and target to `rendered/` and pushes it to the orphan branch `rendered`. Kargo promotes by copying a target's folder from a Freight commit onto that target's branch `env/<sys>/<app>/<target>`, and Argo CD syncs that branch. Workloads never sync from `main`, so a merged change, including a platform change, reaches each target only through promotion. Reviewers see exact manifest diffs, not templates.

- **Branch `rendered`** holds only `rendered/` plus a README (from `platform/render/rendered-README.md`). CI pushes to it on every push to `main`, as the `gitops-cd-demo` GitHub App. The workflow's concurrency group serializes the pushes. CI commits only when the render changed: `render: <sha7>`, with the source commit's URL in the body. A ruleset lets only that App update the branch.
- **`main`** has `rendered/` gitignored, and a PR that adds it fails CI. `gcd render` writes it locally. PR CI comments the render diff against `rendered`.
- **Readers**: the Kargo Warehouse git subscription and the AppSets `kargo.yaml`, `projects.yaml`, `workloads.yaml`, `addons/kargo-shard-hub.yaml` and `addons/argocd-region-hub.yaml` read revision `rendered`. The `internal/appsets` test enforces this.
- **CODEOWNERS** stays on `main`, because GitHub reads it only from the default branch. Authors run `gcd codeowners`, and CI fails if it's stale.

## Considered Options

- **CI commits the render to `main` after the merge**: every change shows up twice in history, and the second commit is never reviewed.
- **A bot commits the render to the PR branch**: needs a PAT so the commit re-triggers CI, plus "up to date" branch protection to close the merge window.
- **Authors render locally and CI only verifies**: everyone needs the full toolchain. Kept only for CODEOWNERS, which rarely changes.

## Consequences

- A Freight's config commit is a commit on the `rendered` branch. Its body links back to the `main` commit.
- On a fresh repo or fork, push and wait for the first render before running `gcd up`. Until then, the AppSets report a missing revision.
- A re-run of an older push run doesn't commit, because it would roll `rendered` back. A stale CODEOWNERS only warns on `main`, so it never blocks the render.
- `GITHUB_TOKEN` can't bypass the ruleset, so CI uses a short-lived installation token (repo variable `RENDER_APP_CLIENT_ID`, secret `RENDER_APP_PRIVATE_KEY`). Kargo holds the same App's credentials, so it could push to `rendered` too. A dedicated App would close that.
- The PR diff is against the latest render of `main`, not against the PR's merge base.
