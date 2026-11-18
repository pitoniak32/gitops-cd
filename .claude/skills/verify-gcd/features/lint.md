# Lint

`gcd lint` validates system and team YAML with the full rules. It prints `file:line:col: label: message` and exits non-zero on any problem. It is the only validator app teams have.

## Sub-features

- `lint-clean`: clean inputs exit 0 with no stdout.
- `lint-schema`: per-file field and type errors (unknown field, out-of-range value).
- `lint-crossfile`: cross-file rules, e.g. `envFrom` naming a missing `environment-variables/<name>.yml`.
- `lint-formats`: `--format text|json|github`.
- `lint-changed`: `--changed --base <rev>` lints only what the edits can affect.

## How to get to it (user POV)

- `gcd lint`, or `gcd lint <sys>`, in the repo.
- PR CI (`config.yaml`) runs it with `--changed` and posts `::error` annotations.

## Driving it with verify-gcd

Preconditions:

- Fresh launch (or `$V reset`). `$V doctor` is ok.

- **Clean.** Run `$V run lint-clean lint --no-progress`. Result: exit 0, empty stdout.
- **Schema error.** Run `$V edit bad-replicas systems/payments/apps/my-app/app.yml 's/^replicas: 1/replicas: 101/'`, then `$V run lint-bad lint payments --no-progress`. Result: exit 1, stdout `systems/payments/apps/my-app/app.yml:5:1: replicas: expected an integer from 0 to 100`.
- **Formats.** Run `$V run lint-json lint payments --format json --no-progress`. Result: an array of `{file,line,col,label,msg}`. Run `$V run lint-gh lint payments --format github --no-progress`. Result: `::error file=…,line=5,col=1::replicas: …`.
- **Cross-file.** Fix replicas with `$V edit fix systems/payments/apps/my-app/app.yml 's/^replicas: 101/replicas: 1/'`. Break envFrom with `$V edit bad-envfrom systems/payments/apps/my-app/app.yml 's/envFrom: \[launchdarkly\]/envFrom: [nope]/'`. Run `$V run lint-envfrom lint payments --no-progress`. Result: exit 1 and two lines, one per dev target, `…: target <t> has no environment-variables/nope.yml`.
- **Changed scope.** Run `$V reset`, edit one payments file, then `$V run lint-changed lint --changed --base @- --no-progress`. Result: stderr `lint: changes since <id>: payments`, so search is not in scope.
- **Proof.** The `.stdout` and `.exit` files for each label, plus the `.diff` for each edit.

## Gotchas

- Schema errors mask cross-file errors in the same system. Prove them one at a time.
- `--changed` without `--base` in the sandbox uses jj `trunk()`, which has no remote there. Always pass `--base @-`.
- `GITHUB_ACTIONS=true` in the env switches the default format to github and changes the scope. Don't set it unless you are proving CI behavior.
