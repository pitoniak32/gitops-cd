# gitops-cd Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the CUE gitops POC (`~/code/personal/gitops-cd-initial`) into kargo-demo, so that one repo renders capabilities and Kargo/Argo CD on kind run them.

**Architecture:** A single CUE module sits at the repo root, with the renderer in package `render/`. Cluster add-ons live in `infrastructure/`, still inlined into Argo CD apps by `mise run infra`. `bootstrap/rendered.yaml` makes Argo CD sync `rendered/{argocd,kargo}` from GitHub. Keycloak groups drive the generated Kargo promote roles.

**Tech Stack:** CUE 0.17.0, mise, kind, Argo CD, Kargo 1.12.5, Keycloak, bash/jq/yq, jj.

**Spec:** `docs/superpowers/specs/2026-11-07-gitops-cd-merge-design.md`

## Global Constraints

- **VCS:** jj only. Commit with `jj commit -m`. No git mutations, no `jj git push/fetch`. The user pushes.
- **`mise run up`** must go from nothing to working, and every `mise-tasks/*` script must be idempotent.
- **Versions:** pin tools in `.mise.toml`, charts by `targetRevision`, images by tag.
- **Gitops-only:** no UI user gets create/edit/delete rights. Promote rights come only from the generated `owner-<team>` roles.
- **Secrets** come only from fnox. Nothing secret goes in git.
- **Teams write only YAML.** User-facing errors come from CUE `error("…")`.
- Repo URL: `https://github.com/pitoniak32/gitops-cd`. CUE module: `github.com/pitoniak32/gitops-cd@v0`. Package: `render`.
- `render/scripts/test.sh` must pass at the end of every task from Task 1 on.

## Review Focus

1. **The env branch push before the user has pushed main or created the repo.** Promotion fails, and the expected behavior is a clear Kargo error, not a silent hang. Covered in `docs/gotchas.md` by Task 5 (README states the order: push, then `mise run up`).
2. **Re-running `mise run infra` on a cluster where the rendered apps already exist.** It must stay idempotent (`kubectl apply`, no create-only). Task 5's verification runs it twice.
3. **A secret-backed env var on a `secretStore: false` cluster.** The env var is omitted and the deployment still renders and is valid. Task 4 adds a golden case with `secret:` in `payments/my-app`.
4. **The CUE module moving to the repo root.** Running `cue` in `render/` must still resolve `cue.dev/x/k8s.io`, and CI's cache key must change to `cue.mod/module.cue`. Task 1's test run catches the first; Task 3 owns the CI path.
5. **Keycloak users without groups (bob).** They log in, see projects as a viewer, and can't promote. Covered by the manual check in Task 5.

---

### Task 1: Import the CUE renderer, team YAML and docs

**Files:**
- Copy from `~/code/personal/gitops-cd-initial`:
  - `platform/*.cue`, `platform/{schemas,testdata,scripts,README.md}` → `render/`
  - `capabilities/`, `teams/`
  - `.github/{CODEOWNERS.tmpl,CODEOWNERS,workflows/ci.yaml}`
  - `rendered/`, except `rendered/bootstrap` → `bootstrap/`
  - `GLOSSARY.md`, `docs/{adr,agents}`
  - `docs/superpowers/{specs,plans}/*`
