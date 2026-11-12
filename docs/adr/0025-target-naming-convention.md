# Target naming convention and environment types

Target names show up in Kargo Stage names, `env/*` branches and Argo CD Applications, so they follow a fixed shape that lint checks against the target's own fields:

`<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]`, e.g. `us-east4-npr-k8s-perf`.

- `<region>`: K8S takes the cluster's region. TERRAFORM and CLOUD_RUN take `region`, one of `GcpRegions` in `config.go`.
- `<qualifier>` (`[a-z0-9]+`, optional) separates targets that share the first three parts.

A target has two environment fields:

- `environment`: a free, descriptive name (`staging`, `perf`, `qa1`), used in the `example.com/environment` label.
- `environmentType`: `dev | npr | prd`, the class that policy keys on. It drives the name part, which cluster accepts the target, the replica floor (`dev` 0, `npr`/`prd` 1), the pre-deploy checks (ADR 0028), the no-`autoPromote`-to-`prd` rule, and the `example.com/environment-type` label.

Stage names (`dev`, `staging`, `prod`) are independent: stages are the pipeline, and environments are where a target runs. Region names are real ones (`us-east4`, or `dc1`/`dc2` as placeholder on-prem datacenters), and the local kind clusters are named after what they stand in for.

## Considered Options

- **Convention without lint**: names drift as soon as someone copies a target.
- **Replace `environment` with `dev|npr|prd`**: loses the descriptive name (`staging` vs `perf`).
- **Derive `environmentType` from a fixed `environment` list**: a hidden mapping, and a platform change for every new name.

## Consequences

- Names are at most 40 characters, so `us-central1-npr-k8s-` leaves 19 for a qualifier.
- A new GCP region or datacenter is a platform change to `config.go`.
- Renaming a target creates a new Kargo Stage and branch. History doesn't carry over.
