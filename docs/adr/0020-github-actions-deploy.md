# GitHub Actions as a deploy method

Status: accepted. Extends ADR 0012's target Stages and ADR 0016's promotion steps; Argo CD stays the default.

Some teams deploy with GitHub Actions rather than Argo CD, and need to move one target at a time (dev before prod). A target Stage always ended with `argocd-update`.

Now each deploy chooses how it is applied:

- **`app.yml`**: `deployWith: argocd | gha` (default `argocd`) and `workflow: {repo, file, ref}` (defaults: this repo, `deploy.yaml`, `main`) are `#Layer` fields, so app, stage, or target can set them; the most specific wins, per field. `workflow` is ignored where the result is `argocd`.
- **Rendering**: every target folder is rendered as before. A `gha` deploy gets no `rendered/argocd/deploys/...` params file (so no Application) and no AppProject destination. Target Stages carry `example.com/deploy-with`.
- **Promotion** (`gha` target Stage): the same steps through `git-push` (the target branch still records what was deployed), then two Kargo `http` steps:
  - `dispatch`: `POST /repos/<repo>/actions/workflows/<file>/dispatches` with `return_run_details: true` and inputs `capability, app, stage, target, commit` (the pushed target-branch commit), `image` (`<repo>:<tag>@<digest>`). Any non-200 fails the Promotion with GitHub's message.
  - `wait`: `GET /actions/runs/<id>` every 30s; `completed`+`success` succeeds, any other completed conclusion (or a non-200) fails, anything else keeps it Running; `retry.timeout: 30m`.
- **Auth**: a fine-grained PAT (`GITHUB_PAT` in fnox, `Actions: read and write` on each workflow repo). `mise run creds` writes it to `kargo-shared-resources/github-actions` (`cred-type: generic`, key `token`); steps read `sharedSecret('github-actions').token`. Region shards read it from the hub like the git creds.
- **Example**: `.github/workflows/deploy.yaml` checks out the commit, checks the image matches, and writes a job summary. It deploys nothing: GitHub-hosted runners can't reach the local kind clusters. `payments/worker` dev uses it.

## Considered Options

- **Per-app switch only**: rejected; a team couldn't move dev without prod.
- **Kargo `gha-dispatch-workflow` / `gha-wait-for-workflow`**: Kargo Enterprise only; this repo runs open-source Kargo.
- **Dispatch without `return_run_details`** and find the run by a correlation input: more requests and races; GitHub returns the run id since 2026-02.
- **Skip the target branch for `gha`**: no git record per target, and switching back to Argo CD would start from a stale branch.
- **GitHub App installation token**: expires in 1h and Kargo expressions can't mint one; it would need a refresher in the cluster. A PAT is enough for a local demo.
- **Self-hosted runner in kind**: real deploys, but another add-on and runner registration; out of scope for the demo.

## Consequences

- Switching a target from `argocd` to `gha` deletes its Application; the `workloads` AppSet has `preserveResourcesOnDeletion: true`, so the running workload is orphaned, not removed. Clean it up by hand if the workflow now owns it.
- Switching back creates the Application on the next render, tracking the already-current target branch.
- `mise run kargo-restore` re-promotes `gha` targets, which re-dispatches their workflow.
- The workflow and its `ref` are read from GitHub at dispatch, not rendered; a missing file or a file without `workflow_dispatch` on that ref fails the Promotion at `dispatch` (404/422).
- GitHub.com only: GHES ignores `return_run_details` and returns 204, which fails `dispatch`.
- A `gha` Stage shows **Healthy** after any successful promotion, and nothing checks the workload afterwards. Kargo derives Stage health only from health checks that steps register; in OSS Kargo only `argocd-update` registers one, and a Stage with none counts as Healthy (`assessHealth` in Kargo's `regular_stages.go`). It reports Unknown only while promoting, without Freight, or after a failed promotion. There's no Stage setting to report Unknown instead.
- For a real signal, add Kargo verification to `gha` target Stages: an AnalysisTemplate (e.g. a Job that queries the deploy target) runs after each promotion and marks the Freight verified or failed. Not wired here, because the demo workflow deploys nothing to check; until then, the run's conclusion is the only check.
