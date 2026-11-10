#!/usr/bin/env bash
# tf-module-push idempotency with stub oras/fnox/gh: an existing tag is never re-pushed (the tarball isn't
# reproducible, a re-push would move the tag), a missing tag is pushed, a registry/auth error fails.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin"
cat >"$tmp/bin/oras" <<'S'
#!/usr/bin/env bash
echo "oras $*" >>"$LOG"
case "$1 $2" in
  "login ghcr.io") cat >/dev/null; exit 0 ;;
  "manifest fetch")
    case "$FAKE" in
      exists) echo '{}' ;;
      missing) echo "Error: failed to fetch the content of \"$3\": $3: not found" >&2; exit 1 ;;
      *) echo "Error: GET https://ghcr.io/v2/...: response status code 401: unauthorized" >&2; exit 1 ;;
    esac ;;
  push*) exit 0 ;;
esac
S
printf '#!/usr/bin/env bash\necho tok\n' >"$tmp/bin/fnox"
printf '#!/usr/bin/env bash\necho someone\n' >"$tmp/bin/gh"
chmod +x "$tmp/bin"/*
run() { LOG="$tmp/log.$1" FAKE="$1" PATH="$tmp/bin:$PATH" MISE_PROJECT_ROOT="$root" "$root/platform/mise-tasks/tf-module-push" 1.0.0 >"$tmp/out.$1" 2>&1; }
run exists || { echo "existing tag: must exit 0" >&2; cat "$tmp/out.exists" >&2; exit 1; }
grep -q '^oras push' "$tmp/log.exists" && { echo "existing tag must not be re-pushed" >&2; exit 1; }
grep -q '^oras login' "$tmp/log.exists" || { echo "must log in before checking the tag (private packages)" >&2; exit 1; }
run missing || { echo "missing tag: must exit 0" >&2; cat "$tmp/out.missing" >&2; exit 1; }
grep -q '^oras push ghcr.io/pitoniak32/tf-bucket:1.0.0 .*--annotation org.opencontainers.image.source=https://github.com/pitoniak32/gitops-cd' "$tmp/log.missing" || { echo "missing tag: push with source annotation" >&2; cat "$tmp/log.missing" >&2; exit 1; }
if run error; then echo "registry error must fail" >&2; exit 1; fi
grep -q '^oras push' "$tmp/log.error" && { echo "registry error must not push" >&2; exit 1; }
echo "tf-module-push ok"
