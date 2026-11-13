# Go CLI `gcd` for setup and rendering

Cluster setup (`up`, `down`, `infra`, `kargo-shards`, `region-dns`, `certs`, `creds`, `regions`, `kargo-restore`, `open`) and the renderer (`render`, `lint`, `codeowners`, ADR 0030) are one Go CLI, `gcd` (`platform/cli`, its own module, Go pinned in `.mise.toml`). The mise task `build` builds it to `.bin/gcd` (on PATH via mise); it is the only mise task (mise pins only Go). Before this, setup was bash: `&`/`wait` parallelism, retry loops, jq/sed surgery on Corefiles and Helm values, macOS bash 3.2 pitfalls, and nothing was unit-testable.

- **Libraries over CLIs**: kind (`sigs.k8s.io/kind/pkg/cluster`), the Helm v4 SDK and client-go (a dynamic client for Argo CD and Kargo CRDs, with no argo-cd or kargo module imports). `errgroup` runs hub and region work in parallel, and the first error cancels the rest. Output lines are prefixed `[hub]` or `[region-<r>]`.
- **Subprocesses** only where a tool is the interface: `docker`, `gh`, `open` and macOS `security` (keychain secrets, CA trust). kind, Helm and oras are Go libraries; the local CA and certs are made in Go.
- **Pure functions for logic** (Corefile rewrite, region values, Freight from a branch) are unit-tested without a cluster. `go vet` and `go test` run in `gcd test` and CI.

## Considered Options

- **Bash plus bats tests**: keeps the bash 3.2 quirks and stringly `kubectl`/`helm` calls.
- **Python**: adds a runtime and a venv, and the kind and Helm SDKs are Go.
- **Go shelling out to kubectl/helm/kind**: keeps output parsing and version coupling to host binaries.

## Consequences

- A new setup step is Go code plus a test. A new hub add-on that needs waiting goes in `infra.HubWaitRest`.
- Setup doesn't need `kubectl` or `helm`.
