# Renderer in plain Go

The renderer is plain Go: `platform/cli/internal/system` reads the team YAML (ADR 0006), validates it into located diagnostics, and builds every file under `rendered/`. It was CUE first (typed schemas, cross-file validation and one source for validation and generation), with generated editor JSON Schemas and later Go tooling around the CUE rules. Teams only ever wrote YAML, so CUE was purely an implementation language, and most of the renderer worked against it:

- **Layered override** (ADR 0004): unification can't override, so every setting was a hand-written "first defined" ladder.
- **Checks on derived values**: a rule that reads the value it constrains is a cycle in CUE.
- **Located messages**: ~400 lines of Go parsed CUE's error text and mapped CUE paths back to YAML lines.
- **The Kargo graph** (ADR 0012) is an algorithm, which CUE forced into comprehensions.

Now:

- `config.go`: platform-owned config (cluster registry, replica floors, pre-deploy checks, the `platforms` table). `load.go`: the one table of input file routes. `schema.go` + `validate.go`: per-file and cross-file rules, each a located `Diag`. `model.go`: the layer merge and owners. `graph.go`: the Kargo graph. `kargo.go` / `output.go` / `platformfiles.go`: outputs. `internal/emit` writes ordered YAML/JSON.
- Lint runs in two rounds: the files as written, then, once those are clean, what only shows in derived values.
- **No editor schemas**: hand-maintained JSON Schema next to the Go rules would be a second source of truth. `gcd lint` is the feedback loop.

## Considered Options

- **Stay on CUE**: one source for schema and validation, at the cost above.
- **Another config language (KCL, Pkl, Nickel, Jsonnet)**: each fixes the override problem, but none fixes located YAML diagnostics, and the glue would remain.

## Consequences

- CI installs only Go.
- A new input field is a schema entry in `schema.go` or `validate.go`, read in `model.go`. A new region or pre-deploy check is a change to `config.go`.
- Editors get no completion or as-you-type validation. If that's wanted later, generate JSON Schema from `schema.go`, or run `gcd lint` as an editor linter.
- Output field order is whatever the Go builders write, and the golden test guards it.
