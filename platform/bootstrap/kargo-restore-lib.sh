# Sourced by mise-tasks/kargo-restore and its test. Pure functions: no cluster, no network.

# freight_json <warehouse> <git repoURL> <image repoURL> <env branch head message> <deployment image ref>
# Rebuilds the Freight a target's env branch was promoted from: the source commit from Kargo's commit
# message ("promote <cap>/<app> to <stage>/<target> from <sha>[, image <tag>]") and the pinned image
# (<repo>:<tag>@<digest>) from the branch's deployment.yaml. Same artifacts -> Kargo computes the same
# Freight ID. Prints Freight JSON, or a reason on stderr and returns 1 when the branch can't be restored.
freight_json() {
  local wh=$1 repo=$2 irepo=$3 msg=$4 img=$5 sha tag digest
  sha=$(sed -nE 's/^promote [^ ]+ to [^ ]+ from ([0-9a-f]{40})(,.*)?$/\1/p' <<<"$msg" | head -1)
  [[ -n "$sha" ]] || { echo "head is not a Kargo promotion commit: ${msg%%$'\n'*}" >&2; return 1; }
  [[ "$img" =~ ^(.+):([^:@/]+)@(sha256:[0-9a-f]+)$ ]] || { echo "no pinned image (<repo>:<tag>@<digest>) in deployment.yaml (promoted before ADR 0016?)" >&2; return 1; }
  [[ "${BASH_REMATCH[1]}" == "$irepo" ]] || { echo "deployment image ${BASH_REMATCH[1]} is not $irepo" >&2; return 1; }
  tag=${BASH_REMATCH[2]} digest=${BASH_REMATCH[3]}
  jq -n --arg wh "$wh" --arg repo "$repo" --arg sha "$sha" --arg irepo "$irepo" --arg tag "$tag" --arg digest "$digest" '{
    apiVersion: "kargo.akuity.io/v1alpha1", kind: "Freight", metadata: {},
    origin: {kind: "Warehouse", name: $wh},
    commits: [{repoURL: $repo, id: $sha}],
    images: [{repoURL: $irepo, tag: $tag, digest: $digest}]}'
}
