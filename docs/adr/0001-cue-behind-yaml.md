# CUE behind team-written YAML

Status: superseded by ADR 0030 (the renderer is Go; teams still write only YAML).

Teams describe delivery in plain YAML; the platform's CUE validates it and renders Kubernetes, Kargo and Argo CD resources. We chose this over Helm charts or text templating because CUE gives typed schemas, cross-file validation with file:line errors, and one source for both validation and generation, while teams never need to read or write CUE.
