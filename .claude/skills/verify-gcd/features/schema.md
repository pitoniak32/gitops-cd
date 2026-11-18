# Schema

`gcd schema [kind]` prints an annotated YAML skeleton for each input file kind (owners, stage, app, target, env-vars), built from the schema lint enforces. It shows placeholders, allowed values, and required/pattern/description comments.

## Sub-features

- `schema-all`: every kind, each headed by `# <kind>: systems/{sys}/…`.
- `schema-kind`: a single kind, e.g. `target`.
- `schema-sources`: `-s` adds `(e.g. file:line)` fixture examples.
- `schema-json`: `--json` gives the machine-readable schema.

## How to get to it (user POV)

- `gcd schema app` before adding a field.

## Driving it with verify-gcd

Preconditions:

- Any launch. Schema doesn't read sandbox inputs, except that `-s` reads the fixture.

- **Kind.** Run `$V run schema-target schema target`. Result: exit 0. Output starts `# target: systems/{sys}/targets/*/target.yml`, then includes `environmentType: dev|npr|prd   # required; …` and `cluster: us-east4|dc1`.
- **Sources.** Run `$V run schema-app-src schema app -s`. Result: field comments include `(e.g. …:N)` references.
- **JSON.** Run `$V run schema-json schema --json`. Result: valid JSON. Check with `$V sh schema-json-ok 'gcd schema --json | python3 -m json.tool >/dev/null && echo valid'`.
- **Agrees with lint.** Each pattern shown (e.g. `replicas` 0 to 100) matches the message lint gives for a violation (lint.md `lint-schema`).

## Gotchas

- `cluster` values come from the cluster registry in `platform/cli/internal/system/config.go`, so a new region changes this output.
