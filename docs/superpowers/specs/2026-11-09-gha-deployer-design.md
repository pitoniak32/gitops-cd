# GitHub Actions deployer — design

Status: approved in chat 2026-11-09. Implemented (ADR 0020). Renamed `deployer` → `deployWith` (label `example.com/deploy-with`): GLOSSARY avoids "deployer" (it means promoter).

## Goal

Teams choose per app, stage or target whether a Kargo target Stage deploys through Argo CD (today) or
GitHub Actions. Layered so a team can move dev to GHA before committing prod. Scope is a **demo**: GHA
runners can't reach local kind clusters, so the example workflow only proves the Kargo ↔ GHA handshake
(dispatch, wait, pass/fail the promotion). Nothing is deployed by it.

## Decisions

| Question | Decision |
|---|---|
| What GHA deploys | demo only: workflow logs inputs + rendered manifests, succeeds |
| Granularity | layered `#Layer` field (app → stage → target, most specific wins) |
| Which workflow | per-layer `workflow: {repo, file, ref}`, defaults to platform example in this repo |
| Env branch for GHA targets | yes, written as today; workflow gets the pushed SHA |
| Kargo integration | OSS `http` step (GHA steps are Kargo Enterprise only) |
| Auth | fine-grained PAT (`GITHUB_PAT`, fnox) → shared generic secret |

## 1. Schema (`platform/render/app.cue`)

New `#Layer` fields:

```yaml
deployWith: gha            # argocd (default) | gha
workflow:                # read only where deployWith resolves to gha
  repo: pitoniak32/gitops-cd   # owner/name; default this repo
  file: deploy.yaml            # file in <repo>/.github/workflows; default deploy.yaml
  ref: main                    # default main
```

- `deployWith?: "argocd" | "gha"` with `error("deployWith: expected argocd or gha")`.
- `workflow?: {repo?: =~"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", file?: =~"^[A-Za-z0-9_.-]+\\.ya?ml$", ref?: !=""}`, each with an `error(...)` message.
- Merge: per field (`deployWith`, `workflow.repo`, `workflow.file`, `workflow.ref`), target > stage > app > default, same pattern as `cpu`/`memory`.
- `workflow` set where deployWith resolves to argocd: allowed, ignored (an app-level `workflow` serves its GHA targets only).
- `deploys[*]` gains `deployWith` and `workflow: {repo, file, ref}` (always resolved).
- `pipelines[*].stages[*].targets[*]` gains `deployWith`.
- `mise run schemas` regenerates `platform/render/schemas/`.

Default repo: `pitoniak32/gitops-cd`, derived from `_repoURL`.

## 2. Rendering (`platform/render/delivery.cue`)

- `rendered/apps/...`: unchanged. Every target folder is rendered regardless of deployWith.
- `argocdDeploys`: only deploys with `deployWith == "argocd"` → no Application for GHA targets.
- `_appProject` destinations / `_deployRegion`: only argocd deploys. A region with no argocd deploys for a system gets no AppProject.
- Target Stage: label `example.com/deploy-with: <argocd|gha>`. Shared steps `git-clone` … `git-push` unchanged. Then:
  - **argocd**: `argocd-update` (unchanged).
  - **gha**:

```yaml
- uses: http
  as: dispatch
  config:
    method: POST
    url: https://api.github.com/repos/<repo>/actions/workflows/<file>/dispatches
    headers:
      - {name: Authorization, value: "Bearer ${{ sharedSecret('github-actions').token }}"}
      - {name: Accept, value: application/vnd.github+json}
      - {name: X-GitHub-Api-Version, value: "2022-11-28"}
      - {name: Content-Type, value: application/json}
    body: <JSON: ref, return_run_details: true, inputs {system, app, stage, target, commit: ${{ outputs.commit.commit }}, image: <repo>:<tag>@<digest>}>
    successExpression: response.status == 200
    failureExpression: response.status != 200
    outputs:
      - {name: runID, fromExpression: response.body.workflow_run_id}
      - {name: runURL, fromExpression: response.body.html_url}
- uses: http
  as: wait
  retry: {timeout: 30m}
  config:
    method: GET
    url: https://api.github.com/repos/<repo>/actions/runs/${{ outputs.dispatch.runID }}
    headers: <same>
    successExpression: response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'
    failureExpression: response.status != 200 || (response.body.status == 'completed' && response.body.conclusion != 'success')
```

Neither expression true → step Running → Kargo retries until `retry.timeout`.

## 3. Platform pieces

- **Creds** (`platform/cli/internal/creds`): `Run` also applies `kargo-shared-resources/github-actions`, label `kargo.akuity.io/cred-type: generic`, key `token` = `fnox get GITHUB_PAT`. Idempotent (apply). Shards read it from the hub via `kargo-hub-kubeconfig`, like git creds. PAT: fine-grained, `Actions: read and write` on the repos that hold workflows.
- **Example workflow** `.github/workflows/deploy.yaml`:
  - `on: workflow_dispatch`, inputs `system, app, stage, target, commit, image` (all required strings).
  - `run-name: deploy ${{ inputs.system }}/${{ inputs.app }} → ${{ inputs.target }}`.
  - `permissions: contents: read`.
  - Steps: checkout `inputs.commit`; print `deployment.yaml` image + `manifests.yaml`; job summary (sys/app/stage/target, commit, image); exit 0.
- **Example config**: `systems/payments/apps/worker/app.yml`: `stages.dev.deployWith: gha` (staging, prod stay argocd).
- **Docs**: ADR 0020 "GitHub Actions as a deploy method"; GLOSSARY *Deploy with*; `docs/sync-boundaries.md` (workflow file is read from GitHub at dispatch; PAT via `mise run creds`); README/`docs/operations.md` creds note if they list secrets.

## 4. Testing

- Golden (`platform/render/testdata/root`): an app with app-level argocd and one target overridden to gha with a custom `workflow`, one stage-level gha. `UPDATE=1 mise run test`, review diff: Stage steps, missing params file, AppProject destinations.
- Lint cases (`testdata/lint-cases.tsv`): bad `deployWith`, bad `workflow.repo`, bad `workflow.file`.
- `creds_test.go`: secret created with label + key; re-run idempotent.
- `go vet`/`go test`, `platform/appsets/check.sh` via `mise run test`.
- E2E (after push + CI render): `mise run creds`; assemble Freight for `payments/worker`; promote `worker-us-dev`; expect Stage verified, GHA run green with matching commit, `env/payments/worker/us-dev` head == dispatched commit. Negative: revoke/blank token → promotion fails at dispatch.

## Risks to verify in the plan

- Kargo 1.12.5: `sharedSecret()` expression exists and reads generic secrets from `kargo-shared-resources`; else use a per-project secret + `secret()`.
- Kargo 1.12.5 `http` step: Running-and-retry when neither expression matches; `retry.timeout` on steps; `outputs[].fromExpression`; body templating of JSON.
- `return_run_details` field names (`workflow_run_id`, `html_url`) against GitHub REST docs.
- Switching argocd → gha removes the Application; `workloads` AppSet has `preserveResourcesOnDeletion: true`, so the old workload is orphaned (left running), not pruned. Documented in ADR; cleanup is manual.
- Switching gha → argocd: env branch already current, Application created on next render; first sync deploys it.

## Out of scope

- Real deploys from GHA (self-hosted runners, non-K8s platforms).
- GitHub App installation-token minting.
- Kargo verification (AnalysisTemplates) for GHA targets.
- A failure-toggle input on the example workflow (Kargo can't set it per promotion).
