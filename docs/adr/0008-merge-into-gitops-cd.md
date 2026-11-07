# Merge the CUE gitops POC into the local Kargo demo (gitops-cd)

The CUE renderer, capability/team YAML and generated output now live in the same repo as the local kind + Argo CD + Kargo platform, so the whole flow (YAML → render → Freight → promotion → sync) runs on a laptop. We use idiomatic names. CUE has one module at the repo root (`cue.mod/`), with the renderer as package `render/`. Argo CD cluster add-ons live in `infrastructure/`, root Applications in `bootstrap/`, and generated config in `rendered/{argocd,kargo}`. This frees "platform" to mean only the platform team.

## Consequences

- The cluster registry is a single `in-cluster` (kind) entry accepting every environment. Targets differ by namespace (`<cap>-<target>`).
- kind has no secret store (`secretStore: false`): `secret:` env values are validated but neither ExternalSecrets nor secret env refs are rendered. External Secrets Operator isn't installed.
- Promote rights come from Keycloak groups through the generated `owner-<team>` Kargo roles. The example project's email-based `rbac.yml` is gone.
- Infrastructure Applications keep inlined values (fast local iteration without pushing). Only `rendered/` is synced from GitHub, so the repo must be pushed before `mise run up`.
