# Go CLI (`gcd`) for cluster setup

Date: 2026-11-08. Status: approved design, pre-plan.

## Goal

Replace the bash cluster-setup tasks with one Go CLI, `gcd`, so the orchestration logic (waits,
parallel bootstrap, Kargo shard handoff, Kargo restore) is typed, testable with `go test`, and free of
macOS bash 3.2 pitfalls. Behavior, order and idempotency stay the same; `mise run <task>` stays the
entry point.

## Decisions

- Scope: all cluster setup: `up`, `down`, `infra`, `kargo-shards`, `region-dns`, `certs`, `creds`,
  `regions`, `kargo-restore`, `open`. Out of scope: the renderer (`platform/render/scripts/*`, CUE),
  `platform/appsets/check.sh`, `k9s` (personal Rex tooling).
- Libraries, not CLIs: kind's Go package (`sigs.k8s.io/kind/pkg/cluster`), Helm v4 SDK
  (`helm.sh/helm/v4`, matching `.mise.toml` helm 4.x), client-go (typed core + dynamic client for
  Argo CD/Kargo CRDs as unstructured; no argo-cd/kargo Go module imports).
- Still subprocesses: `docker` (v4-only network, hub container IP), `mkcert`, `fnox` (secrets),
  `cue` (region list from `clusters.cue`), `gh` (env branch heads in restore), `open` (browser).
- Entry point: mise tasks stay; each becomes `exec "$GCD" <cmd> "$@"` style one-liner. Binary `gcd`.
- CI: builds the CLI, runs `go vet` + `go test`.

## Layout

- `platform/cli/` own module (`go.mod`, module path `github.com/pitoniak32/gitops-cd/platform/cli`).
  Go pinned in `.mise.toml`.
- `platform/cli/main.go` + `internal/`:
  - `cmd`: cobra root + one file per subcommand; flags mirror today's `#USAGE` args
    (`render`-style positional caps for `kargo-restore`, `--print` for `open`).
  - `kube`: per-context clients (KUBECONFIG from env, contexts `kind-kargo-demo`, `kind-region-<r>`),
    server-side apply of objects/YAML, namespace/secret helpers.
  - `cluster`: kind create/delete from `platform/cluster/kind.yaml` + `region.yaml` (name per region),
    docker network `kargo-demo` (`--ipv4 --ipv6=false`), regions created in parallel.
  - `regions`: region list via `cue export clusters.cue regions.cue -e regionConfig`; writes
    `platform/cluster/regions`, keycloak clients, kargo urls (same output as today's `regions` task,
    byte-identical files).
  - `argocd`: Helm v4 SDK install of `argo-cd` (pin + values from the AppSet file, regional
    `{{.values.region}}` substitution), `ApplyRoot` (wait for controllers, apply, force refresh),
    `WaitApp` (exists → refresh → Synced → Healthy, via watch with timeout).
  - `dns`: hub CoreDNS rewrite, regional hub-traefik Service/EndpointSlice + rewrite; Corefile edit is a
    pure function.
  - `certs`: run mkcert into a temp dir, apply `traefik/local-tls`.
  - `creds`: `fnox` for `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY`;
    apply `kargo-shared-resources/github-gitops-cd` (cred-type git), drop legacy keys and old Argo CD
    repo secret.
  - `kargo`: shard token handoff (hub token → region `kargo/kargo-hub-kubeconfig`, restart controller on
    change), `KickCertPods` (pure filter + delete), `Restore` (pure `FreightFromBranch` + apply/approve/
    promote/wait).
- `.bin/gcd` built by mise task `cli` with `sources`/`outputs` (rebuild only on change); `.bin/` ignored.

## Behavior (unchanged from bash)

- `up`: down → network → hub + regions (parallel) → infra → wait https://kargo.localhost 200.
- `infra`: hub CoreDNS → region-dns → regions bootstrap in parallel with hub (helm install no wait,
  ApplyRoot, WaitApp argocd) → hub helm install → ApplyRoot → WaitApp traefik → certs → KickCertPods →
  WaitApp kargo → creds → WaitApp rest → join regions → WaitApp argocd-ui-<r> → kargo-shards.
- Parallelism via `errgroup`; first error cancels the rest. Output lines prefixed `[hub]`,
  `[region-<r>]`.
- Every command idempotent.

## Testing

- `go test ./...` unit tests (no cluster): FreightFromBranch (port of `kargo-restore-test.sh` cases),
  KickCertPods filter (plain + projected volumes), Corefile rewrite (insert once, idempotent), regional
  values substitution, regions file outputs equal current committed files, WaitApp against
  client-go dynamic fake.
- `mise run test` adds `go vet ./...` + `go test ./...` in `platform/cli`.
- Proof: clean timed `mise run up` (all apps Synced/Healthy hub + regions, time not worse than 286s),
  `mise run kargo-restore` on the rebuilt cluster restores the same Stages as the bash version,
  `mise run regions` produces no diff, re-runs are no-ops.

## Removed

`platform/mise-tasks/{infra,kargo-shards,region-dns,certs,creds,regions,kargo-restore,open}` bodies,
`up`/`down` bodies (become one-liners), `platform/bootstrap/{wait-app.sh,kargo-restore-lib.sh,
kargo-restore-test.sh}`.

## Docs

ADR 0018 (Go CLI for setup; why libraries; what stays bash/CUE). Update `CLAUDE.md` (Layout, Rules:
idempotent tasks now in `platform/cli`), `README.md` layout, `docs/sync-boundaries.md` mentions,
`docs/gotchas.md` references to bash helpers.

## Out of scope

Renderer rewrite; Windows/Linux setup support (fnox/keychain remain macOS); typed Argo CD/Kargo imports.
