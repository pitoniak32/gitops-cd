# Teams author YAML, not CUE (for now)

We probed letting teams write their config in CUE instead of YAML and kept YAML. Fencing CUE was workable: team CUE exported on its own to JSON and fed to the platform gave byte-identical renders, a pattern constraint in a platform `#Team` rejected stray top-level fields, and an import allowlist (platform `schema`/`util` plus a few stdlib packages) blocked cross-team sharing. The blocker was the editor: `cue lsp` (v0.17, which the VS Code extension uses) gives only syntax diagnostics — no schema-driven completion, no hover, no evaluation errors — so teams would lose what the JSON Schema gives them today. Also, `error("…allowed…")` messages report the position in the schema, not the team's file, while plain conflict errors keep the position but are cryptic.

## Consequences

Revisit when `cue lsp` reports evaluation errors and completes fields from definitions. The probe's design carries over: a platform `#Team` that teams embed, with team CUE exported to JSON in isolation before render and a `util` package as the only shared-code path.
