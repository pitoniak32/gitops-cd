# Capabilities and App Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Organize config by capability. Add a `teams/` registry and `owners.yml` files. From those, generate CODEOWNERS and Kargo promote RBAC, and forbid autoPromote on prod stages.

**Architecture:** The platform CUE renders one capability per run. Inputs are routed by their path relative to a *root* dir that holds `capabilities/<cap>/…` and `teams/…`, so test fixtures and the monorepo share the same shape. Each capability render emits a CODEOWNERS fragment, and the repo-level render assembles `.github/CODEOWNERS` from a template.

**Tech Stack:** CUE v0.17.0 (`~/.local/share/mise/installs/cue/0.17.0/cue`; prepend to PATH), bash, jq, yq v4, jj.

**Spec:** `docs/superpowers/specs/2026-11-07-capability-ownership-design.md`

## Global Constraints

- VCS is jj only. Commit with `jj commit -m "<msg>"`. Never use git mutations, and never run `jj git fetch/push`.
- Teams author only YAML. There is no local tooling requirement beyond an editor: lint, render and CODEOWNERS all run in CI.
- Names follow `#Name: =~"^[a-z][a-z0-9-]{0,39}$"`, and capability names follow `^[a-z][a-z0-9-]{0,29}$` (the old `team` tag regex).
- Kargo project is `kargo-<cap>`. Stage branch is `env/<cap>/<app>/<stage>`. Argo CD app is `<cap>-<app>-<target>`, with AppProject `<cap>` and label `example.com/capability`.
- autoPromote error text: `stage <s> deploys to prod targets; autoPromote not allowed`
- Unknown owner error text: `unknown team; allowed: <comma-joined team names>`
- Platform-only paths in CODEOWNERS: `/capabilities/**/owners.yml`, `/capabilities/**/owners.yaml`, `/teams/` → `@acme/platform`. These lines come **last**, because the last match wins.
- Run `cue fmt` on every changed `.cue` file. `platform/scripts/test.sh` must pass at the end of every task.

## Review Focus

1. **An app with the same name as its capability, or an app named `owners`.** Routing must not collide; render normally. Covered by the Task 1 routing test (fixture app `payments` in capability `payments`).
2. **A capability whose apps have no `owners.yml`.** The CODEOWNERS union must still list the capability owners, never produce an ownerless line. Covered by the Task 4 golden (capability `search` fixture).
3. **The same team on both the capability and the app `owners.yml`.** RBAC and CODEOWNERS must list it once. Covered by the Task 3 and Task 4 goldens (fixture app `worker` overrides with an overlapping team).
4. **`owners.yaml` / `teams/x.yaml` with the `.yaml` extension.** These must be routed like `.yml`. Covered by the Task 2 lint case using a `.yaml` team file.
5. **A deleted capability.** Its CODEOWNERS fragment and rendered files must disappear on the next full render. Covered by the Task 4 root-render test.

---

### Task 1: Path-based input routing + rename team → capability

**Files:**
- Create: `platform/scripts/inputs.sh`
- Modify: `platform/input.cue`, `platform/delivery.cue`, `platform/k8s.cue`, `platform/files.cue`, `platform/pipeline.cue`, `platform/scripts/render.sh`, `platform/scripts/lint.sh`, `platform/scripts/test.sh`, `platform/README.md`
- Move: `platform/testdata/team1/` → `platform/testdata/root/capabilities/payments/`
- Create: `platform/testdata/root/capabilities/payments/apps/payments/app.yml` (a copy of `worker/app.yml`, for Review Focus 1)
- Regenerate: `platform/testdata/golden/`

