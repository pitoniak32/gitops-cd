# Renderer + CI tooling in Go Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `platform/render/scripts/*.sh`, `platform/appsets/check.sh` and the render/lint/codeowners/schemas/test mise task bodies with `gcd` subcommands and `go test`, on the CUE Go library.

**Architecture:** New `platform/cli/internal/` packages: `cueload` (one CUE value builder), `render`, `yamlpos`, `lint`, `yamledit` (test helper), `schemas`, `appsets` (tests only), `cuefmt` (test only). Mise tasks become one-line `exec .bin/gcd …`. Five PRs, each deleting the scripts it replaces.

**Tech Stack:** Go 1.27.2, `cuelang.org/go` v0.17.0, `gopkg.in/yaml.v3`, cobra, jj.

**Spec:** `docs/superpowers/specs/2026-11-11-go-ci-tooling-design.md`

## Global Constraints

- `cuelang.org/go` exactly `v0.17.0` (= `.mise.toml` `cue = '0.17.0'`).
- Golden (`platform/render/testdata/golden`) and `.github/CODEOWNERS` byte-identical after every task; `UPDATE=1 mise run test` still rewrites golden.
- `mise run lint` output identical to `lint.sh` on every lint case and on `systems/` (format `file:line:col: [label: ]message`, sorted, deduped; exit 1 on any diagnostic).
- `mise run render [out]`, `mise run lint [caps…]`, `mise run codeowners`, `mise run schemas`, `mise run test` keep their args and stdout lines (`rendered <sys>`, `rendered regions`, `wrote .github/CODEOWNERS`, `wrote <schema file>`).
- `schemas/`: content-equal (`jq -S`) to today; key order may change.
- Teams never write CUE; messages still come from CUE `error()`.
- jj only; one jj change + bookmark per PR; never push. Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Each PR: update `CLAUDE.md` (Layout/Commands) and any doc naming a deleted script (`platform/render/README.md`, `regions.cue`, `files.cue`, `input.cue` comments, `docs/adr/0022`).

## Review Focus

1. Library error set vs `cue vet -c -E`: a lint case whose message only appears via a nested/follow-on path. Task 2's transitional diff test covers every case.
2. System with no `environment-variables/` or no `stages/` dir (empty globs): render must still work. Task 1 test `TestSystemEmptyGlobs`.
3. `.yaml` vs `.yml` inputs: both picked up and located. Task 2 test `TestLintYamlExtension`.
4. `GITOPS_REPO` set: `repo` tag passed. Task 1 test `TestRepoTag`.
5. Stale outputs: re-render after deleting an app removes its files. Task 1 test `TestRenderPrunes`.

---

### Task 0: ADR 0026

**Files:** Create `docs/adr/0026-renderer-tooling-in-go.md` (format of `docs/adr/0018-go-cli-for-setup.md`).

- [ ] **Step 1:** Write ADR: decision (renderer/lint/schemas/CI checks in `gcd` on `cuelang.org/go` v0.17.0; CUE stays the rule language; ADR 0006 unchanged), why (awk over CUE text output, yq per assertion, shared helpers), consequences (cue pin and library pin move together).
- [ ] **Step 2:** Commit with Task 1 (same PR).

### Task 1 (PR 1): `cueload` + `render` + `gcd render`/`codeowners`

**Files:**
- Create: `platform/cli/internal/cueload/cueload.go`, `cueload_test.go`
- Create: `platform/cli/internal/render/render.go`, `codeowners.go`, `render_test.go`
- Create: `platform/cli/internal/cmd/render.go`, `codeowners.go`
- Modify: `platform/cli/internal/regions/regions.go:32-45` (use `cueload.Platform`, drop `exec cue`)
- Modify: `platform/mise-tasks/render`, `platform/mise-tasks/codeowners` → `#MISE depends=["cli"]` + `exec "$MISE_PROJECT_ROOT/.bin/gcd" <cmd> "$@"`
- Modify: `platform/render/scripts/test.sh`: `scripts/render.sh <r> <sys> <out>` → `"$GCD" render <out> --root <r> --system <sys>`; `regions.sh`/`codeowners.sh` likewise (`GCD=$MISE_PROJECT_ROOT/.bin/gcd`; test task gains `depends=["cli"]`). `inputs.sh` stays (lint.sh) until Task 2.
- Delete: `platform/render/scripts/{render,regions,unique-apps,codeowners}.sh`

