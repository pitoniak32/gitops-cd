# Renderer in Go, no CUE

Status: accepted. Supersedes ADR 0001 (CUE behind YAML), ADR 0005 (editor schema) and ADR 0026 (CUE rules, Go tooling). ADR 0006 unchanged: teams write only YAML.

The renderer is plain Go: `platform/cli/internal/capability` reads the YAML, validates it and builds every file under `rendered/`. There is no CUE left in the repo.

Teams only ever wrote YAML, so CUE was purely the platform's implementation language, and most of the renderer worked against it:

- **Layered override** (target > stage > app > default): unification can't override, so every setting was a hand-written "first defined" ladder.
- **Checks on derived values**: a rule that reads the value it constrains is a cycle, hence a raw copy of the inputs and error side channels.
- **Located messages**: ~400 lines of Go parsed and filtered CUE's error text and mapped CUE paths back to YAML lines.
- **Editor schema vs validation**: the editor schema is stricter than validation in places; one set of definitions served both through a `generic` switch.
- **The Kargo graph** is an algorithm, written as comprehensions.

## What changed

- `capability`: `config.go` (platform-owned: cluster registry, replica policy, pre-deploy checks, the platform table), `load.go` (the one table of input file routes), `schema.go` + `validate.go` (per-file and cross-file rules, each a located `Diag`), `model.go` (layer merge, owners), `graph.go` (pipeline, Kargo graph), `kargo.go` / `output.go` / `platformfiles.go` (outputs).
- `emit`: ordered maps to YAML/JSON in the committed formatting.
- Output is byte-identical to the CUE renderer: same golden tree, and identical trees for the real capabilities and for mutated fixtures rendered by both.
- Lint runs in two rounds: the files as written, then (once clean) what only shows in derived values. Messages kept their wording where they were custom; CUE's own ("conflicting values", "incomplete value") became plain ones ("required", "field not allowed").
- No editor schemas: they were generated from the CUE definitions, and maintaining JSON Schema by hand next to the Go rules would be a second source of truth. `platform/render/schemas/`, the `$schema` header lines and `gcd schemas` are gone; `mise run lint` is the feedback loop.
- Platform config is Go, not a data file: a new region or pre-deploy check is a change to `config.go`.

## Considered Options

- **Stay on CUE.** Keeps one source for schema and validation, at the cost above.
- **Another config language (KCL, Pkl, Nickel, Jsonnet).** Each fixes the override problem; none fixes located YAML diagnostics or the editor/validation split, and the glue would remain.

## Consequences

- No `cue` tool or `cuelang.org/go` dependency; CI installs only Go.
- A new input field is a schema entry in Go (`schema.go` / `validate.go`).
- No completion or as-you-type validation in editors. If wanted later: generate JSON Schema from `schema.go`, or run `gcd lint` as an editor linter.
- Field order in the output is whatever the Go builders write; the golden test is the guard.
