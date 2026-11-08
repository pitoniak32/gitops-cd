# Kargo controller shard per region
Status: needs-triage
Blocked by: 01

- **Hub:** a `kargo-ctrl-<shard>` chart release per shard (see spec "Spike results"; values proven).
- **Region:** kargo-shard add-on with the spike values, including the events Role.
- **mise:** copies the shard token into a region `kargo-hub-kubeconfig` Secret (re-run safe).

Done when: both shard controllers are running and connected (`kubectl get stage` SHARD column populated), and a manual promotion on each shard succeeds.

## Comments
- Implemented 2026-11-08. Addons `platform/infrastructure/kargo-shard-hub/` (hub, one Application per region `kargo-ctrl-<r>`) and `kargo-shard/` (region), templated with `__SHARD__`; `infra-render` takes `SHARD=<region>` and sets the chart pin from `kargo/app.yaml`. New task `platform/mise-tasks/kargo-shards` (called at end of `infra`): hub apps -> token handoff to region Secret `kargo-hub-kubeconfig` (restart controller on change) -> regional app. Add a region = one line in `platform/cluster/regions`.