**Interfaces:**
- Produces:
  - `inputs.sh`, sourced; defines `cue_args <root> <cap>`. It fills the global array `CUE_ARGS` with every file matching `<root>/teams/*.y*ml`, `<root>/capabilities/<cap>/{owners.y*ml,stages/*.y*ml,targets/*.y*ml,apps/*/app.y*ml,apps/*/owners.y*ml}`, followed by `--with-context -l 'in:' -l "strings.TrimPrefix(filename, \"<abs-root>/\")" -t capability=<cap>`, plus `-t repo=$GITOPS_REPO` if set.
  - `render.sh <root> <cap> <out-dir>`
  - `lint.sh <root> <cap>`. Output paths are relative to `<root>`.
  - CUE: tag `capability`, alias `_cap`. `in: [relpath]: _`, routed by regex into `stages`, `targets`, `apps`, and (for Task 2) `teams`, `capOwners`, `appOwners`.

- [ ] **Step 1:** Move testdata into place (`mkdir -p platform/testdata/root/capabilities && mv platform/testdata/team1 platform/testdata/root/capabilities/payments`), add `apps/payments/app.yml`, and change `test.sh` to call `render.sh testdata/root payments "$tmp"` and `lint.sh testdata/root payments`.
- [ ] **Step 2:** Run `platform/scripts/test.sh`. Expected: FAIL, because render.sh's arguments changed.
- [ ] **Step 3:** Implement routing in `input.cue`. Replace the `in: [string]: [string]: _` block with this:
  ```cue
  capability: =~"^[a-z][a-z0-9-]{0,29}$" @tag(capability)
  in: [string]: _
  let C = "capabilities/\(capability)/"
  _route: {
  	stages:    "^\(C)stages/([^/]+)\\.ya?ml$"
  	targets:   "^\(C)targets/([^/]+)\\.ya?ml$"
  	apps:      "^\(C)apps/([^/]+)/app\\.ya?ml$"
  	appOwners: "^\(C)apps/([^/]+)/owners\\.ya?ml$"
  	teams:     "^teams/([^/]+)\\.ya?ml$"
  }
  for f, re in _route {(f): {for k, v in in if k =~ re {(regexp.FindSubmatch(re, k)[1]): v}}}
  capOwners: [for k, v in in if k =~ "^\(C)owners\\.ya?ml$" {v}]   // 0 or 1 element
  ```
  Remove the `apps: [!="stages" & !="targets"]` key exclusion (apps is now a plain `[#Name]: #App`). Rename `team`/`_team` to `capability`/`_cap` everywhere, along with the `_teamStages` → `_capStages` and `_teamTargets` → `_capTargets` aliases. Rename the label `example.com/team` → `example.com/capability` and the pipeline field `team:` → `capability:`. `render.sh` and `lint.sh` source `inputs.sh` and use `CUE_ARGS`. render.sh's wipe now uses `$CAP`.
- [ ] **Step 4:** Update lint.sh's `locate()` so `LOC_FILE` is relative to `<root>`: prefix the existing cases with `capabilities/$CAP/`. Leave the yloc branch as it is; it already makes paths relative to the dir it strips (now `<root>`).
- [ ] **Step 5:** Run `UPDATE=1 platform/scripts/test.sh`, then inspect the diff against the old golden. Expected: only `team1` → `payments` renames, plus new `apps/payments/**` output. Then run `platform/scripts/test.sh`. Expected: `golden ok`, `lint ok`.
- [ ] **Step 6:** Run `cue fmt ./platform/...`, update `platform/README.md` usage lines, then `jj commit -m "platform: route inputs by path; team -> capability"`.

### Task 2: Teams registry, owners, validation, editor schemas

**Files:**
- Create: `platform/owners.cue`, `platform/testdata/root/teams/{team1.yml,team2.yaml}`, `platform/testdata/root/capabilities/payments/owners.yml` (`owners: [team1]`), `platform/testdata/root/capabilities/payments/apps/worker/owners.yml` (`owners: [team1, team2]`), `platform/testdata/lint-cases.tsv`, `platform/schemas/{owners,team}.json`
- Modify: `platform/scripts/lint.sh` (locate), `platform/scripts/test.sh`, `platform/scripts/schemas.sh` (`for def in App Stage Target Owners Team`)

