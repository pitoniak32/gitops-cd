#!/usr/bin/env bash
# Write platform-level rendered files (regions.cue `regionFiles`) under <rendered-dir>.
# usage: regions.sh <rendered-dir>
set -euo pipefail
PLATFORM_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$(mkdir -p "$1" && cd "$1" && pwd)"
tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
cd "$PLATFORM_DIR"
cue export clusters.cue regions.cue -e regionFiles --out json >"$tmp"
rm -rf "$OUT/infrastructure"
jq -r 'keys[]' "$tmp" | while read -r f; do
  mkdir -p "$OUT/$(dirname "$f")"
  jq -j --arg f "$f" '.[$f]' "$tmp" >"$OUT/$f"
  echo "wrote $f"
done
