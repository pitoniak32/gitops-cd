# GitHub Actions as a deploy method

Some teams deploy with GitHub Actions rather than Argo CD, one target at a time (dev before prod). So each deploy chooses how it is applied. Argo CD stays the default.

- **`app.yml`**: `deployWith: argocd | gha` and `workflow: {repo, file, ref}` (defaults: this repo, the platform's workflow file, `main`) are layered fields (ADR 0004). TERRAFORM and CLOUD_RUN targets are always `gha` (ADR 0021, 0024).
- **Rendering**: the target folder is rendered as usual. A `gha` deploy gets no Argo CD params (so no Application) and no AppProject destination. Its target Stage carries `example.com/deploy-with`.
- **Promotion** (ClusterPromotionTask `promote-gha`, ADR 0027): the same steps through `git-push` (the target branch still records what was deployed), then two Kargo `http` steps:
  - `dispatch`: `POST /repos/<repo>/actions/workflows/<file>/dispatches` with `return_run_details: true` and inputs `system, app, stage, target, commit` (the pushed target-branch commit) plus `image` or `module`. Any non-200 fails the Promotion with GitHub's message.
  - `wait`: `GET /actions/runs/<id>` every 30s, up to 60m. `completed`+`success` succeeds, any other conclusion fails, and a 404 keeps polling (GitHub returns the run id before its API serves the run).
- **Auth**: a fine-grained PAT (`GITHUB_PAT` in the keychain, `gcd secret set`, `Actions: read and write` on each workflow repo). `gcd creds` writes it to `kargo-shared-resources/github-actions`, and the shards read it from the hub.
- **Example workflows** (`.github/workflows/`) check that the artifact matches and write a job summary. They deploy nothing, because GitHub-hosted runners can't reach the local clusters.

## Considered Options

- **A per-app switch only**: a team couldn't move dev without prod.
- **Kargo `gha-dispatch-workflow` / `gha-wait-for-workflow`**: Kargo Enterprise only.
- **Dispatch without `return_run_details`, then find the run by a correlation input**: more requests and races.
- **Skip the target branch for `gha`**: no git record per target, and switching back to Argo CD would start from a stale branch.
- **A GitHub App installation token**: it expires in 1h, and Kargo expressions can't mint one.
- **A self-hosted runner in kind**: real deploys, but another add-on. Out of scope for the demo.

## Consequences

- Switching a target from `argocd` to `gha` deletes its Application, but the workload is orphaned, not removed (ADR 0017). Switching back recreates the Application, which tracks the current target branch.
- `gcd kargo-restore` re-promotes `gha` targets, which re-dispatches their workflow.
- The workflow is read from GitHub at dispatch. A missing file, or one without `workflow_dispatch`, fails at `dispatch` (404/422). GitHub.com only: GHES returns 204 without run details.
- A `gha` Stage shows **Healthy** after any successful promotion. In OSS Kargo, only `argocd-update` registers a health check, and a Stage with none counts as Healthy. For a real signal, add Kargo verification (an AnalysisTemplate that queries the deploy target). It isn't wired, because the demo workflows deploy nothing.
