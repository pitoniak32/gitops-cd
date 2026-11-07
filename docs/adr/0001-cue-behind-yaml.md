# CUE behind team-written YAML

Teams describe delivery in plain YAML; the platform's CUE validates it and renders Kubernetes, Kargo and Argo CD resources. We chose this over Helm charts or text templating because CUE gives typed schemas, cross-file validation with file:line errors, and one source for both validation and generation, while teams never need to read or write CUE.
