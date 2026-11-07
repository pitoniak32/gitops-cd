# One monorepo for teams, platform and output

All teams' YAML, the platform CUE and the rendered output live in one repo, replacing a repo per team plus a platform repo and a rendered repo. This removes platform version pinning, schema vendoring and cross-repo pushes, and a platform change shows its rendered impact on every team in one PR.

## Consequences

Every team gets platform changes at once; per-team rollout control comes from Kargo promotion, not from pinning. The CI render bot needs permission to push to the protected `main` branch.
