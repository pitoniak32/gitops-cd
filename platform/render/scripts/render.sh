#!/usr/bin/env bash
# Render one capability into rendered/ (the repo's scripts/render.sh runs every capability).
# usage: render.sh <root> <capability> <rendered-dir>
#   <root>/capabilities/<cap>/{owners.yml,stages/*.yml,targets/<t>/{target.yml,environment-variables/*.yml},apps/<app>/{app,owners}.yml}
#   <root>/teams/<team>.yml
# Wipes and rewrites only this capability's paths, so deletions prune.
set -euo pipefail
PLATFORM_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$1" && pwd)"; CAP="$2"; OUT="$(mkdir -p "$3" && cd "$3" && pwd)"
. "$PLATFORM_DIR/scripts/inputs.sh"
[[ -d "$ROOT/capabilities/$CAP" ]] || { echo "no capability dir $ROOT/capabilities/$CAP" >&2; exit 1; }
cue_args "$ROOT" "$CAP"

tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
cd "$PLATFORM_DIR"
# vet checks everything (export only evaluates what `files` references, missing e.g. typo'd fields)
cue vet -c . "${CUE_ARGS[@]}"
cue export . "${CUE_ARGS[@]}" -e files --out json >"$tmp"

rm -rf "$OUT/apps/$CAP" "$OUT/kargo/$CAP.yaml" "$OUT/argocd/$CAP.yaml" "$OUT"/argocd/*/"$CAP.yaml" "$OUT/pipelines/$CAP.json" "$OUT/codeowners/$CAP"
jq -r 'keys[]' "$tmp" | while read -r f; do
  mkdir -p "$OUT/$(dirname "$f")"
  jq -j --arg f "$f" '.[$f]' "$tmp" >"$OUT/$f"
  echo "wrote $f"
done
