# gcd verification map

This directory is the maintained source for verifying what app teams see from `gcd`. Read the index, then use the matching feature file as the recipe.

## Baseline preconditions

- `eval "$(.claude/skills/verify-gcd/verify-gcd launch)"` from the repo root. This creates a disposable sandbox copy of `systems/` and `teams/`, with the baseline committed at `@-`.
- `$V doctor` is all `ok`.
- The baseline source is real `systems/`, which has payments (my-app, infra, worker) and search (api, frontend). `launch --fixture` uses `platform/render/testdata/root` instead. That fixture is richer: its payments system adds web, flagged and payments apps.
- Never drive the real repo root as `--root` for journeys that write (render, codeowners).

## Driving conventions

- `$V run <label> <gcd args>` runs gcd. `$V edit <label> <file> '<sed>'` edits YAML. `$V sh <label> '<cmd>'` runs anything else.
- Diffs always use `--base @-` or `--against @-`.
- `$V reset` between journeys that need the baseline.
- Treat commands as literal.

## Proof and skip reporting

- CLI proof: the `.cmd`, `.stdout`, `.stderr` and `.exit` files under `.verify/<run>/evidence/`.
- Mutation proof: the `edit` diff plus a second read-only view (`show`, a `rendered/` file, `jj diff`).
- Report the feature ID and evidence path for each claim.
- An unreachable journey is reported with its command and the unmet precondition. It is never covered via another path.

## Feature entry contract

Each feature file has an H1 title and one paragraph. Then come four H2s, in order: `Sub-features`, `How to get to it (user POV)`, `Driving it with verify-gcd` (starts with `Preconditions:`), `Gotchas`.

## Features

- [Lint](./lint.md): validation of systems/teams YAML, three output formats, `--changed` scope.
- [Show merged config](./show.md): effective config per deploy with sources, JSON, rendered manifests.
- [Show diff](./show-diff.md): config diff, rendered diff, `--against` (the PR comment).
- [Render](./render.md): `rendered/` tree + CODEOWNERS, determinism, single-system render.
- [Schema](./schema.md): annotated YAML skeletons per file kind.
- [Live cluster (read-only)](./live-cluster.md): observing Kargo Stages / Argo CD apps on the user's kind clusters.