**Interfaces (Produces):**
- `cueload.System(root, cap string) (cue.Value, error)`: load package `.` in `<root>/platform/render` with `load.Config{Dir, Tags: ["system=<sys>", "repo=$GITOPS_REPO" if set]}`; for each input (globs in this order, `filepath.Glob`: `teams/*.y*ml`, `systems/<sys>/owners.y*ml`, `stages/*.y*ml`, `targets/*.y*ml`, `targets/*/target.y*ml`, `targets/*/environment-variables/*.y*ml`, `apps/*/app.y*ml`, `apps/*/owners.y*ml`) `yaml.Extract(absPath, bytes)` → `ctx.BuildFile` → `FillPath(cue.MakePath(cue.Str("in"), cue.Str(relPath)))`. Returns build/extract errors as-is (they carry positions).
- `cueload.Platform(root string, files ...string) (cue.Value, error)`: `load.Instances(files, {Dir: <root>/platform/render})`.
- `cueload.Generic(root string) (cue.Value, error)`: package with tag `generic=true`.
- `cueload.Inputs(root, cap string) []string` (relative paths; used by lint).
- `render.System(root, cap, out string) ([]string, error)`: `v.Validate(cue.Concrete(true))` (error returned unchanged), decode `files` to `map[string]string`, remove `apps/<sys>`, `kargo/<sys>.yaml`, `argocd/projects/*/<sys>.yaml`, `argocd/deploys/*/<sys>`, `pipelines/<sys>.json`, `codeowners/<sys>`, write each file (0644, dirs 0755); returns written paths sorted.
- `render.Regions(root, out string) error`: `Platform(root, "clusters.cue", "regions.cue")`, `regionFiles` map, `rm -rf <out>/infrastructure` first.
- `render.UniqueApps(out string) error`: per `<out>/argocd/deploys/<r>/`, JSON `.appName` duplicates → error lines `duplicate workload Application name <n> in region <r>: rename a system or app` (joined with `\n`).
- `render.CodeOwners(root, out string) error`: rules of `codeowners.sh` (exactly one `#GENERATED`; fragments `<out>/codeowners/*` sorted, concatenated; per `teams/*.y*ml` line `/teams/<base> @<githubTeam>[ <owners of the template's "/teams/" line>]`).
- `render.All(root, out string, w io.Writer) error`: the `render` task (wipe generated dirs, each system → `rendered <sys>`, Regions, UniqueApps, `rendered regions`, CodeOwners, copy `platform/render/rendered-README.md` to `<out>/README.md`).
- `gcd render [out] [--root <dir>] [--system <sys>]…` (default out `rendered`, root = repo; `--system` renders only those, no wipe of other caps, no Regions/CodeOwners: the `render.sh` replacement `test.sh` calls until Task 3), `gcd codeowners` (render to temp dir, CodeOwners, print `wrote .github/CODEOWNERS`).

- [ ] **Step 1: Failing tests** in `render_test.go`:
  - `TestGolden`: render `testdata/root` payments+search + Regions into `t.TempDir()`; compare tree to `platform/render/testdata/golden` file by file (missing/extra/different → `t.Errorf` with path; first differing line). `UPDATE=1` → replace golden dir and `t.Skip("updated")`.
  - `TestCodeOwnersGolden`: copy `testdata/root` + repo `.github/CODEOWNERS.tmpl` the way `test.sh:185-195` does; result equals `testdata/golden-codeowners`.
  - `TestUniqueApps`: the case at `test.sh:313-326` → error contains `duplicate workload Application name`.
  - `TestSystemEmptyGlobs`, `TestRepoTag` (`GITOPS_REPO=https://example.com/x` → a Warehouse git `repoURL` equals it), `TestRenderPrunes` (render, delete `apps/worker`, render → no `apps/payments/worker`).
- [ ] **Step 2:** `go test ./internal/render/...` → FAIL (package missing).
- [ ] **Step 3:** Implement `cueload`, `render`, cmds; `go get cuelang.org/go@v0.17.0 && go mod tidy`.
- [ ] **Step 4:** `go test ./...` PASS; `mise run render /tmp/x` then `diff -r` vs old `render.sh` output from `main` (run before deleting scripts) → identical; `mise run regions && jj diff --stat` empty; `mise run test` PASS.
- [ ] **Step 5:** CI: `.github/workflows/ci.yaml` needs `go` before render (already in `install_args`); confirm `mise run render` builds `cli`. `jj describe -m "feat(cli): gcd render + codeowners on the CUE Go library (ADR 0026)"`, `jj new`, bookmark `go-render`.

### Task 2 (PR 2): `yamlpos` + `lint` + `gcd lint`

