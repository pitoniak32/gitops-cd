# One monorepo for teams and platform

All teams' YAML, the renderer, the cluster setup and the add-on config live in one repo. This replaces a repo per team plus a platform repo and a rendered repo. It removes platform version pinning, schema vendoring and cross-repo pushes, and a platform change shows its rendered impact on every team in one PR. Generated output lives on this repo's `rendered` branch (ADR 0002).

## Consequences

Every team gets platform changes at once. Per-team rollout control comes from Kargo promotion, not from pinning.
