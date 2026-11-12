# Layered override merge (app → stage → target)

Settings in `app.yml` merge "most specific wins": `stages.<s>.targets.<t>` > `stages.<s>` > app top level > the target platform's defaults > the global defaults (`model.go`, `config.go`). `env` merges per key, and `workflow` merges per field. Teams expect a target to override a stage default, so a value set at a more specific layer is never a conflict.

- **Layered**: `replicas`, `env`, `envFrom` (ADR 0014), `resources`, `deployWith` and `workflow` (ADR 0020).
- **App-level only**: `image`/`module` and their constraints, `port` and `sourceRepo`. The artifact stays app-level so that it promotes as one unit.