**Files:**
- Create: `platform/cli/internal/yamlpos/yamlpos.go`, `yamlpos_test.go`
- Create: `platform/cli/internal/lint/lint.go`, `locate.go`, `lint_test.go`, `compat_test.go` (transitional)
- Create: `platform/cli/internal/cmd/lint.go`
- Modify: `platform/mise-tasks/lint` → `exec gcd lint "$@"`; `scripts/test.sh` lint calls → `gcd lint` via a small wrapper until Task 3
- Delete (end of task): `platform/render/scripts/lint.sh`, `inputs.sh`, `compat_test.go`

**Interfaces (Produces):**
- `yamlpos.Find(file string, path []string) (line, col int, ok bool)`: walk `yaml.v3` document; string segment = map key (return the key node's position), all-digits segment = sequence index (item position).
- `lint.Diag{File string; Line, Col int; Label, Msg string}`; `(Diag) String()` = `File:Line:Col: [Label: ]Msg`.
- `lint.System(root, cap string) ([]Diag, error)` (error only for I/O, not CUE): from `errors.Errors(err)` of `cueload.System` + `Validate(cue.Concrete(true))`:
  - path = `e.Path()`; msg = `fmt.Sprintf(e.Msg())`; yaml position = first of `errors.Positions(e)` whose filename ends `.yml`/`.yaml`.
  - drop disjunction-header errors (`errors in empty disjunction`); drop an error whose path has a reported path as a proper prefix; drop `conflicting values…` when another error has the same path.
  - `locate(path)` table = `lint.sh:27-48` verbatim semantics (file, query path, label per top-level field, `stageNameErrors` included). Position: yaml position from the error if its file is under root, else `yamlpos.Find`, else `1:1`.
- `lint.All(root string, caps []string) ([]Diag, error)`: default every `systems/*`; sort by file, line, col; dedupe.
- `gcd lint [caps…]`: print diags, exit 1 if any.

- [ ] **Step 1: Failing tests:**
  - `yamlpos_test.go`: leading `# yaml-language-server: $schema=…` + blank line → key on file line 3 reports 3 (no offset hack); quoted keys; list index.
  - `compat_test.go`: for every row of `testdata/lint-cases.tsv` and the dir-rename cases, apply the edit (via `yq -i`, transitional), run `scripts/lint.sh` and `lint.System`; outputs equal (`t.Skip` if `lint.sh` already deleted).
  - `lint_test.go`: `TestLintClean` (fixture lints clean), `TestLintYamlExtension` (rename `stages/dev.yml` → `.yaml`, break it, diag file ends `.yaml`).
- [ ] **Step 2:** `go test ./internal/lint/... ./internal/yamlpos/...` FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Tests PASS; `mise run lint` clean on repo; CI lint step (sed to `::error`) unchanged.
- [ ] **Step 5:** Delete `lint.sh`, `inputs.sh`, `compat_test.go`; `mise run test` PASS. `jj describe -m "feat(cli): gcd lint from CUE error values"`, `jj new`, bookmark `go-lint`.

### Task 3 (PR 3): `yamledit` + `test.sh` → Go tests + cue fmt test

**Files:**
- Create: `platform/cli/internal/yamledit/yamledit.go`, `yamledit_test.go`
- Create: `platform/cli/internal/render/testutil_test.go` (fixture + doc query helpers), `kargo_test.go`, `argocd_test.go`, `rbac_test.go`, `gha_test.go`, `terraform_test.go`, `cloudrun_test.go`
- Create: `platform/cli/internal/lint/cases_test.go` (replaces `lint-cases.tsv` + `test.sh` lint blocks)
- Create: `platform/cli/internal/cuefmt/cuefmt_test.go`
- Modify: `platform/mise-tasks/test` → gofmt + `go vet ./...` + `go test ./...` (in `platform/cli`) + `platform/tests/tf-module-push.sh`; `ci.yaml` "renderer tests" step → `mise run test` only
- Delete: `platform/render/scripts/test.sh`, `platform/render/testdata/lint-cases.tsv`

**Interfaces (Produces):**
- `yamledit.Set(file string, path []string, value any) error`, `Del(file string, path []string) error`, `Rename(file string, path []string, newKey string) error`: `yaml.v3` node edit, comments kept, 2-space indent on write.
- test helpers (package `render`, `_test.go`): `fixture(t) (root string)` copies `testdata/root` to `t.TempDir()`; `renderFixture(t, root) (out string)`; `docs(t, file) []map[string]any` (multi-doc); `find(docs, kind, name) map[string]any`; `get(m, "spec.shard") any` (dot path, quoted segments for dotted keys).

- [ ] **Step 1:** Port every `test.sh` block (sections ending `echo "<x> ok"` at lines 14–403) to a Go test named after the section (`TestRegions`, `TestKindCluster`, `TestGhaTargets`, `TestTerraformRender`, `TestTerraformKargo`, `TestCloudRunRender`, `TestRenderedBranch`, `TestTargetSchema`, `TestLegacyAppOwners`, `TestTargetStageNameCollision`, `TestTargetDoneCollision`, `TestTargetLayout`, `TestEnvFromPrecedence`, `TestEnvFromClash`, `TestControlFlowGraph`, `TestClusterTypes`, `TestKargoStageNameLength`, `TestTargetNameConvention`), each assertion keeping its expected value and failure message. Lint cases: one table `[]struct{name, file string; edit func(t, root); want *regexp.Regexp}`, rows = `lint-cases.tsv` rows in order, edits written with `yamledit`.
- [ ] **Step 2:** Mutation check: break one fixture expectation per file (e.g. change a want string) → that test FAILS with its message; revert.
- [ ] **Step 3:** `cuefmt_test.go`: every `.cue` under `platform/render` (excluding `cue.mod`): `format.Source(b)` equals `b`, else fail naming the file.
- [ ] **Step 4:** `mise run test` PASS; old `test.sh` on the same tree PASS (run before deleting); count Go assertions ≥ `test.sh` checks.
- [ ] **Step 5:** Delete `test.sh`, `lint-cases.tsv`; `jj describe -m "test(cli): renderer tests in Go"`, `jj new`, bookmark `go-tests`.

### Task 4 (PR 4): `check.sh` → `internal/appsets` tests

**Files:** Create `platform/cli/internal/appsets/appsets_test.go`; delete `platform/appsets/check.sh`; `mise-tasks/test` drops it.

- [ ] **Step 1:** One test per `check.sh` rule (lines 7–77), same messages: addon dir ↔ AppSet, no `infrastructure/*/app.yaml`, no `__SHARD__`, AppSet `goTemplate`/`missingkey=error`/`preserveResourcesOnDeletion`/no finalizers/valueFiles exist, addon `retry.limit == -1`, region selector `Exists` + `values.region` text, projects `allowEmpty`, chart source `sources[0]`, kargo + argo-cd pin groups, root app path + bootstrap AppSets, workloads template keys ⊆ golden deploy params keys, `rendered` revision rule.
- [ ] **Step 2:** Mutation check per rule on a temp copy (e.g. set `goTemplate: false`) → FAIL with message.
- [ ] **Step 3:** `mise run test` PASS; `jj describe -m "test(cli): appsets checks in Go"`, `jj new`, bookmark `go-appsets`.

### Task 5 (PR 5): `schemas` + `gcd schemas`

**Files:** Create `platform/cli/internal/schemas/schemas.go`, `schemas_test.go`, `platform/cli/internal/cmd/schemas.go`; `mise-tasks/schemas` → `exec gcd schemas "$@"`; delete `platform/render/scripts/schemas.sh`; `ci.yaml` `install_args` drops `cue jq` if no `ci.yaml` step still uses them.

**Interfaces (Produces):**
- `schemas.Generate(root, outDir string, w io.Writer) error`: `cueload.Generic`; for `App Stage Target EnvironmentVariables Owners Team`: `jsonschema.Generate(v.LookupPath(cue.MakePath(cue.Def("#"+d))), &jsonschema.GenerateConfig{})` (= what `cue def --out jsonschema` calls), `format.Node` → JSON → `any`; apply passes; write `<out>/<kebab>.json` with 2-space indent + trailing newline; print `wrote <file>`.
- passes (one func each, `func(any) any`, `schemas.sh:14-51` semantics): `dropFalseAnyOf`, `inlineRefProps`, `propertyNamesMessages`, `foldNotPattern`, `patternMessages`, `patternErrorMessage`.

- [ ] **Step 1: Failing tests:** one per pass on a tiny input/output pair from the jq; `TestGenerateMatchesCommitted`: generate to temp, each file `reflect.DeepEqual` (after JSON decode) to committed `platform/render/schemas/*.json`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS; `mise run schemas`, `jj diff` shows key-order-only changes (`jq -S` equal per file); open `systems/payments/apps/worker/app.yml` with an unknown key and a bad image → yamlls messages unchanged (manual).
- [ ] **Step 5:** `jj describe -m "feat(cli): gcd schemas"`, `jj new`, bookmark `go-schemas`.

## Unresolved questions

- None blocking. Each PR is based on the previous one's bookmark until merged.
