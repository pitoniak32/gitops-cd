# Agent-forward capability config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Claude `gcd schema`, `gcd show`, `gcd lint --format json`, `gcd diff` plus a repo skill so capability YAML edits are right first try and cheap.

**Architecture:** All knowledge comes from the existing renderer package `capability`: schemas hoisted into describable values, provenance recorded during the existing merge, rendering reused for diff. New cobra commands in `internal/cmd` only format. Skill in `.claude/skills/capability-config/`.

**Tech Stack:** Go 1.27, cobra, gopkg.in/yaml.v3, `github.com/pmezard/go-difflib` (already indirect in go.mod; promote to direct), jj CLI (read-only).

**Spec:** `docs/superpowers/specs/2026-11-12-agent-capability-config-design.md`

## Global Constraints
- All work in `platform/cli` unless stated. Run tests from `platform/cli`: `go test ./...`. Full gate: `mise run test-cli` (gofmt + vet + test).
- Existing render golden (`platform/render/testdata/golden`) and lint test cases must stay byte-identical / green: provenance and schema hoisting change no output or Diag text.
- New CLI goldens live in `platform/render/testdata/golden-cli/`; regenerate with `UPDATE=1` like `capability/golden_test.go`.
- Fixture root = `platform/render/testdata/root` (repo-relative). "Example" locations are repo-relative `file:line`.
- Schema field paths: map field = name, dict key = `*`, list item = `[]`, joined with `.` (e.g. app `stages.*.targets.*.env.*`, owners `promoters.*.[]`).
- Source refs: repo-relative `file:line`, or exactly `platform default (<PLATFORM>)` / `global default`.
- jj only, read-only (`jj log`, `jj file list`, `jj file show`). No git mutations. Commit per task with `jj commit -m "<msg>"` + trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Any new test reading a path outside `platform/cli` → add it to `.github/workflows/cli.yaml` `paths`.

## Review Focus
1. `gcd show` on a capability with lint errors → prints the diags (text format), exit non-zero, no partial deploy output. (Task 5)
2. `gcd show payments/nope` or `--stage nope` / `--target nope` → error naming the allowed apps/stages/targets, not empty output. (Task 5)
3. `gcd diff` when a capability exists only on one side (new or deleted) → every file shows as added/removed, no error. (Task 8)
4. Env key set both by an `envFrom` bundle and a layer `env` → `show` source is the winning layer's `env` line, not the bundle. (Task 3)
5. `gcd schema` when the fixture dir is absent (e.g. `--root` elsewhere) → still prints the schema, just no `e.g.` parts. (Task 2)

---

### Task 1: Describable schemas + required `desc`

Hoist the inline/dynamic schemas so one definition serves both validation and docs; add `desc` to every field.

**Files:**
- Modify: `internal/capability/schema.go` (field struct, add `desc`, `platforms`; new constructors; `Schemas`)
- Modify: `internal/capability/validate.go:69-80` (stage), `:83-200` (target), `:290+` (owners) to use constructors
- Create: `internal/capability/describe.go` (`FieldDoc`, `SchemaDoc`, `Schemas`)
- Test: `internal/capability/describe_test.go`

**Interfaces:**
- Produces:
  - `field` gains `desc string` and `platforms []string` (nil = every platform).
  - `func stageSchema(stages []string) *schema`
  - `func targetSchema(cfg Config) *schema` — `config` map whose fields = common + per-platform fields (`platforms` from the `platforms` table `fields`); used by `validator.target` for lookups (keep its custom loop, messages unchanged).
  - `func ownersSchema(teams, stages []string) *schema`
  - `type FieldDoc struct { Name, Type string; Required bool; Enum []string; Pattern, Desc string; Platforms []string; Example string; Fields []FieldDoc; Elem *FieldDoc }` (json tags lower-camel, omitempty)
  - `type SchemaDoc struct { Kind, File, Desc string; Fields []FieldDoc }` — Kind ∈ `owners, stage, app, target, env-vars`; File = route pattern from `routes` (e.g. `capabilities/{cap}/apps/*/app.yml`).
  - `func Schemas(cfg Config) []SchemaDoc` — ordered as Kind list above. Capability-dependent enums (stages, teams) are empty; their `Desc` says what they reference (e.g. "a stage of this capability").
  - `func FieldPaths(d SchemaDoc) []string` — every path per Global Constraints, depth-first, field order.

