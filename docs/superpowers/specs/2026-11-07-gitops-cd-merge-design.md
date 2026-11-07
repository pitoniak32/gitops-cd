# Merge CUE gitops POC into kargo-demo (→ gitops-cd)

## Goal

Make `kargo-demo` one repo that holds:
- the local kind and Argo CD platform
- the CUE renderer
- the team YAML (capabilities and teams)
- the generated `rendered/` output

A clean `mise run up` must end with Kargo running the generated capability projects, where doug (team1) can promote a `payments` app from dev to prod on kind. Later the repo gets renamed to `gitops-cd`, by the user, since it's a remote operation.

Source of the POC: `~/code/personal/gitops-cd-initial`. It's a jj repo, and its specs, plans and ADR 0001–0007 come along.

## Layout

```
cue.mod/module.cue      module: "github.com/pitoniak32/gitops-cd@v0" (single CUE module, repo root)
render/                 CUE package `render` (POC platform/*.cue); schemas/, testdata/, scripts/
capabilities/<cap>/     owners.yml, stages/, targets/, apps/<app>/{app,owners}.yml
teams/<team>.yml        githubTeam, idpGroup
cluster/kind.yaml       unchanged
infrastructure/<addon>/ was platform/<app>/ (app.yaml, values.yaml, manifests/)
bootstrap/              root Applications: rendered-argocd, rendered-kargo
rendered/               CI-generated: apps/<cap>/<app>/<stage>/<target>/, argocd/<cap>.yaml,
                        kargo/<cap>.yaml, pipelines/<cap>.json, codeowners/<cap>
mise-tasks/             up, down, infra, infra-render, render, lint, test, schemas, creds
.github/                workflows/ci.yaml, CODEOWNERS.tmpl, CODEOWNERS (generated)
docs/                   adr/ (POC 0001–0007 + 0008 this merge), agents/, specs/, plans/, gotchas.md
GLOSSARY.md CLAUDE.md README.md
```

Removed:
- `example-project/` (to be rebuilt later as a capability) and its ClusterAnalysisTemplates
- `.github/workflows/wait-and-echo.yaml`
- every reference to the `kargo-demo-gitops` repo

## Renames

| Before | After | Why |
|---|---|---|
| POC `platform/` (CUE) | `render/` package `render`, module at repo root | Idiomatic CUE: one module per repo, packages in dirs |
| kargo-demo `platform/` | `infrastructure/` | Argo CD convention for cluster add-ons, so it doesn't clash with "platform team" |
| `mise run platform` | `mise run infra` | matches the dir |
| `mise run render` (inline Argo apps) | `mise run infra-render` | frees `render` for the CUE render |
| POC `scripts/*.sh` | `mise-tasks/{render,lint}` | one task runner; CI uses mise too |
| `rendered/platform/{argocd,kargo}/` | `rendered/{argocd,kargo}/` | flatter; "platform" no longer overloaded |
| `rendered/bootstrap/` | `bootstrap/` | hand-maintained, not generated |

## Rendering changes (CUE)

- **Repo URL:** `repoURL` defaults to `https://github.com/pitoniak32/gitops-cd`.
- **Cluster registry:** replaced by a single `in-cluster` entry: `argocdName: "in-cluster"`, `kind: "kind"`, every environment, `secretStore: false`.
- **Secrets:**
  - New cluster field `secretStore: bool`.
  - When it's false, the renderer omits ExternalSecrets and secret-backed env vars, with a manifest comment saying so.
  - `secret:` values still validate.
  - External Secrets Operator isn't installed.
- **Argo CD AppProjects:** one per capability (`<cap>`), rendered into `rendered/argocd/<cap>.yaml`, with these settings:
  - source repo = repoURL
  - destinations = that capability's target namespaces on `in-cluster`
  - `clusterResourceWhitelist: []`
  - namespace resources allowed
- **Example capabilities:** `payments` (team1) and `search` (team2) are kept. The namespaces become `<cap>-<target>` (one cluster, so namespaces must be unique per target), and the image becomes `ghcr.io/pitoniak32/nginx:<pinned tag>`. Pinned by tag or digest, per CLAUDE.md.

## Platform wiring

- **bootstrap:**
  - `bootstrap/rendered.yaml` holds two Argo CD Applications:
    - `rendered-argocd`, path `rendered/argocd`
    - `rendered-kargo`, path `rendered/kargo`
  - Both use repo `main`, automated sync with prune.
  - `mise run infra` applies it after the infrastructure apps (it replaces `mise run project`).
- **Keycloak:**
  - add groups `team1` (doug) and `team2` (alice)
  - add a `groups` client scope with a group-membership mapper (`full.path: false`), as a default scope on the `kargo` and `argocd` clients
- **Kargo:**
  - `additionalScopes: [groups]`
  - viewers stay email-based
  - promote rights only come from the generated `owner-<team>` roles, via `{"groups": [idpGroup]}`
- **Argo CD:** unchanged (doug is an operator by email). Argo CD RBAC per capability is out of scope.
- **Git credentials:**
  - `mise run creds` reads `GITHUB_PAT` from fnox and applies a Secret labeled `kargo.akuity.io/cred-type: git` into Kargo's shared/global credentials namespace.
  - The Kargo chart is configured to read from that namespace.
  - Generated projects need no secret of their own.
- **Gitops-only admission policy:** unchanged. It matches only `kargo-api`, while Argo CD applies the generated resources.

## Tooling and CI

- **`.mise.toml`:** adds `cue = "0.17.0"` and `jq`. yq is already pinned.
- **`ci.yaml`** (from the POC):
  - uses `jdx/mise-action` to install the tools
  - runs `mise run test`, `mise run lint`, `mise run render`
  - keeps the POC's guards, PR diff comment and render commit on main (`RENDER_BOT_TOKEN`)
  - the paths change to match the layout above

## Docs

- **CLAUDE.md:** the two files merged. Keep kargo-demo's rules (reproducible `up`, gitops-only, pinned versions, idempotent tasks, fnox, jj) and the POC's rules (teams write only YAML, `error()` messages, field shadowing).
- **README:** kargo-demo's quickstart and logins, plus the POC's layout, ownership and flow sections.
- **GLOSSARY:** add Infrastructure and Bootstrap.
- **ADR 0008:** the merge, idiomatic layout, and skipping secrets on kind.
- **gotchas.md:** kept, plus any new failures found along the way.

## Verification

1. `mise run test` passes: golden, CODEOWNERS and lint cases, updated for the new names.
2. `mise run lint` is clean, and `mise run render` reproduces the committed `rendered/`.
3. **Manual, with the user:**
   - The user pushes to GitHub.
   - A clean `mise run up` brings everything up: Argo CD shows `rendered-argocd`/`rendered-kargo` Synced, and Kargo shows projects `kargo-payments` and `kargo-search`.
   - doug (team1) can promote `my-app` dev→staging→prod.
   - alice can't promote in `kargo-payments`.
   - prod stages never auto-promote.

## Out of scope

- Infrastructure apps from git (inlining stays)
- External Secrets Operator
- Argo CD RBAC per capability
- multi-cluster
- the managers role
- rebuilding the example project

## Unverified

- The Kargo 1.12 chart setting and namespace for shared/global git credentials.
- The exact Keycloak JSON for the group-membership mapper.

Both get checked against the docs and the running cluster during implementation, and recorded in `gotchas.md`.