- Create: `cue.mod/module.cue` (`module: "github.com/pitoniak32/gitops-cd@v0"`, the POC's `language` and `deps` copied verbatim)
- Modify: every `render/*.cue` (`package platform` → `package render`)

**Interfaces:**
- Produces: `render/scripts/{render,lint,codeowners,test,schemas,inputs}.sh`, with the same CLIs as the POC (`render.sh <root> <cap> <out>`, `lint.sh <root> <cap>`, `codeowners.sh <root> <out>`).

- [ ] **Step 1:** Copy the files (`cp -R`). Don't copy `.jj`, POC root `scripts/`, POC `README.md`/`CLAUDE.md`, or `platform/cue.mod`.
- [ ] **Step 2:** Run `render/scripts/test.sh`. Expected: FAIL, because of `package platform` and no module at the root.
- [ ] **Step 3:** Rename the package, create `cue.mod/module.cue`, and run `cue mod tidy` from the repo root.
- [ ] **Step 4:** Run `render/scripts/test.sh`. Expected: `golden ok`, `codeowners ok`, `codeowners prune ok`, `override-all owners ok`, `lint ok`, 5 `lint case ok`, 3 `bad name ok`.
- [ ] **Step 5:** `jj commit -m "import CUE renderer, capabilities, teams, docs from POC"`

### Task 2: `platform/` → `infrastructure/`; retire example-project

**Files:**
- Move: `platform/` → `infrastructure/`
- Rename: `mise-tasks/platform` → `mise-tasks/infra`, `mise-tasks/render` → `mise-tasks/infra-render`
- Modify: `mise-tasks/{infra,infra-render,up}`, and `infrastructure/kargo/values.yaml` comments that reference `platform/` or `example-project`
- Delete:
  - `example-project/`
  - `mise-tasks/project`
  - `.github/workflows/wait-and-echo.yaml`
  - the `example-project/gh_app.yml` line in `.gitignore`

**Interfaces:**
- Produces: `mise run infra`, `mise run infra-render [apps…]` (same behavior as the old `platform`/`render`).

- [ ] **Step 1:** Before the move, save the baseline with `mise run render > $TMP/before.yaml`.
- [ ] **Step 2:** Move and rename. Inside the tasks, `platform/` becomes `infrastructure/`, `mise run render` becomes `mise run infra-render`, and `up` drops `mise run project`.
- [ ] **Step 3:** Run `mise run infra-render > $TMP/after.yaml && diff $TMP/before.yaml $TMP/after.yaml`. Expected: no diff, except comment lines that mention `example-project`.
- [ ] **Step 4:** Run `rg -n 'example-project|kargo-demo-gitops|platform/' -g '!render/**' -g '!docs/**' -g '!rendered/**'`. Expected: no hits outside the CUE/POC docs.
- [ ] **Step 5:** `jj commit -m "platform -> infrastructure; retire example-project"`

### Task 3: mise tasks for render/lint/test; flatten rendered paths; CI on mise

**Files:**
- Create:
  - `mise-tasks/render`: the POC's `scripts/render.sh` body; `#MISE description`; optional out-dir arg
  - `mise-tasks/lint`: the POC's `scripts/lint.sh`; `#USAGE arg "[caps]..."`
  - `mise-tasks/test`: runs `render/scripts/test.sh`
  - `mise-tasks/schemas`: runs `render/scripts/schemas.sh`
- Modify:
  - `.mise.toml`: add `cue = '0.17.0'`, `jq = '1.8.1'`
  - `render/files.cue`: `"platform/kargo/…"` → `"kargo/…"`, `"platform/argocd/…"` → `"argocd/…"`
  - `render/scripts/render.sh` (wipe paths)
  - `bootstrap/*.yaml` (paths `rendered/argocd`, `rendered/kargo`)
  - `.github/workflows/ci.yaml`:
    - `jdx/mise-action@v2` replaces the curl cue install
    - steps call `mise run test|lint|render`
    - cache key `cue.mod/module.cue`
    - schema check runs `mise run schemas && git diff --exit-code render/schemas/`
    - guard regex `^rendered/(apps|argocd|kargo|pipelines|codeowners)/`
    - `cue fmt --check ./render/...`
  - golden (regenerated), `render/README.md`, `rendered/README.md`

- [ ] **Step 1:** Change `render/files.cue` paths, then run `render/scripts/test.sh`. Expected: FAIL, golden diff only on the `platform/` → flat paths.
- [ ] **Step 2:** Run `UPDATE=1 render/scripts/test.sh`, check that the diff is renames only, then run `render/scripts/test.sh` and expect a pass.
- [ ] **Step 3:** Create the tasks and update `.mise.toml`, then run `mise install && mise run test && mise run lint && mise run render`. Expected: tests pass, lint is silent, and `rendered payments` / `rendered search` print. `jj diff --stat rendered` should show only the path flattening.
- [ ] **Step 4:** Run `rg -n 'scripts/(render|lint)\.sh|rendered/platform|platform/scripts' -g '!docs/**'`. Expected: no hits.
- [ ] **Step 5:** `jj commit -m "mise tasks for render/lint/test; flatten rendered paths; CI via mise"`

### Task 4: kind cluster registry, secretStore, AppProjects, example data

**Files:**
- Modify:
  - `render/clusters.cue`: `clusters` = `{"in-cluster": {kind: "kind", environments: ["dev", "staging", "perf", "prod"], secretStore: false}}`; `#Cluster.kind` adds `"kind"`; `#Cluster` adds `secretStore: *true | bool`
  - `render/k8s.cue`: if `!clusters[d.tgt.cluster].secretStore`, skip the ExternalSecret and the secret-backed env entries
  - `render/delivery.cue`: AppProject
  - `render/input.cue`: repoURL default
  - `capabilities/*/targets/*.yml`, `capabilities/*/apps/*/app.yml`
  - the matching `render/testdata/root/**`
  - golden

**Interfaces:**
- Produces: in the `argocd` list, one `AppProject` per capability, before the Applications:
  ```
  {apiVersion: "argoproj.io/v1alpha1", kind: "AppProject",
   metadata: {name: _cap, namespace: "argocd"},
   spec: {sourceRepos: [_repoURL],
          destinations: [for ns in <sorted unique target namespaces> {name: "in-cluster", namespace: ns}],
          clusterResourceWhitelist: [],
          namespaceResourceWhitelist: [{group: "*", kind: "*"}]}}
  ```
- Produces: `repoURL: *"https://github.com/pitoniak32/gitops-cd" | string @tag(repo)`.

- [ ] **Step 1:** Update the fixture `render/testdata/root`:
  - every target `cluster: in-cluster`
  - namespace `<cap>-<target>`, unique per target, because the placement check forbids two targets of one app sharing a cluster and namespace
  - images `ghcr.io/pitoniak32/nginx:1.27`
  - keep a `secret:` env value in `my-app`

  Add to `render/scripts/test.sh` these assertions against the render output:
  - `yq -e 'select(.kind=="AppProject" and .metadata.name=="payments")' argocd/payments.yaml`
  - `! grep -q ExternalSecret -r apps/`
  - `! grep -q secretKeyRef -r apps/`
- [ ] **Step 2:** Run `render/scripts/test.sh`. Expected: FAIL (unknown cluster `in-cluster`).
- [ ] **Step 3:** Implement the changes from the Interfaces block.
- [ ] **Step 4:** Run `UPDATE=1 render/scripts/test.sh`, review the diff, then run `render/scripts/test.sh` and expect a pass. Run `mise run schemas`. Expected: the `target.json` cluster enum is now `["in-cluster"]`.
- [ ] **Step 5:** Apply the same data changes to the real `capabilities/` (team1 → payments, team2 → search). Then run `mise run lint && mise run render`. Expected: clean, and `rendered/` shows the new namespaces, image and AppProjects.
- [ ] **Step 6:** `jj commit -m "render: kind in-cluster, skip secrets without store, AppProjects"`

### Task 5: Platform wiring: bootstrap, Keycloak groups, Kargo creds

**Files:**
- Create:
  - `bootstrap/rendered.yaml`: Applications `rendered-argocd` and `rendered-kargo`
    - namespace `argocd`, project `default`
    - source `{repoURL: https://github.com/pitoniak32/gitops-cd, targetRevision: main, path: rendered/argocd | rendered/kargo}`
    - destination `in-cluster`
    - `syncPolicy.automated: {prune: true, selfHeal: true}`
  - `mise-tasks/creds`: idempotent; `fnox exec`, then `kubectl apply` a git cred Secret into the shared credentials namespace
- Delete: the old `bootstrap/platform-apps.yaml`, replaced by the above
- Modify:
  - `infrastructure/keycloak/manifests/kargo-realm.json`:
    - `groups: [{name: team1}, {name: team2}]`
    - doug `groups: ["/team1"]`, alice `groups: ["/team2"]`
    - client scope `groups` with protocol mapper `oidc-group-membership-mapper` (`claim.name: groups`, `full.path: "false"`, `id.token.claim`, `access.token.claim`, `userinfo.token.claim`: `"true"`)
    - add `groups` to `defaultClientScopes` of `kargo` and `argocd`
  - `infrastructure/kargo/values.yaml`:
    - `additionalScopes: [groups]`
    - the global/shared credentials namespace setting, after checking the Kargo 1.12.5 chart's values (`helm show values kargo --repo … --version 1.12.5 | rg -n -i 'globalCredentials|sharedResources'`)
  - `mise-tasks/infra`: after the loop, run `mise run creds` and `kubectl apply -f bootstrap/rendered.yaml`
  - `docs/gotchas.md`

- [ ] **Step 1:** Look up the credentials setting (above) and the Keycloak group-mapper JSON (Keycloak docs or an export from the running realm). Record both in `docs/gotchas.md`.
- [ ] **Step 2:** Make the edits. Check them with `mise run infra-render kargo keycloak | kubectl apply --dry-run=server -f -` on a running cluster, or with `--dry-run=client` if none is up.
- [ ] **Step 3:** **Gate (the user pushes):** ask the user to create and push `pitoniak32/gitops-cd` (or push the renamed repo). Then run `mise run up`. Expected:
  - it ends with `ready: https://kargo.localhost`
  - `kubectl -n argocd get app rendered-argocd rendered-kargo` shows Synced/Healthy
  - `kubectl get projects.kargo.akuity.io` lists `kargo-payments` and `kargo-search`
- [ ] **Step 4:** Run `mise run infra` again. Expected: completes, no errors (idempotent).
- [ ] **Step 5:** Manual checks with the user:
  - doug logs into Kargo and promotes `my-app` to dev, then staging and prod
  - alice and bob see `kargo-payments` but get a permission error when promoting
  - prod doesn't auto-promote

  Record any new failure in `gotchas.md`.
- [ ] **Step 6:** `jj commit -m "bootstrap rendered apps; keycloak groups; kargo shared git creds"`

### Task 6: Docs merge

**Files:**
- Modify: `CLAUDE.md`, `README.md`, `GLOSSARY.md`, `render/README.md`
- Create: `docs/adr/0008-merge-into-gitops-cd.md`

- [ ] **Step 1:** Merge the docs:
  - **CLAUDE.md:** kargo-demo's rules, with paths updated to `infrastructure/` and `mise run infra`/`infra-render`, plus the POC rules and the `## Agent skills` block, copied verbatim from the POC.
  - **README:** quickstart, logins (with a groups column) and How-it-works from kargo-demo, plus Layout, Ownership and Flow from the POC. State the push-then-`up` order.
  - **GLOSSARY:** add **Infrastructure** (cluster add-ons managed by Argo CD; _Avoid_: platform) and **Bootstrap** (root Applications applied by `mise run infra`).
  - **ADR 0008:** the merge, the idiomatic layout, and skipping secrets on kind.
- [ ] **Step 2:** Run `rg -n 'platform/|scripts/(render|lint)|example-project|kargo-demo-gitops|acme/gitops' CLAUDE.md README.md GLOSSARY.md render/README.md rendered/README.md`. Expected: no stale hits.
- [ ] **Step 3:** `jj commit -m "docs: merge CLAUDE.md/README, ADR 0008"`
