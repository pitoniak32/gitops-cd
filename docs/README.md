# Docs

Pick the path for what you're doing.

## App teams: ship an app

You own a capability under `capabilities/` and want to deploy, change config, or promote.

1. [Concepts](guide/concepts.md): capability, app, stage, target, Freight, and how a YAML change reaches a cluster.
2. [Onboard a capability](guide/onboard.md): team file, `owners.yml`, first stage, target and app.
3. [Configure apps](guide/apps.md): image or module, layered overrides, env, secrets, resources.
4. [Targets and stages](guide/targets-stages.md): naming, platforms (K8S, TERRAFORM, CLOUD_RUN), `deployWith: gha`, pre-deploy checks.
5. [Promote and access](guide/promote-access.md): promoters, viewers, break-glass, what you see in Kargo and Argo CD.
6. [Working loop](guide/workflow.md): `gcd schema`, `gcd show`, `gcd lint`, the PR diff comment.
7. [Troubleshooting](guide/troubleshooting.md): common lint errors and "merged but not deployed".

Each file kind explained, with examples from the sandbox: [reference/](reference/) (one page per kind: [owners](reference/owners.md), [stage](reference/stage.md), [app](reference/app.md), [target](reference/target.md), [env-vars](reference/env-vars.md)).

## Evaluators: what this shows and why

- [Root README](../README.md): overview, local setup, logins, the delivery flow.
- [ADRs](adr/): every design decision with its context and trade-offs.
- [Glossary](../GLOSSARY.md): the terms used everywhere.

## Platform engineers: run and change the platform

- [Operations](operations.md): runbooks (break-glass, resetting Kargo state).
- [Sync boundaries](sync-boundaries.md): what Argo CD pulls from git vs what needs `mise run infra` / `up`.
- [Gotchas](gotchas.md): networking, OIDC, Argo CD health, Kargo RBAC.
- [Break-glass](break-glass.md): the approval guard.
- [FAQ](faq.md): Kargo topology, sharding, scaling.