- [ ] **Step 1: Write failing tests** in `describe_test.go`:
  - `TestSchemasKinds`: `Schemas(Default())` kinds == `[owners stage app target env-vars]`.
  - `TestEveryFieldHasDesc`: walk all `FieldDoc` (incl. Elem) → every named field `Desc != ""`; failure lists `kind:path`.
  - `TestTargetPlatforms`: `config.cluster` `Platforms == ["K8S"]`; `config.gcpProject` and `config.region` `["CLOUD_RUN","TERRAFORM"]` (sorted); `config.backend` `["TERRAFORM"]`; `config.environment` nil.
  - `TestFieldPaths`: app paths contain `stages.*.targets.*.env.*` and `replicas`; owners contain `promoters.*.[]`.
- [ ] **Step 2: Run** `go test ./internal/capability -run 'Schemas|Desc|Platforms|FieldPaths'` → FAIL (undefined).
- [ ] **Step 3: Implement** constructors + `desc` on every field (one line, user-facing, no trailing period) + `describe.go`. Move existing `//` field comments into `desc` where they exist. Validation call sites switch to constructors.
- [ ] **Step 4: Run** `go test ./...` → PASS, render golden + lint tests unchanged.
- [ ] **Step 5: Commit** `feat(capability): describable schemas with field descriptions`

### Task 2: Fixture coverage + examples + `gcd schema`

**Files:**
- Create: `internal/capability/examples.go`
- Create: `internal/cmd/schema.go`, register in `internal/cmd/root.go`
- Modify: fixture YAML under `platform/render/testdata/root/capabilities/` (add `#` comments; add fields that coverage reports missing)
- Test: `internal/capability/examples_test.go`, `internal/cmd/schema_test.go`; goldens `platform/render/testdata/golden-cli/schema.txt`, `schema.json`
- Modify: `.github/workflows/cli.yaml` `paths` if `golden-cli/` not covered by existing `platform/render/**`

**Interfaces:**
- Consumes: `Schemas`, `FieldPaths`, `SchemaDoc`, `FieldDoc` (Task 1); `routes` for file→kind.
- Produces:
  - `const FixtureRoot = "platform/render/testdata/root"`
  - `func Examples(root string) (map[string]string, error)` — key `kind/path`, value repo-relative `file:line` (prefixed with `FixtureRoot/`) of first occurrence, walking fixture files in `routes` order then path order. Missing dir → empty map, nil error.
  - `func WithExamples(docs []SchemaDoc, ex map[string]string) []SchemaDoc` — fills `Example`.
  - `gcd schema [kind] [--json] [--root dir]`; unknown kind → error listing kinds.

- [ ] **Step 1: Failing tests**:
  - `TestFixtureCoversSchema`: for each kind, every `FieldPaths` entry has a key in `Examples(<repo>/FixtureRoot)`; failure lists missing `kind/path`.
  - `TestExamplesMissingDir`: `Examples(t.TempDir())` → empty, nil.
  - `TestSchemaGolden` (cmd): `gcd schema` text and `gcd schema --json` match goldens; `gcd schema app` output starts with `app  capabilities/{cap}/apps/*/app.yml`.
  - `TestSchemaUnknownKind`: `gcd schema nope` → error containing `owners, stage, app, target, env-vars`.
  - `TestSchemaNoFixture` (Review Focus 5): `gcd schema --root <tmp>` succeeds, output has no `e.g.`.
- [ ] **Step 2: Run** `go test ./internal/capability ./internal/cmd -run 'Fixture|Examples|Schema'` → FAIL.
- [ ] **Step 3: Implement.** Text format per field line, 2-space indent per depth:
  `<name>  <type>[  required][  one of a|b|c | /pattern/][  platforms K8S,…]  — <desc>[  (e.g. <file:line>)]`
  Kind header: `<kind>  <file>  — <desc>`. Blank line between kinds.