**Interfaces:**
- Consumes: `teams`, `capOwners`, `appOwners` from Task 1.
- Produces, in `owners.cue`:
  - `#Team: {githubTeam: =~"^[a-z0-9-]+/[a-z0-9._-]+$", idpGroup: =~"^[A-Za-z0-9._-]+$"}`
  - `teams: [#Name]: #Team`
  - `#Owners: {owners: [_, ...]` of element `[if generic {#Name}, or(_teamNames) | error("unknown team; allowed: \(_teamList)")][0]`, with `list.UniqueItems()` `}`
  - `capOwners: [#Owners] | error("capability needs owners.yml")` (**the capability's owners.yml is required**), `appOwners: [#Name]: #Owners`
  - `owners: {for an, _ in apps {(an): list.SortStrings([for t, _ in {for t in [if appOwners[an] != _|_ {appOwners[an].owners}, capOwners[0].owners][0] {(t): _}} {t}])}}`, which gives each app its deduped, sorted owner list
  - `_allOwners: list.SortStrings([for t, _ in {for _, o in owners for t in o {(t): _}, for t in capOwners[0].owners {(t): _}} {t}])`

- [ ] **Step 1:** Write `lint-cases.tsv` with three tab-separated columns: `<file under testdata/root>`, `<yq -i expr>`, and `<ERE the lint output must match>`.
  ```
  capabilities/payments/owners.yml	.owners = ["nobody"]	^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*unknown team; allowed: team1, team2
  capabilities/payments/apps/worker/owners.yml	.owners = []	^capabilities/payments/apps/worker/owners\.yml:[0-9]+:[0-9]+:
  teams/team2.yaml	.githubTeam = "Bad Name"	^teams/team2\.yaml:[0-9]+:[0-9]+:
  ```
  Extend `test.sh`: for each row, copy `testdata/root` to a tmp dir, run `yq -i "$expr" "$tmp/$file"`, run `lint.sh "$tmp" payments`, and check that the output matches with `grep -E`. Print `lint case ok: <expr>`, or fail.
- [ ] **Step 2:** Run `platform/scripts/test.sh`. Expected: FAIL, because the owners and teams files are not routed or validated yet.
- [ ] **Step 3:** Implement `owners.cue` as specified in Interfaces. Add the `_teamNames` and `_teamList` helpers next to `_stageNames` in `input.cue`.
- [ ] **Step 4:** Add `locate()` cases in `lint.sh`:
  - `capOwners` → `yamlfile capabilities/$CAP/owners`, skipping the index segment
  - `appOwners` → `apps/<app>/owners`
  - `teams` → `yamlfile teams/<t>`, searched under `<root>`, not under the capability
- [ ] **Step 5:** Run `platform/scripts/test.sh`. Expected: all three `lint case ok` lines, `golden ok`, `lint ok`. Run `platform/scripts/schemas.sh`. Then check that `schemas/owners.json` validates `owners: [team1]` and rejects `owners: []` (minItems 1), with yamlls or `npx ajv-cli` if it's available. Otherwise inspect it with jq.
- [ ] **Step 6:** Add `# yaml-language-server: $schema=` modelines to the fixture owners/team files. Paths: `capabilities/<cap>/owners.yml` → `../../platform/schemas/owners.json`; `apps/<app>/owners.yml` → `../../../../platform/schemas/owners.json`; `teams/x.yml` → `../platform/schemas/team.json`. Then `cue fmt`, then `jj commit -m "platform: teams registry and owners"`.

### Task 3: Prod autoPromote rule + Kargo promote RBAC

**Files:**
- Modify: `platform/team.cue` (stage rule), `platform/delivery.cue` (RBAC objects), `platform/testdata/lint-cases.tsv`, `platform/testdata/golden/platform/kargo/payments.yaml` (regenerated)

**Interfaces:**
- Consumes: `deploys` (entries have `app`, `stage`, `target`, `tgt` = target config with `environment`), `owners`, `_allOwners`, `teams`.
- Produces: kargo stream objects per `t` in `_allOwners`, all in namespace `kargo-<cap>`, each carrying label `app.kubernetes.io/managed-by: gitops-platform` and annotation `rbac.kargo.akuity.io/managed: "true"`:
  - `ServiceAccount owner-<t>` with annotation `rbac.kargo.akuity.io/claims: json.Marshal({groups: [teams[t].idpGroup]})`
  - `Role owner-<t>` with these rules:
    - `{apiGroups: ["kargo.akuity.io"], resources: ["stages"], resourceNames: list.SortStrings([for an, o in owners if list.Contains(o, t) for sn, _ in apps[an].stages {"\(an)-\(sn)"}]), verbs: ["promote"]}`
    - `{apiGroups: ["kargo.akuity.io"], resources: ["freights", "stages", "warehouses", "promotions"], verbs: ["get", "list", "watch"]}`
  - `RoleBinding owner-<t>` binding `ServiceAccount owner-<t>` to `Role owner-<t>`

- [ ] **Step 1:** Add the lint case `capabilities/payments/stages/prod.yml` ⇥ `.autoPromote = true` ⇥ `^capabilities/payments/stages/prod\.yml:[0-9]+:[0-9]+: .*stage prod deploys to prod targets; autoPromote not allowed`.
- [ ] **Step 2:** Run `platform/scripts/test.sh`. Expected: FAIL on that case.
- [ ] **Step 3:** In `team.cue`, implement `_prodStages: {for _, d in deploys if d.tgt.environment == "prod" {(d.stage): _}}` and `stages: {for s, _ in _prodStages {(s): autoPromote: false | error("stage \(s) deploys to prod targets; autoPromote not allowed")}}`. In `delivery.cue`, add the RBAC objects from Interfaces.
- [ ] **Step 4:** Run `UPDATE=1 platform/scripts/test.sh` and check the golden diff. Expected: `owner-team1` lists every payments app stage; `owner-team2` lists only `worker-*`. Then run `platform/scripts/test.sh` and expect it to pass.
- [ ] **Step 5:** Run `cue fmt`, then `jj commit -m "platform: prod autoPromote rule; kargo promote RBAC"`.

### Task 4: CODEOWNERS generation

**Files:**
- Modify: `platform/files.cue`, `platform/scripts/render.sh` (wipe adds `codeowners/$CAP`), golden
- Create: `platform/testdata/root/capabilities/search/` (one stage, one target, one app, `owners.yml: [team2]`, no app owners), `platform/scripts/codeowners.sh`, `platform/testdata/root/.github/CODEOWNERS.tmpl`, `platform/testdata/golden-codeowners`

**Interfaces:**
- Produces:
  - `files."codeowners/<cap>"`: text lines in this order:
    - `/capabilities/<cap>/ <@githubTeam of each _allOwners, space-joined>`
    - then per app, sorted: `/capabilities/<cap>/apps/<app>/ <@owners>`
  - `codeowners.sh <root> <rendered-dir>`: writes `<root>/.github/CODEOWNERS`. It reads `<root>/.github/CODEOWNERS.tmpl` and replaces the single line `#GENERATED` with the concatenation of `<rendered-dir>/codeowners/*` (sorted by capability). The template must place the platform-only lines after `#GENERATED`.

- [ ] **Step 1:** Write `golden-codeowners`, the expected full output for the fixture root:
  ```
  # header line(s) from tmpl
  /capabilities/payments/ @acme/team1 @acme/team2
  /capabilities/payments/apps/my-app/ @acme/team1
  /capabilities/payments/apps/payments/ @acme/team1
  /capabilities/payments/apps/worker/ @acme/team1 @acme/team2
  /capabilities/search/ @acme/team2
  /capabilities/search/apps/<app>/ @acme/team2
  /capabilities/**/owners.yml @acme/platform
  /capabilities/**/owners.yaml @acme/platform
  /teams/ @acme/platform
  ```
  Extend `test.sh` so it:
  - renders both fixture capabilities
  - runs `codeowners.sh` on a tmp copy of root and diffs the result against `golden-codeowners`
  - deletes `capabilities/search` from the copy, re-runs the full render + codeowners, and asserts that no `search` line remains (Review Focus 5)
- [ ] **Step 2:** Run `platform/scripts/test.sh`. Expected: FAIL.
- [ ] **Step 3:** Implement the `files` entry (`strings.Join(lines, "\n") + "\n"`) and `codeowners.sh` (bash + awk, no yq).
- [ ] **Step 4:** Run `UPDATE=1 platform/scripts/test.sh`, then `platform/scripts/test.sh`. Expected: pass.
- [ ] **Step 5:** Run `cue fmt`, then `jj commit -m "platform: generate CODEOWNERS"`.

### Task 5: Migrate monorepo, CI, docs

**Files:**
- Move: `teams/team1/` → `capabilities/payments/`, `teams/team2/` → `capabilities/search/`
- Create:
  - `teams/team1.yml`: `githubTeam: acme/team1`, `idpGroup: team1`
  - `teams/team2.yml`: `githubTeam: acme/team2`, `idpGroup: team2`
  - `capabilities/payments/owners.yml`: `owners: [team1]`
  - `capabilities/search/owners.yml`: `owners: [team2]`
  - `.github/CODEOWNERS.tmpl` (the current CODEOWNERS header lines for platform/rendered/.github/scripts, then `#GENERATED`, then the platform-only footer)
  - `docs/adr/0007-capabilities-and-ownership.md`
- Modify: `scripts/render.sh`, `scripts/lint.sh`, `.github/workflows/ci.yaml`, `README.md`, `CLAUDE.md`, `GLOSSARY.md`, `rendered/README.md`
- Regenerate: `.github/CODEOWNERS`, `rendered/**`

**Interfaces:**
- Consumes:
  - `render.sh <root> <cap> <out>`, `lint.sh <root> <cap>` (Task 1)
  - `codeowners.sh <root> <rendered>` (Task 4)

- [ ] **Step 1:** Do the moves (jj tracks them through snapshotting) and add the new files with modelines.
- [ ] **Step 2:** Update the root scripts:
  - `scripts/render.sh` loops over `capabilities/*/`, calling `platform/scripts/render.sh . "$cap" "$OUT"`, wipes `$OUT/codeowners` up front, then runs `platform/scripts/codeowners.sh . "$OUT"`.
  - `scripts/lint.sh [cap...]` calls `platform/scripts/lint.sh . "$cap"`, without the sed prefix since paths are already root-relative, piped through `sort | uniq` so teams/ errors aren't repeated per capability.
- [ ] **Step 3:** Update CI:
  - The "rendered/ is CI-owned" grep also matches `^\.github/CODEOWNERS$`, with the message `edit .github/CODEOWNERS.tmpl or owners.yml instead`.
  - The commit step runs `git add -A rendered .github/CODEOWNERS`.
  - The step name "lint all teams" becomes "lint all capabilities".
- [ ] **Step 4:** Run `PATH=…cue…:$PATH scripts/render.sh && scripts/lint.sh && platform/scripts/test.sh`. Expected:
  - `rendered payments`, `rendered search`
  - lint prints nothing
  - tests pass
  - `.github/CODEOWNERS` matches the layout from Task 4
  - `jj diff --stat` shows rendered paths renamed team → capability
- [ ] **Step 5:** Update the docs:
  - **README, CLAUDE.md:** the layout tree, command usage, and an ownership section that copies the spec's permissions table.
  - **GLOSSARY:** add **Capability**, **Owner** and **Team** (an owning group from `teams/`), and mark "team folder" as _Avoid_.
  - **ADR 0007:** the decision and why, from the spec, citing the promotion-gate reasoning and the CODEOWNERS-on-main tradeoff.
- [ ] **Step 6:** `jj commit -m "migrate to capabilities; CI generates CODEOWNERS; docs"`.
