# Renderer tooling in Go

Status: superseded by ADR 0030 (the rules moved to Go too). Extends ADR 0018 (Go CLI) to the renderer tooling; ADR 0006 unchanged (teams never write CUE or code).

Render, lint, schemas and CI checks were bash over the `cue` CLI: awk over CUE text output, `yq` per assertion, helpers copied between scripts, `jq` splitting exported JSON.

Now they live in `gcd` (`platform/cli`) on `cuelang.org/go` v0.17.0, pinned to match the `cue` CLI in `.mise.toml`. CUE stays the rule language: all validation and output logic remains in `platform/render/*.cue`; Go only loads packages, fills the YAML inputs, decodes `files`, and writes them.

- `internal/cueload`: `load.Instances` + `yaml.Extract` + `FillPath` into `in."<path>"` (same routing as before).
- `internal/render`: `gcd render`, `gcd codeowners`, `gcd regions`; byte-identical to the old scripts (golden + CODEOWNERS golden).
- `internal/yamlpos` + `internal/lint`: `gcd lint`, `<file>:<line>:<col>: <message>` from CUE error values; errors collected per capability, YAML syntax errors become diagnostics.
- `internal/schemas`: `gcd schemas` (editor schemas).
- `internal/yamledit` (test helper), `internal/appsets` + `internal/cuefmt` (tests only): lint cases, AppSet static checks, `cue fmt` check.
- `mise run test` = gofmt + `go vet` + `go test` + `tf-module-push.sh`. CI installs only go.

## Considered Options

- **Keep bash.** Works, but every check re-parses text and shares nothing; no unit tests.
- **Rewrite rules in Go.** Loses the schema/validation single source and editor schemas; rejected.

## Consequences

- `cue` CLI pin and `cuelang.org/go` pin move together.
- Errors keep CUE positions (`file:line:col`), now from the library.
- Adding a check means Go code plus a test, not a bash edit.
