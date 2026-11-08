#!/usr/bin/env bash
# Unit tests for kargo-restore-lib.sh (no cluster, no network). Run by `mise run test`.
set -euo pipefail
cd "$(dirname "$0")"
. ./kargo-restore-lib.sh
fail() { echo "kargo-restore: $*" >&2; exit 1; }
R=https://github.com/pitoniak32/gitops-cd I=ghcr.io/pitoniak32/nginx
msg="promote payments/my-app to dev/us-dev from a35be032c36807442071c8cb1f069718a14b1aa2, image 1.1.0"
img="ghcr.io/pitoniak32/nginx:1.1.0@sha256:c8fa106acc6d713fc0cf8c00906b1d54157ff9cb77f6aa3f65ba1c27348e4ce6"

# env branch head -> Freight (same artifacts as the original, so Kargo computes the same Freight ID)
got=$(freight_json my-app "$R" "$I" "$msg" "$img" | jq -c .)
want='{"apiVersion":"kargo.akuity.io/v1alpha1","kind":"Freight","metadata":{},"origin":{"kind":"Warehouse","name":"my-app"},"commits":[{"repoURL":"https://github.com/pitoniak32/gitops-cd","id":"a35be032c36807442071c8cb1f069718a14b1aa2"}],"images":[{"repoURL":"ghcr.io/pitoniak32/nginx","tag":"1.1.0","digest":"sha256:c8fa106acc6d713fc0cf8c00906b1d54157ff9cb77f6aa3f65ba1c27348e4ce6"}]}'
[[ "$got" == "$want" ]] || fail "freight_json: got $got"

# not restorable -> non-zero + reason (never a Freight that would re-promote something different)
r=$(freight_json my-app "$R" "$I" "promote payments/worker to dev/us-dev from a4b1dea7d42e7ba001b9292983998890cbaed8b6" "" 2>&1) && fail "pre-ADR-0016 branch (no deployment.yaml) must be skipped"
grep -q "no pinned image" <<<"$r" || fail "reason for missing image: $r"
r=$(freight_json my-app "$R" "$I" "manual edit" "$img" 2>&1) && fail "non-Kargo head commit must be skipped"
grep -q "not a Kargo promotion commit" <<<"$r" || fail "reason for foreign commit: $r"
r=$(freight_json my-app "$R" "$I" "$msg" "ghcr.io/other/img:1.0@sha256:abc" 2>&1) && fail "image from another repo must be skipped"
grep -q "not ghcr.io/pitoniak32/nginx" <<<"$r" || fail "reason for other image repo: $r"
r=$(freight_json my-app "$R" "$I" "$msg" "ghcr.io/pitoniak32/nginx:1.1.0" 2>&1) && fail "image without digest must be skipped"
grep -q "no pinned image" <<<"$r" || fail "reason for undigested image: $r"
echo "kargo-restore ok"
