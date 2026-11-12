# Generic editor schema; full rules only in CI

Status: superseded by ADR 0030 (no editor schema; lint is the only validation).

Editors get a generic JSON Schema (`platform/render/schemas/`, from `cue def -t generic=true`) referenced by each file's `$schema` line; the rules that depend on other files (unknown stage/target names, cycles, cluster/environment match, prod minimums) run only in CI lint, as PR annotations. This keeps editing working in any editor on Mac and Windows with nothing installed and no regeneration when teams add stages or targets.

## Considered Options

Per-team schemas with name enums (needed regenerating plus editor restarts); an nvim-only lint integration (editor-specific, bash/yq on the machine). A cross-platform Go CLI on CUE's API remains the option if local full checks are needed.