- [ ] **Step 4: Extend fixture** until `TestFixtureCoversSchema` passes; add short `#` comments on non-obvious features (layer overrides, envFrom, perf qualifier, TF/CR targets). Run `go test ./...` → render golden unchanged (comments only + new fields may change golden: if new fields change output, accept with `UPDATE=1` and review diff).
- [ ] **Step 5: Run** `UPDATE=1 go test ./internal/cmd -run SchemaGolden` then `go test ./...` → PASS.
- [ ] **Step 6: Commit** `feat(gcd): schema command with fixture examples`

### Task 3: Provenance in the merge

**Files:**
- Modify: `internal/capability/model.go` (`kv`, `layer`, `readLayer`, `deploy`, `merge`), `load.go`/wherever `targetEnvs` is filled
- Test: `internal/capability/provenance_test.go`

**Interfaces:**
- Produces:
  - `type Source struct { File string; Line int; Note string }` with `func (s Source) String() string` → `file:line` or `Note`.
  - `kv` gains `src Source`; `layer` gains `file string` (set where layers are read; platform defaults → Note `platform default (<NAME>)`, `defaults` → `global default`).
  - `deploy` gains `src map[string]Source` keyed by leaf path: `artifact` (image|module), `port`, `replicas`, `resources.cpu`, `resources.memory`, `deployWith`, `workflow.repo|file|ref`, `envFrom`, `env.<KEY>`. Env sources come from the winning `kv.src` (bundle file line, or layer env line). Forced `deployWith` → Note `forced by <artifact|platform>`.
  - `func (m *model) deployFor(app, target string) *deploy` reuse existing `m.deploy`.

- [ ] **Step 1: Failing tests** (load fixture via `loadModel(Default(), <FixtureRoot>, "payments", nil)`):
  - `TestSrcTargetOverride`: deploy `my-app/dc1-dev-k8s` `src["replicas"]` == `…/apps/my-app/app.yml:<line of replicas: 3 under dc1-dev-k8s>`; `src["env.APP_ENV"]` → same file, line of `APP_ENV: 'false'`.
  - `TestSrcStageLayer`: `my-app/us-east4-dev-k8s` `src["replicas"]` → stage-level `replicas: 1` line.
  - `TestSrcDefaults`: a deploy with no `resources` → `src["resources.cpu"].String()` is `platform default (K8S)` or `global default` (whichever the code resolves).
  - `TestSrcEnvFromBundle`: `my-app/us-east4-dev-k8s` key from `launchdarkly.yml` → src file `…/targets/us-east4-dev-k8s/environment-variables/launchdarkly.yml`.
  - `TestSrcEnvBeatsBundle` (Review Focus 4): add to fixture a key present in both the bundle and the app's env; src → app.yml line.
- [ ] **Step 2: Run** `go test ./internal/capability -run Src` → FAIL.
- [ ] **Step 3: Implement.** `first` returns the winning layer too; record `src` there. Lines from `getKV(l.node, …)` key node.
- [ ] **Step 4: Run** `go test ./...` → PASS, golden unchanged.
- [ ] **Step 5: Commit** `feat(capability): record value provenance during merge`

### Task 4: Owners provenance

**Files:**
- Modify: `internal/capability/model.go` (`owners`, `readOwners`)
- Test: `internal/capability/provenance_test.go`

**Interfaces:**
- Produces: `owners.src map[string]map[string]Source` — app → path (`codeOwners`, `viewers`, `breakGlass`, `promoters.<stage>`) → line of the list used (app override key line or capability-wide key line in `owners.yml`). Add `viewers map[string][]string` (per app effective) if not derivable.

