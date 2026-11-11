#!/usr/bin/env bash
# Static checks for platform/appsets/ (no cluster needed). Run by `mise run test`.
set -euo pipefail
cd "$(dirname "$0")/../.."
fail() { echo "appsets: $*" >&2; exit 1; }

# every add-on dir has an AppSet; no inlined Application left
for d in platform/infrastructure/*/; do
  a=$(basename "$d")
  [[ -f "platform/appsets/addons/$a.yaml" ]] || fail "no platform/appsets/addons/$a.yaml for $d"
done
ls platform/infrastructure/*/app.yaml >/dev/null 2>&1 && fail "platform/infrastructure/*/app.yaml must be gone (AppSets replace them)"
if grep -rn __SHARD__ platform/ --exclude=check.sh; then fail "__SHARD__ templating left (use {{.values.region}} / {{.region}})"; fi

# every AppSet: go templates with missingkey=error; never cascade-delete its Applications
for f in $(find platform/appsets -name '*.yaml' | sort); do
  [[ "$(yq '.kind' "$f")" == ApplicationSet ]] || fail "$f: not an ApplicationSet"
  [[ "$(yq '.spec.goTemplate' "$f")" == true ]] || fail "$f: goTemplate must be true"
  [[ "$(yq -o=json -I=0 '.spec.goTemplateOptions' "$f")" == '["missingkey=error"]' ]] || fail "$f: goTemplateOptions must be [missingkey=error]"
  [[ "$(yq '.spec.syncPolicy.preserveResourcesOnDeletion' "$f")" == true ]] || fail "$f: syncPolicy.preserveResourcesOnDeletion must be true"
  # finalizer on a generated App = cascade delete of its resources when the generator stops matching
  [[ "$(yq '.spec.template.metadata | has("finalizers")' "$f")" == false ]] || fail "$f: template must not set finalizers"
  for v in $(yq '.spec.template.spec.sources[].helm.valueFiles[]' "$f" 2>/dev/null); do
    [[ -f "${v#\$values/}" ]] || fail "$f: valueFile $v missing"
  done
done

# add-ons are created all at once (ordering = retries until deps exist): retries must never run out
for f in platform/appsets/addons/*.yaml; do
  [[ "$(yq '.spec.template.spec.syncPolicy.retry.limit' "$f")" == -1 ]] || fail "$f: syncPolicy.retry.limit must be -1 (unlimited)"
done

# region from cluster labels: the selector must require the label (a missing label would otherwise
# template an empty region: shard id "", paths deploys//), and the lookup must use exactly that key
want_region="{{index .metadata.labels \"example.com/region\"}}"
for f in $(grep -rl --include='*.yaml' 'example.com/role: region' platform/appsets); do
  [[ "$(yq '[.. | select(has("matchExpressions")) | .matchExpressions[] | select(.key == "example.com/region" and .operator == "Exists")] | length' "$f")" -ge 1 ]] \
    || fail "$f: region cluster selector needs matchExpressions [{key: example.com/region, operator: Exists}]"
  for v in $(yq '.. | select(has("values")) | .values.region' "$f" | grep -v '^null$' | tr ' ' '~'); do
    [[ "${v//\~/ }" == "$want_region" ]] || fail "$f: values.region must be '$want_region', got '${v//\~/ }'"
  done
done

# a region may lose its last capability: argocd-projects must be allowed to prune to empty
[[ "$(yq '.spec.template.spec.syncPolicy.automated.allowEmpty' platform/appsets/projects.yaml)" == true ]] || fail "projects.yaml: automated.allowEmpty must be true"

# gcd infra (argocd package) reads chart pin + valuesObject from sources[0]: the chart source must come first
for f in platform/appsets/addons/*.yaml; do
  if [[ "$(yq '[.spec.template.spec.sources[] | select(has("chart"))] | length' "$f")" -gt 0 ]]; then
    [[ "$(yq '.spec.template.spec.sources[0] | has("chart")' "$f")" == true ]] || fail "$f: chart source must be sources[0]"
  fi
done

# chart pins that must move together
pin() { yq '.spec.template.spec.sources[] | select(has("chart")) | .targetRevision' "platform/appsets/addons/$1.yaml"; }
[[ "$(pin kargo)" == "$(pin kargo-shard)" && "$(pin kargo)" == "$(pin kargo-shard-hub)" ]] || fail "kargo chart pin differs: kargo=$(pin kargo) kargo-shard=$(pin kargo-shard) kargo-shard-hub=$(pin kargo-shard-hub)"
[[ "$(pin argocd)" == "$(pin argocd-region)" ]] || fail "argo-cd chart pin differs: argocd=$(pin argocd) argocd-region=$(pin argocd-region)"

# root app syncs platform/appsets; bootstrap AppSets exist
[[ "$(yq '.spec.source.path' platform/bootstrap/root.yaml 2>/dev/null)" == platform/appsets ]] || fail "platform/bootstrap/root.yaml must sync platform/appsets"
for f in kargo projects workloads; do [[ -f "platform/appsets/$f.yaml" ]] || fail "missing platform/appsets/$f.yaml"; done
# workloads template reads only keys the renderer emits (golden deploy params; values.* is the cluster gen)
keys=$(jq -r 'keys[]' platform/render/testdata/golden/argocd/deploys/dc1/payments/my-app-onprem-prod.json)
for k in $(grep -o '{{\.[A-Za-z.]*}}' platform/appsets/workloads.yaml | sed 's/{{\.\(.*\)}}/\1/' | sort -u); do
  [[ $k == values.* ]] && continue
  grep -qx "$k" <<<"$keys" || fail "workloads.yaml uses {{.$k}}, not a deploy params key"
done

# ADR 0022: rendered/ lives on the rendered branch (CI-written); main has none. Every git source/generator
# reading rendered/ uses revision rendered, and nothing else does.
for f in $(find platform/appsets -name '*.yaml' | sort); do
  while IFS=$'\t' read -r rev paths; do
    [[ -z "$rev" ]] && continue
    if [[ "$paths" == rendered/* ]]; then [[ "$rev" == rendered ]] || fail "$f: $paths must be read from revision rendered, not $rev"
    elif [[ "$rev" == rendered ]]; then fail "$f: revision rendered only holds rendered/, not '$paths'"; fi
  done < <(yq -r '.. | select(type == "!!map" and (has("revision") or has("targetRevision"))) | [(.revision // .targetRevision), ([.path, (.files[]?.path)] | map(select(. != null)) | join(","))] | @tsv' "$f")
done
echo "appsets ok"
