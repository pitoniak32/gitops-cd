#!/usr/bin/env bash
# Validate a capability's YAML (+ teams/) with the full CUE rules (same as render/CI) and
# print diagnostics, one per line, paths relative to <root>:
#   <file>:<line>:<col>: <message>
# Exit 0 = clean. CI turns these into PR annotations.
# usage: lint.sh <root> <capability>
# needs: cue, yq (mikefarah v4)
set -uo pipefail
PLATFORM_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$1" && pwd)"; CAP="$2"
. "$PLATFORM_DIR/scripts/inputs.sh"
cue_args "$ROOT" "$CAP"

out="$(cd "$PLATFORM_DIR" && cue vet -c -E . "${CUE_ARGS[@]}" 2>&1)" && exit 0

yamlfile() { # <path>/<stem> under root -> existing .yml/.yaml (relative to root)
  for ext in yml yaml; do [[ -f "$ROOT/$1.$ext" ]] && { echo "$1.$ext"; return; }; done
  echo "$1.yml"
}

C="capabilities/$CAP"
# CUE path (apps."my-app".stages.qa) -> sets LOC_FILE, LOC_Q (yq expr for the key/item), LOC_LABEL
locate() {
  local segs=() s n=0 last=""
  while IFS= read -r s; do segs+=("${s//\"/}"); done < <(grep -oE '"[^"]*"|[^.]+' <<<"$1")
  LOC_Q=""; LOC_LABEL=""
  case "${segs[0]}" in
    apps) LOC_FILE="$(yamlfile "$C/apps/${segs[1]}/app")"; n=2 ;;
    stages | targets) LOC_FILE="$(yamlfile "$C/${segs[0]}/${segs[1]}")"; n=2 ;;
    capOwners) LOC_FILE="$(yamlfile "$C/owners")"; n=2 ;; # skip list index (0 or 1 file)
    appOwners) LOC_FILE="$(yamlfile "$C/apps/${segs[1]}/owners")"; n=2 ;;
    teams) LOC_FILE="$(yamlfile "teams/${segs[1]}")"; n=2 ;;
    stageDepth) LOC_FILE="$(yamlfile "$C/stages/${segs[1]}")"; LOC_Q=".dependsOn"; LOC_LABEL="dependsOn"; n=${#segs[@]} ;;
    deploys) # "app/target": point at the target entry under its stage
      local t="${segs[1]#*/}"
      LOC_FILE="$(yamlfile "$C/apps/${segs[1]%%/*}/app")"
      LOC_Q=".stages[].targets | select(has(\"$t\")) | .\"$t\""; LOC_LABEL="targets.$t"; n=2 ;;
    *) return 1 ;;
  esac
  while ((n < ${#segs[@]})); do
    s="${segs[n]}"; n=$((n + 1)); last="$s"
    if [[ $s =~ ^[0-9]+$ ]]; then LOC_Q+="[$s]"; else LOC_Q+=".\"$s\""; fi
    LOC_LABEL+="${LOC_LABEL:+.}$s"
  done
  # map keys: report the key's position; list items: the item's
  if [[ -n $last && ! $last =~ ^[0-9]+$ ]] || [[ ${segs[0]} == deploys && -z $last ]]; then LOC_Q="($LOC_Q) | key"; fi
  LOC_Q="${LOC_Q:-.}"
}

{
  # Blocks: "<cue path>: <message>:" followed by indented "    <file>:<line>:<col>" positions.
  printf '%s\n' "$out" | awk '
    function flush() { if (path != "") { n++; P[n] = path; M[n] = msg; Y[n] = yloc }; path = "" }
    /^[^ ]/ {
      flush()
      if ($0 ~ /errors in empty disjunction:$/ || $0 !~ /: /) next
      i = index($0, ": "); path = substr($0, 1, i - 1); msg = substr($0, i + 2); sub(/:$/, "", msg)
      yloc = ""; next
    }
    /^    / && path != "" && yloc == "" && $1 ~ /\.ya?ml:[0-9]+:[0-9]+$/ { yloc = $1 }
    END {
      flush()
      # drop follow-on errors nested under an already-reported path
      for (i = 1; i <= n; i++) {
        skip = 0
        for (j = 1; j <= n; j++) if (j != i && index(P[i], P[j] ".") == 1) { skip = 1; break }
        if (!skip) print P[i] "\t" M[i] "\t" Y[i]
      }
    }
  ' | while IFS=$'\t' read -r path msg yloc; do
    # skip disjunction noise ("conflicting values "dev" and "qa"") when a custom error exists
    [[ $msg == conflicting\ values* ]] && grep -qF "$path: " <<<"$(grep -v 'conflicting values' <<<"$out")" && continue
    locate "$path" || continue
    file="$LOC_FILE"; q="$LOC_Q"; label="$LOC_LABEL"
    if [[ -n $yloc ]]; then
      f="${yloc%%:*}"; pos="${yloc#*:}"
      [[ $f != /* ]] && f="$PLATFORM_DIR/$f"
      f="$(cd "$(dirname "$f")" && pwd)/$(basename "$f")"; f="${f#"$ROOT"/}"
      [[ $f == /* ]] || { echo "$f:$pos: ${label:+$label: }$msg"; continue; }
    fi
    pos="$(yq "($q) | [line, column] | join(\":\")" "$ROOT/$file" 2>/dev/null | head -1)"
    if [[ $pos =~ ^[1-9][0-9]*:[0-9]+$ ]]; then
      # yq numbers lines after a leading comment block (e.g. the $schema modeline); add it back
      off="$(awk '/^[[:space:]]*(#.*)?$/ { n++; next } { exit } END { print n + 0 }' "$ROOT/$file")"
      pos="$((${pos%%:*} + off)):${pos#*:}"
    else
      pos="1:1"
    fi
    echo "$file:$pos: ${label:+$label: }$msg"
  done
} | sort -t: -k1,1 -k2,2n -k3,3n | uniq
exit 1
