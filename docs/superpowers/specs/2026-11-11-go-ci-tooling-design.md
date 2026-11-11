# Renderer + CI tooling in Go (`gcd`)

Date: 2026-11-11. Status: draft design, pre-plan.

## Goal

Replace the renderer/CI bash (`platform/render/scripts/*.sh`, `platform/appsets/check.sh`, the
render/lint/codeowners/schemas/test mise task bodies) with `gcd` subcommands and `go test`, using the
CUE Go library instead of the `cue`/`jq`/`yq` binaries. Less fragile (no awk over CUE's text output, no
yq per assertion), shared helpers, typed failures. `mise run <task>` stays the entry point with the same
args and output.

## Decisions

- Library: `cuelang.org/go` at v0.17.0 (= `.mise.toml` cue pin and `cue.mod` language version). Module
  deps (`cue.dev/x/k8s.io`) resolve through the library's registry client, same `CUE_CACHE_DIR` as the
  CLI (CI already caches `~/.cache/cue`).
- Home: `platform/cli` (`gcd`), new `internal/` packages; reuses `repo.Root`, `logx`.
- Out of scope: `tf-module-push` + `platform/tests/tf-module-push.sh` (demo task); `.github/workflows/*`
  deploy/terraform/cloud-run steps.
- `cue fmt --check ./platform/render/...` moves into `mise run test` (as a Go test using the library's
  formatter), and the separate CI step goes.
- Contract per step: golden output and `.github/CODEOWNERS` byte-identical; `mise run lint` output
  identical on every lint case and on `capabilities/` (a transitional test runs old `lint.sh` vs
  `gcd lint` over all lint cases and diffs, deleted with `lint.sh`); `schemas/` equal in content
  (`jq -S`), key order may change, editor behavior (yamlls messages) unchanged.
- New ADR 0026: renderer tooling is Go + CUE library; CUE stays the rule language (ADR 0006 unchanged:
  teams still only write YAML).

## Packages (`platform/cli/internal/`)

- `cueload`: the one place that builds a CUE value.
  - `Capability(root, cap string, opts) (cue.Value, error)`: loads package `render` from
    `platform/render`, adds every capability input (same globs as `inputs.sh`: `teams/*.y*ml`,
    `owners.y*ml`, `stages/*`, `targets/*.y*ml`, `targets/*/target.y*ml`,
    `targets/*/environment-variables/*`, `apps/*/app.y*ml`, `apps/*/owners.y*ml`) at
    `in."<path relative to root>"` with source positions kept, tags `capability`, `repo`
    (`GITOPS_REPO`).
  - `Platform(files ...string) (cue.Value, error)`: `clusters.cue regions.cue` (regions, `gcd regions`
    drops its `cue export` subprocess).
  - `Generic() (cue.Value, error)`: `generic=true` for schemas.
- `render`: `Capability(root, cap, out)` = vet (concrete, all errors) + write `files` (wipe the same
  per-capability paths as `render.sh`); `Regions(out)`; `UniqueApps(out)` (duplicate workload
  Application name per region, same message); `CodeOwners(root, out)` (template + `#GENERATED` + teams
  lines, same rules as `codeowners.sh`); `All(root, out)` = the `render` task.
- `yamlpos`: `Find(file, path []string) (line, col int, ok bool)` over `yaml.v3` nodes (map key position
  for keys, item position for list indexes); real line numbers, so the leading-comment offset hack goes.
- `yamledit` (test helper, shared by lint + render tests): `Set`, `Del`, `Rename` on a YAML file by key
  path, preserving comments.
- `lint`: `Capability(root, cap) ([]Diag, error)`; `Diag{File string; Line, Col int; Msg string}`,
  `String()` = `file:line:col: [label: ]msg`. From `errors.Errors(err)`: CUE path + message + first
  YAML position; same rules as `lint.sh`: drop errors nested under a reported path, drop "conflicting
  values" when a custom error exists at that path, `locate` table per top-level field (`apps`, `stages`,
  `targets`, `legacyTargets`, `targetEnvs`, `capOwners`, `legacyAppOwners`, `teams`,
  `stageNameErrors`, `stageDepth`, `deploys`), sorted + deduped across capabilities.
- `schemas`: `Generate(outDir)`: `#App #Stage #Target #EnvironmentVariables #Owners #Team` →
  JSON Schema, then the `schemas.sh` jq passes as Go functions over decoded JSON (`any`; key order not
  kept)
  (anyOf `false` drop, `$ref`+properties inline, `propertyNames` errorMessage, `not.pattern` fold,
  pattern messages, `patternErrorMessage`). Generator: library `encoding/jsonschema` if its output equals
  `cue def --out jsonschema`, else exec `cue def` (decided in the plan's first schemas task).
- `cmd`: `render [out]`, `codeowners`, `lint [caps…]` (exit 1 on diagnostics), `schemas [out]`.

## Tests

- `internal/render`: golden test over `platform/render/testdata/root` (payments, search) vs
  `testdata/golden`; `UPDATE=1` rewrites golden (same contract as today). The `test.sh` assertions become
  Go tests grouped by area (`kargo_test`, `argocd_test`, `gha_test`, `terraform_test`, `cloudrun_test`,
  `rbac_test`), over a small helper: parse rendered multi-doc YAML, `find(kind, name)`, field getters.
  Each assertion keeps its failure message.
- `internal/lint`: `lint-cases.tsv` becomes a Go table `{name, file, edit func(*yaml.Node), want
  regexp}`; fixture copy per case in `t.TempDir()`; edits via small `yamledit` helpers (`Set(path,
  value)`, `Del(path)`, `Rename(path, newKey)`) on `yaml.v3` nodes, written back; the dir-rename /
  clusters.cue / long-name cases join the same table with file-level edits. No yq at test time.
- `internal/appsets`: `check.sh` rules as Go tests over `platform/appsets/*.yaml` + `platform/bootstrap/root.yaml`.
- `internal/cuefmt`: every `.cue` under `platform/render` is formatter-stable.
- `mise run test` = gofmt + `go vet` + `go test ./...` + `tf-module-push.sh`.

## Migration (one PR each; scripts deleted in the PR that replaces them)

1. `cueload` + `render` + `gcd render`/`codeowners`; `regions` on `cueload`. Deletes `render.sh`,
   `inputs.sh` (after 2), `regions.sh`, `unique-apps.sh`, `codeowners.sh`. Golden byte-identical.
2. `yamlpos` + `lint` + `gcd lint`, with the transitional old-vs-new diff test. Deletes `lint.sh`,
   `inputs.sh`.
3. `test.sh` → Go tests + cue fmt test. Deletes `test.sh`, the CI `cue fmt` step.
4. `check.sh` → `internal/appsets` tests.
5. `schemas` + `gcd schemas`. Deletes `schemas.sh`. Last: CI `install_args` drops `cue jq` if nothing
   in `ci.yaml` uses them.
Docs each step: `CLAUDE.md` commands/layout, `docs/` script references, `GLOSSARY.md` if terms move.

## Risks

- Library `Validate` vs `cue vet -c -E` may report a different error set/order: the old-vs-new lint diff
  test catches it before `lint.sh` goes.
- Registry auth/network for `cue.dev` modules in CI: same cache dir as today; verify on the first PR.
- Schema key order changes once (Go maps): one-time diff in `schemas/`, content-equal.
