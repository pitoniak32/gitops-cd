#!/usr/bin/env bash
# Re-render only what a change affects (ADR 0022): changed paths on stdin -> affected.sh. "all" re-renders
# every capability + regions; else each affected capability (a deleted one is pruned); none: rendered/ is
# untouched. CODEOWNERS is always reassembled. Same result as a full render when <rendered-dir> matched the base before the change.
# usage: render-affected.sh <root> <rendered-dir> < changed-paths   (ALL=1: everything, no stdin)
set -euo pipefail
S="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$1" && pwd)"; OUT="$(mkdir -p "$2" && cd "$2" && pwd)"
if [[ "${ALL:-}" == 1 ]]; then affected=all; else affected="$("$S/affected.sh")"; fi
if [[ "$affected" == all ]]; then
  rm -rf "$OUT/apps" "$OUT/argocd" "$OUT/kargo" "$OUT/pipelines" "$OUT/codeowners" "$OUT/infrastructure"
  affected="$(cd "$ROOT/capabilities" && ls)"
  "$S/regions.sh" "$OUT" >/dev/null
fi
for cap in $affected; do
  if [[ -d "$ROOT/capabilities/$cap" ]]; then "$S/render.sh" "$ROOT" "$cap" "$OUT" >/dev/null; echo "rendered $cap"
  else PRUNE=1 "$S/render.sh" "$ROOT" "$cap" "$OUT"; echo "pruned $cap"; fi
done
"$S/unique-apps.sh" "$OUT"
"$S/codeowners.sh" "$ROOT" "$OUT"