- [ ] **Step 1: Failing tests**: in a temp copy of fixture (use `yamledit` to add `apps: {my-app: {promoters: {prod: [x@example.com]}}}` to payments `owners.yml`): `src["my-app"]["promoters.prod"]` → override line; `src["worker"]["promoters.prod"]` → capability-wide line; `src["my-app"]["breakGlass"]` → capability-wide line.
- [ ] **Step 2: Run** `go test ./internal/capability -run Owners` → FAIL.
- [ ] **Step 3: Implement** in `readOwners` alongside existing lookups.
- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(capability): owners provenance`

### Task 5: `capability.Show` + `gcd show`

**Files:**
- Create: `internal/capability/show.go`, `internal/cmd/show.go` (register in `root.go`)
- Test: `internal/capability/show_test.go`, `internal/cmd/show_test.go`; goldens `golden-cli/show-payments.txt`, `show-my-app-prod.txt`, `show-target.txt`, `show-my-app.json`

**Interfaces:**
- Consumes: `deploy.src`, `owners.src`, `Source` (Tasks 3–4); `loadModel`.
- Produces:
  - `type ShowFilter struct { App, Stage, Target string }`
  - `type Value struct { Path string; Value any; Source Source }` (json: `path,value,file,line,note`)
  - `type ShowDeploy struct { App, Stage, Target, Platform string; Values []Value }`
  - `type ShowResult struct { Deploys []ShowDeploy; Owners map[string][]Value }`
  - `func Show(cfg Config, root, capName string, teams *Teams, f ShowFilter) (ShowResult, error)` — diags → `Error`; unknown app/stage/target → error `unknown <what> <x>; allowed: a, b`. Owners included for each shown app; with `Stage` set, only `promoters.<stage>` + codeOwners/viewers/breakGlass.
  - `func (r ShowResult) Text() string`:
    ```
    my-app  dev  dc1-dev-k8s  K8S
      replicas: 3                     ← capabilities/payments/apps/my-app/app.yml:22
      env.APP_ENV: false              ← capabilities/payments/apps/my-app/app.yml:20
    owners my-app
      promoters.dev: [doug@example.com]  ← capabilities/payments/owners.yml:3
    ```
    Values padded to align `←` within a block. Target config (`config.*`) values included, sourced from target.yml.
  - `gcd show <cap>[/<app>] [--stage s] [--target t] [--root dir] [--json]`.

- [ ] **Step 1: Failing tests**: goldens for `payments`, `payments/my-app --stage prod`, `payments --target dc1-dev-k8s`, `--json payments/my-app`; `TestShowLintErrors` (Review Focus 1: temp fixture with bad `replicas: x` → error text contains `app.yml:` diag, stdout empty, non-zero); `TestShowUnknown` (Review Focus 2: `payments/nope`, `--stage nope`, `--target nope` → error lists allowed names).
- [ ] **Step 2: Run** `go test ./internal/... -run Show` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `UPDATE=1 go test ./internal/cmd -run Show`, inspect goldens (sources point at right layers), then `go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(gcd): show effective config with sources`

### Task 6: `gcd lint --format json`

**Files:**
- Modify: `internal/cmd/lint.go:56-63,137`; `internal/capability/diag.go` (json tags on `Diag`: `file,line,col,label,msg`)
- Test: `internal/cmd/lint_test.go`

- [ ] **Step 1: Failing test** `TestLintJSON`: temp fixture with one bad field → `gcd lint --format json --root <tmp>` stdout decodes to `[]Diag` equal to text-mode diags; exit non-zero; clean fixture → `[]` exit 0.
- [ ] **Step 2: Run** → FAIL (`--format: want auto, text or github`).
- [ ] **Step 3: Implement**: `json` case; error message `want auto, text, github or json`; flag help updated.
- [ ] **Step 4: Run** `go test ./internal/cmd -run Lint` → PASS.
- [ ] **Step 5: Commit** `feat(lint): json format`

### Task 7: jj base export

**Files:**
- Create: `internal/repo/jj.go`
- Test: `internal/repo/jj_test.go` (skip if `jj` not on PATH)

**Interfaces:**
- Produces:
  - `func ResolveOne(root, revset string) (commitID string, err error)` — `jj log -r <revset> --no-graph -T 'commit_id ++ "\n"'`; ≠1 line → error `revset <r> resolves to <n> commits; want exactly 1`.
  - `func Export(root, commitID, dst string, prefixes ...string) error` — `jj file list -r <id> <prefix>…` then `jj file show -r <id> <path>` each into `dst/<path>`. Read-only.

- [ ] **Step 1: Failing tests**: in a temp jj repo (`jj git init`, write files, `jj commit`): `ResolveOne(@-)` ok; `ResolveOne("all()")` → error containing `want exactly 1`; `ResolveOne("none()")` → `resolves to 0`; `Export` writes only `capabilities/` + `teams/` files with base content (modify working copy after commit; exported content is the old one).
- [ ] **Step 2: Run** `go test ./internal/repo -run 'Resolve|Export'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(repo): read-only jj revision export`

### Task 8: `gcd diff`

**Files:**
- Create: `internal/render/diff.go`, `internal/cmd/diff.go` (register)
- Modify: `go.mod` (`github.com/pmezard/go-difflib` direct)
- Test: `internal/render/diff_test.go`, `internal/cmd/diff_test.go`

**Interfaces:**
- Consumes: `repo.ResolveOne`, `repo.Export` (Task 7); `capability.Render`, `capability.LoadTeams`, `capability.Names`; `lint.Changed` for scope.
- Produces:
  - `type FileDiff struct { Path string; Old, New string }` (Old "" = added, New "" = removed)
  - `func Diff(cfg capability.Config, baseRoot, root string, caps []string) ([]FileDiff, error)` — per cap: `Render` both sides (missing cap dir on a side → empty map), keep changed paths, sorted. Render error on working side → return it (diags); on base side → error prefixed `base:`.
  - `func Unified(d []FileDiff) string` (difflib, 3 context lines, `a/rendered/<path>` `b/rendered/<path>`), `func Stat(d []FileDiff) string` (`<path> | +a -d`, total line).
  - `gcd diff [--base revset] [--stat] [--capability c…]`; default base `@-`. Scope: `--capability` else `lint.Changed(root, changed files)` where changed = files differing base vs working (from exported trees); `teams/` change → all caps. Temp dir removed after.

- [ ] **Step 1: Failing tests** (render pkg, two fixture copies, no jj): modify `my-app` replicas → exactly the expected rendered paths changed; `TestDiffNewCapability` and `TestDiffDeletedCapability` (Review Focus 3) → all files added/removed, no error; `Stat` format golden. Cmd test (skip without jj): temp jj repo with fixture, edit, `gcd diff --stat` lists the file.
- [ ] **Step 2: Run** `go test ./internal/render ./internal/cmd -run Diff` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go mod tidy && go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(gcd): diff rendered output against a base revision`

