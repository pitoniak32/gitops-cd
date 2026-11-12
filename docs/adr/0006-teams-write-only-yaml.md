# Teams write only YAML

Teams describe delivery in plain YAML: capabilities, apps, stages, targets and owners. The platform validates it (`mise run lint`, `file:line:col` messages) and renders every Kubernetes, Kargo and Argo CD resource from it. Every option is a YAML field. Teams never write templates, config-language code, promotion steps or Kargo/Argo CD resources.

## Considered Options

- **Helm charts or text templating**: teams would read and debug templates, and there is no cross-file validation.
- **Teams write CUE** (probed while the renderer was CUE): fencing worked. Team CUE could be exported in isolation, a pattern constraint rejected stray fields, and an import allowlist was enforced. The editor was the blocker: `cue lsp` gave only syntax diagnostics, and `error()` messages pointed at the schema, not the team's file.
- **Per-team escape hatches** (raw manifests, custom steps): rejected. Platform-wide changes and policy (e.g. pre-deploy checks, ADR 0028) need every deploy to be platform-generated.
