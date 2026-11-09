# Go CLI for cluster setup

Status: accepted. Replaces the bash internals of the setup mise tasks; behavior, order and `mise run <task>` entry points are unchanged.

Cluster setup (`up`, `down`, `infra`, `kargo-shards`, `region-dns`, `certs`, `creds`, `regions`, `kargo-restore`, `open`) was bash: parallel region bootstrap with `&`/`wait`, retry loops, jq/sed text surgery on Corefiles and Helm values, and macOS bash 3.2 pitfalls. None of it was unit-testable.

Now it is one Go CLI, `gcd` (`platform/cli`, own module, Go pinned in `.mise.toml`), built to `.bin/gcd` by mise task `cli`. Every setup task is a one-liner `exec .bin/gcd <cmd>`.

- **Libraries over CLIs.** kind (`sigs.k8s.io/kind/pkg/cluster`), Helm v4 SDK, client-go (typed core, dynamic client for Argo CD and Kargo CRDs as unstructured; no argo-cd or kargo module imports). Typed errors, `context` cancellation, `errgroup` for parallel hub/region work (first error cancels the rest); output lines prefixed `[hub]`, `[region-<r>]`.
- **Still subprocesses** where a tool is the interface: `docker` (v4-only network, hub IP), `mkcert`, `fnox` (secrets), `cue` (region list), `gh` (env branch heads), `open`.
- **Pure functions for logic.** `kargo.FreightFromBranch`, `kargo.KickCertPods` filter, Corefile rewrite, `argocd.RegionValues` (`{{.values.region}}` substitution) are unit-tested without a cluster; `argocd.WaitApp` is tested against the dynamic fake. `go vet` + `go test` run in `mise run test` and CI.
- **Stays outside Go:** the renderer (CUE, `platform/render/scripts/`), `platform/appsets/check.sh`, CI glue. Teams still never write code.

## Considered Options

- **Keep bash, add bats tests.** Least churn; doesn't fix bash 3.2 quirks or typed Kubernetes access, and shelling to `kubectl`/`helm` for every step stays slow and stringly.
- **Python.** Typed enough, but adds a runtime and venv to a repo whose toolchain is mise-pinned binaries; kind and Helm SDKs are Go.
- **Go shelling out to kubectl/helm/kind.** Smaller port, but keeps output parsing and version coupling to host binaries; libraries remove it.
- **Kubernetes operator/controller.** Overkill for local host-side bootstrap.

## Consequences

- Adding a setup step means Go code plus a test, not a bash edit; new hub add-ons that need waiting go in `infra.HubWaitRest`.
- `kubectl` and `helm` are no longer needed by setup tasks (still used interactively).
- Go becomes a pinned tool; first `mise run <setup task>` builds the binary (rebuilds only on source change).
- Removed: the bash helpers under `platform/bootstrap/` (app wait, restore lib and its test) and the task bodies; their logic lives in `argocd.WaitApp`/`ApplyRoot`, `kargo.Restore`/`FreightFromBranch`.
