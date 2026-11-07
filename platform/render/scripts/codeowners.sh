#!/usr/bin/env bash
# Assemble <root>/.github/CODEOWNERS from <root>/.github/CODEOWNERS.tmpl, replacing the
# line "#GENERATED" with every capability fragment in <rendered-dir>/codeowners/ (sorted).
# Lines after #GENERATED win (last match wins), so platform-only paths go there.
# Also emits one line per teams/<team>.yml: that team's githubTeam plus the owners of the
# template's "/teams/" line (so a team or platform can approve its own file; it grants no access).
# usage: codeowners.sh <root> <rendered-dir>
set -euo pipefail
ROOT="$1"; OUT="$2"; tmpl="$ROOT/.github/CODEOWNERS.tmpl"
[[ "$(grep -c '^#GENERATED$' "$tmpl")" == 1 ]] || { echo "$tmpl needs exactly one #GENERATED line" >&2; exit 1; }
gen="$(mktemp)"; trap 'rm -f "$gen"' EXIT
shopt -s nullglob; frags=("$OUT"/codeowners/*); shopt -u nullglob
if ((${#frags[@]})); then cat "${frags[@]}" >"$gen"; fi   # glob expands sorted
teamsOwners="$(awk '$1 == "/teams/" { $1 = ""; sub(/^ +/, ""); print; exit }' "$tmpl")"
shopt -s nullglob
for f in "$ROOT"/teams/*.y*ml; do
  echo "/teams/$(basename "$f") @$(yq '.githubTeam' "$f")${teamsOwners:+ $teamsOwners}" >>"$gen"
done
shopt -u nullglob
awk -v gen="$gen" '$0 == "#GENERATED" { while ((getline l < gen) > 0) print l; next } { print }' "$tmpl" >"$ROOT/.github/CODEOWNERS"
