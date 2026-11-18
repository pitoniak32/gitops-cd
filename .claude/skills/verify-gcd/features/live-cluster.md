# Live cluster (read-only)

The local kind clusters run the pipeline: hub `kind-gitops-cd` (Kargo control plane) and `kind-region-<r>` (Argo CD and a Kargo shard per region). They sync the GitHub `rendered` branch. This is the user's singleton instance, so it is observed only and never driven.

## Sub-features

- `cluster-up`: contexts reachable.
- `cluster-stages`: Kargo Stages per `kargo-<sys>-<app>` project, with Freight and health.
- `cluster-apps`: Argo CD Applications per region.

## How to get to it (user POV)

- The UIs listed in `README.md` (https://kargo.localhost, https://argocd-<region>.localhost). `gcd open --print` lists the URLs.

## Driving it with verify-gcd

Preconditions:

- The user already has clusters up (`docker ps` shows `gitops-cd-control-plane` and `region-<r>-control-plane`). If not, report the journey as unreachable. Don't run `gcd up`.

- **Contexts.** Run `eval "$(gcd env)"; kubectl config get-contexts -o name`. Result: `kind-gitops-cd`, `kind-region-dc1` and `kind-region-us-east4`.
- **Stages.** Run `kubectl --context kind-gitops-cd get stages -A`. Result: rows per `kargo-<sys>-<app>` namespace. Target Stages show a shard (`dc1` or `us-east4`).
- **Apps.** Run `kubectl --context kind-region-us-east4 -n argocd get applications`. Result: workload apps of targets on that cluster.
- **Proof.** Save the command output to `.verify/<run>/evidence/cluster-*.txt`.

## Gotchas

- Local edits never reach the cluster. It reads the pushed `rendered` branch, and pushing is off-limits. This journey can't prove an unpushed change. Use the CLI features for that.
- The hub API can time out (`TLS handshake timeout`) when Docker is under load. That is an environment state to report, not a gcd failure. On 2026-11-18 the hub answered `get stages` but timed out on discovery for `applications`, which live on the regional clusters anyway.
- Stages may be `Unhealthy` in a long-running cluster. Report the state you see. Don't fix it during verification.
- Never `gcd up|down|infra|kargo-restore|kargo-shards|creds|certs`, and never kubectl write verbs.
