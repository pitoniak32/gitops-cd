# Agent-forward system config

## Goal
Claude (Claude Code, local checkout, `gcd` on PATH) makes system config changes correctly on first try with few tokens. App teams primary, platform secondary. Output: a jj change a human reviews in a PR.

Agents edit YAML directly (Edit tool). `gcd` supplies the knowledge (schema, effective config, render effect); `gcd lint` is the fix loop. No mutation/scaffold commands, no MCP server, no new validation rules.

## Problems addressed
1. Valid fields/enums/patterns live only in `system/schema.go` + `validate.go` → agent reads Go source or guesses.
2. Settings layer (app → stage → target, plus platform defaults); agent can't see the effective value or which file set it → wrong-layer edits.
3. Effect of a change on `rendered/` is invisible without a manual render + diff.
4. No compact, authoritative example of each feature.

## Components

### 1. `gcd schema [kind] [--json]`
- Kinds: `owners`, `stage`, `app`, `target`, `env-vars` (names = `routes` kinds in `load.go`). No kind → all.
- Emitted from the `schema` structs in `schema.go`; no second source.
- New `desc string` on `field` (one line, required for every field; enforced by a test).
- Text output, one line per field, indented by nesting:
  `name  type  [required]  enum|pattern  — desc  (e.g. <fixture file>:<line>)`
- Example locations come from the fixture walk (component 6); omitted when none.
- `--json`: same tree as JSON (`name, type, required, enum, pattern, desc, example, fields|elem`).
- Validation rules that live outside `schema` (cross-file in `validate.go`/`model.go`) not listed here; they surface via lint.

### 2. `gcd show <sys>[/<app>] [--stage s] [--target t] [--root dir] [--json]`
- Loads + merges via the existing model; prints one block per deploy (app × target), filtered by flags.
- Each effective leaf (`replicas`, `port`, `env.*`, `envFrom`, `resources.*`, `deployWith`, `workflow.*`, image/module, target config) annotated with its source: `← systems/payments/apps/my-app/app.yml:21` or `← platform default` / `← global default`.
- Requires provenance in the merge: `layer` already holds its `*yaml.Node`; record the source `file`+node per resolved value on `deploy` (e.g. parallel `src map[string]srcRef`). Render path ignores it; no output change (golden unchanged).
- Owners: per app an `owners:` block (effective `codeOwners`, `promoters.<stage>`, `viewers`, `breakGlass`), each tagged `← owners.yml:<line>` (system-wide) or the `apps.<app>` override line. Shown when no `--stage`/`--target` filter, or the filtered stage's promoters only.
- On load/validate errors: print diags (same as lint) and exit non-zero.
- `--json`: ordered arrays: `{deploys: [{app, stage, target, platform, values: [{path, value, file, line, note}]}], owners: {<app>: [{path, value, file, line, note}]}}` (`file`/`line`/`note` omitted when empty).
- A `--stage`/`--target` that matches no deploy of the selected app is an error listing the app's `<stage>/<target>` deploys.

### 3. `gcd lint --format json`
- New value for existing `--format` (`auto|text|github|json`): array of `{file, line, col, label, msg}`.

### 4. `gcd diff [--base <rev>] [--stat] [--system c…]`
- Renders base revision and working copy into temp dirs, prints unified diff of `rendered/` (or `--stat`: changed files + line counts).
- `--base` takes any revset; must resolve to exactly one commit, else error naming the count. Default `@-`; skill recommends `trunk()` to see a whole PR stack.
- Renderer inputs are only `systems/` + `teams/` (platform config is compiled into `gcd`), so base root = temp dir filled via `jj file list -r <base>` + `jj file show -r <base>` for those paths. Read-only jj; no workspace created. Helpers in `repo` package.
- Caveat: diff uses current `gcd` for both sides, so it shows input changes only, not renderer changes.
- Scope: systems with a changed path under `systems/` or `teams/` between the exported base and the working tree (a `teams/` change selects all; deleted caps included; not `lint.Changed`, which drops deleted caps), unless `--system` given (unknown name = error).
- `--root` is a repo root and must be a jj repo.
- Platform files + CODEOWNERS included when in scope.

### 5. Skill `.claude/skills/system-config/SKILL.md`
Committed to the repo (`.claude/` not gitignored) so anyone cloning gets it.

~60 lines, written per `writing-for-agents`. Content:
- Trigger: any change under `systems/` or `teams/`.
- Workflow: `gcd show` (find layer/source) → `gcd schema <kind>` (allowed fields) → edit YAML → `gcd lint <sys>` until clean → `gcd diff --stat` then targeted `gcd diff` → if `owners.yml`/`teams/` changed, `mise run codeowners`.
- Layering rule: change at the narrowest layer that matches the request's scope; `gcd show` source tells where existing value lives.
- Pointers: target naming (ADR 0025), owners model (ADR 0007), fixture as example source.
- What needs platform approval (owners.yml, teams/) so agent can say so in the PR.
- Never edit `rendered/`, `.github/CODEOWNERS`.
- CLAUDE.md: one line pointing to the skill.

### 6. Reference fixture + coverage
- `platform/render/testdata/root/systems/payments` (+ `search`) is the reference system. Add brief `#` comments explaining non-obvious features (comments don't affect render/golden).
- Coverage test: walk the `schema` tree, collect field paths per kind; walk fixture files; fail listing any schema path with no occurrence. Fixtures extended to cover gaps found.
- Same walk produces the `example` file:line per field for `gcd schema` (computed at runtime from the fixture dir under the repo root; skipped if absent).
- Agents can sandbox with `--root platform/render/testdata/root` on `show`/`lint`/`render`.

## Testing
- `schema`: golden text + JSON for all kinds; test fails if any `field` has empty `desc`.
- `show`: golden text against fixture for a few selectors (cap, sys/app, `--stage`, `--target`); assert source lines point at the correct layer for overridden values (e.g. `my-app` `dc1-dev-k8s` `replicas: 3` → target layer line); owners override from `apps.<app>` vs system-wide.
- `diff --base`: revset resolving to 0 or >1 commits → error.
- `lint --format json`: decode + compare to text diags.
- `diff`: two fixture roots (base, modified via `yamledit`) → expected changed files.
- Coverage test as above.
- Golden render output unchanged (provenance is side data).
- All in `mise run test`; add any new read paths to `cli.yaml` `paths`.

## Out of scope
Mutation/scaffold commands (`gcd set`, `gcd new`), MCP server, CI-triggered agents, new validation rules.
