#!/usr/bin/env bash
# Fail if two capabilities render the same workload Application name in one region (<cap>-<app>-<target>
# can collide: cap a/app b-c vs cap a-b/app c). One duplicate breaks that region's whole `workloads` AppSet.
# usage: unique-apps.sh <rendered-dir>   (run by mise run render after all capabilities)
set -euo pipefail
d="$1/argocd/deploys"
[[ -d "$d" ]] || exit 0
dups=$(for r in "$d"/*/; do
  find "$r" -name '*.json' -exec jq -r --arg r "$(basename "$r")" '$r + " " + .appName' {} + | sort | uniq -d
done)
[[ -z "$dups" ]] && exit 0
while read -r r n; do echo "duplicate workload Application name $n in region $r: rename a capability or app" >&2; done <<<"$dups"
exit 1
