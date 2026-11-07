# Sourced by render.sh / lint.sh. cue_args <root> <cap> fills CUE_ARGS with the
# capability's YAML (+ teams/) and the flags that route each file to
# in."<path relative to root>" (input.cue sorts them into stages/targets/apps/...).
cue_args() {
  local root cap="$2"; root="$(cd "$1" && pwd)"
  local c="$root/capabilities/$cap"
  shopt -s nullglob
  CUE_ARGS=("$root"/teams/*.y*ml "$c"/owners.y*ml "$c"/stages/*.y*ml "$c"/targets/*.y*ml
    "$c"/apps/*/app.y*ml "$c"/apps/*/owners.y*ml)
  shopt -u nullglob
  CUE_ARGS+=(--with-context -l 'in:' -l "strings.TrimPrefix(filename, \"$root/\")" -t capability="$cap")
  if [[ -n "${GITOPS_REPO:-}" ]]; then CUE_ARGS+=(-t repo="$GITOPS_REPO"); fi
}
