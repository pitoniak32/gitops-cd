# Show diff

`gcd show --diff` shows how the working tree changes the merged config, or with `--rendered` the rendered manifests, compared with a base revision. `--against <rev>` compares a fresh render with the `rendered/` tree committed at that rev. That is what CI posts as the PR comment.

## Sub-features

- `diff-config`: unified diff of the plain `show` output for each changed system or app.
- `diff-rendered`: per-file manifest diff of the selected deploys. Unchanged files are omitted.
- `diff-stat`: `--stat` gives `path | +a -r` counts and a total line.
- `diff-against`: `--diff --rendered --against <rev>` diffs against the committed `rendered/` (the PR comment).
- `diff-unchanged`: no changes means empty output, exit 0.

## How to get to it (user POV)

- `gcd show --diff`, then `gcd show --diff --rendered --stat` before opening a PR (system-config skill, step 5).
- The PR comment from `config.yaml`.

## Driving it with verify-gcd

Preconditions:

- Fresh launch. The baseline `rendered/` is committed at `@-`.

- **Unchanged.** Run `$V run diff-none show --diff --base @-`. Result: exit 0, empty stdout.
- **Edit.** Run `$V edit replicas systems/payments/apps/my-app/app.yml 's/^replicas: 1/replicas: 4/'`.
- **Config diff.** Run `$V run diff-config show payments/my-app --diff --base @-`. Result: `--- a/payments/my-app`, plus `-replicas: 1 / +replicas: 4` hunks for perf, both prod targets and staging dc1. Dev targets and staging `us-east4-npr-k8s` don't change because they override replicas.
- **Stat (PR comment).** Run `$V run diff-stat show --diff --rendered --against @- --stat`. Result: exactly 4 `apps/payments/my-app/.../deployment.yaml | +1 -1` lines, then `4 files changed, +4 -4`.
- **Rendered detail.** Run `$V run diff-rendered show payments/my-app --stage prod --target us-east4-prd-k8s --diff --rendered --base @-`. Result: a hunk `-  replicas: 1` / `+  replicas: 4` in `rendered/apps/payments/my-app/prod/us-east4-prd-k8s/deployment.yaml`.
- **Proof.** The edit `.diff` and the three diff stdouts.

## Gotchas

- `--against` with a rev that has no `rendered/` reports every file as added. If the stat shows only `+N -0`, the baseline wasn't committed with `rendered/` (old launch bug), so relaunch.
- `--against` excludes `--base`.
- Both sides use the current gcd. A renderer code change by itself shows no diff. To prove a renderer change, compare `render` output against golden or the real `rendered/` branch instead.
