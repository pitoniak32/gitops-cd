# Layered override merge (app → stage → target)

App, stage and target settings merge with "most specific wins" (env merged per key), implemented explicitly per field in `platform/app.cue`. This deliberately departs from CUE's native unification, where conflicting values are errors, because teams expect a target to override a stage default; only `replicas`, `env` and `resources` are overridable, and `image` stays app-level so it promotes as one unit.