### Task 9: Skill + docs

**Files:**
- Create: `.claude/skills/capability-config/SKILL.md` (repo root; committed)
- Modify: `CLAUDE.md` (Commands: `gcd schema|show|diff`, `lint --format json`; Agent skills: pointer), `README.md` if it lists gcd commands, `GLOSSARY.md` if "provenance/source" needs a term (only if used elsewhere)

- [ ] **Step 1: Load** `mattpocock-skills:writing-for-agents`; write SKILL.md (≤ ~60 lines): frontmatter `name: capability-config`, description triggers on changing `capabilities/` or `teams/` YAML. Body: workflow `gcd show` → `gcd schema <kind>` → edit → `gcd lint <cap>` until clean → `gcd diff --stat` (`--base 'trunk()'` for whole PR) → `mise run codeowners` if owners/teams changed; narrowest-layer rule; never edit `rendered/` or `.github/CODEOWNERS`; owners/teams need platform approval (say so in PR); target naming ADR 0025, owners ADR 0007; examples via `gcd schema` `e.g.` or `--root platform/render/testdata/root` sandbox.
- [ ] **Step 2: Verify** each command in the skill runs from repo root: `mise run cli && gcd schema app && gcd show payments/my-app --stage prod && gcd lint payments --format json && gcd diff --stat`. All exit 0 (diff may print nothing).
- [ ] **Step 3: Run** `mise run test-cli` → PASS.
- [ ] **Step 4: Commit** `docs: capability-config agent skill`
