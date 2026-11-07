# 0014: Target environment-variables files, opted in with envFrom

## Status
Accepted

## Context
Some env vars belong to a target, not an app: e.g. the LaunchDarkly SDK key and relay URL differ per region/cluster. Repeating them in every app's `app.yml` per target drifts.

## Decision
- A target is a directory: `targets/<t>/target.yml` (config, unchanged format) plus optional `targets/<t>/environment-variables/<name>.yml`, a flat `KEY: value` map (same key and `secret:` rules as `env`).
- Apps opt in by name: `envFrom: [<name>]` at app, stage or target level (in `app.yml` only, not `stages/*.yml`).
- Precedence: target `env` > stage `env` > app `env` > files from target `envFrom` > stage `envFrom` > app `envFrom`.
- Two files listed at the same layer setting one KEY: equal values are fine, different values are a lint error.
- Lint errors: an `envFrom` name missing on any target it reaches; a same-layer KEY clash; a bundle dir without `target.yml`; a leftover `targets/<t>.yml`.

## Considered
- App-level `envFrom` only: simpler, but forces every target of the app to have the file.
- Skip missing files silently: typos would ship without the vars.
- `env:` wrapper in the file: room for more fields, but the directory name already says what it is.
- Keep `targets/<t>.yml` with a sibling `targets/<t>/` dir: two places per target.

## Consequences
Target files moved (`targets/<t>.yml` → `targets/<t>/target.yml`); `$schema` paths are one level deeper. New editor schema `environment-variables.json`.
