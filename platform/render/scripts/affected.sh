#!/usr/bin/env bash
# Which capabilities a change needs re-rendered (ADR 0022). Reads changed paths (repo-relative, one per
# line) on stdin; prints "all", or the affected capability names (sorted, one per line), or nothing.
#   capabilities/<cap>/...                     -> <cap> (also when the capability was deleted)
#   rendered/ output of <cap> (bot or by hand) -> <cap>: re-rendering overwrites it
#   teams/, renderer (CUE, cue.mod, render
#   scripts), rendered/infrastructure/         -> all
#   anything else (docs, tests, schemas, cli,
#   CODEOWNERS: always reassembled)            -> nothing
set -euo pipefail
all='^(teams/|platform/render/[^/]+\.cue$|platform/render/scripts/|platform/cue\.mod/|rendered/infrastructure/)'
in="$(cat)"
if grep -qE "$all" <<<"$in"; then echo all; exit; fi
sed -nE \
  -e 's#^capabilities/([^/]+)/.*#\1#p' \
  -e 's#^rendered/(apps|codeowners)/([^/]+).*#\2#p' \
  -e 's#^rendered/(kargo|pipelines)/([^/]+)\.(yaml|json)$#\2#p' \
  -e 's#^rendered/argocd/deploys/[^/]+/([^/]+)/.*#\1#p' \
  -e 's#^rendered/argocd/projects/[^/]+/([^/]+)\.yaml$#\1#p' <<<"$in" | sort -u
