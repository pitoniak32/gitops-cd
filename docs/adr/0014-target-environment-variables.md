# Target environment-variables files, opted in with envFrom

Some env vars belong to a target, not to an app. For example, a LaunchDarkly SDK key and relay URL differ per region. Repeating them in every app per target drifts.

- A target is a directory: `targets/<t>/target.yml` plus optional `targets/<t>/environment-variables/<name>.yml` files, each a flat `KEY: value` map with the same key and `secret:` rules as `env`.
- Apps opt in by name: `envFrom: [<name>]` at app, stage or target level in `app.yml` (ADR 0004).
- Precedence: target `env` > stage `env` > app `env` > files from target `envFrom` > stage `envFrom` > app `envFrom`.
- Lint errors: an `envFrom` name missing on any target it reaches; two files at the same layer setting one KEY to different values; a target dir without `target.yml`; a leftover `targets/<t>.yml`.

## Considered Options

- **App-level `envFrom` only**: forces every target of the app to have the file.
- **Skip missing files silently**: a typo would ship without the vars.
- **An `env:` wrapper in the file**: the directory name already says what it is.
