# Show merged config

`gcd show <sys>[/<app>]` prints the effective config of each deploy (`<stage>.<target>` per app), plus the effective owners. `-s` adds a comment to each value naming the `file:line` that sets it, or a default note. `--rendered` prints the manifests the deploy renders to.

## Sub-features

- `show-merged`: YAML per app: an `owners` block, then `<stage>: <target>: …`.
- `show-sources`: `-s` source comments (`# apps/my-app/app.yml:23`, `# global default`).
- `show-narrow`: `--stage` / `--target` filters.
- `show-json`: `--json` gives a `deploys[].values[]` list with `path,value,file,line|note`.
- `show-rendered`: `--rendered [--files app|platform|all]` prints each manifest under a `# rendered/<path>` line.

## How to get to it (user POV)

- `gcd show payments/my-app --stage dev -s` while editing YAML, to see where a value lives.

## Driving it with verify-gcd

Preconditions:

- Fresh launch. The baseline lints clean.

- **Sources.** Run `$V run show-src show payments/my-app --stage dev -s`. Result: exit 0. Under `dc1-dev-k8s`, the line reads `replicas: 2   # apps/my-app/app.yml:23` and `cpu: 100m   # global default`.
- **JSON.** Run `$V run show-json show payments/my-app --stage dev --target dc1-dev-k8s --json`. Result: one deploy. The `replicas` entry has `value: 2`, `file: systems/payments/apps/my-app/app.yml`, `line: 23`.
- **Edit reflected.** Run `$V edit r systems/payments/apps/my-app/app.yml 's/^        replicas: 2/        replicas: 5/'`, then rerun the sources command. Result: `replicas: 5` with the same source line.
- **Rendered.** Run `$V run show-rendered show payments/my-app --stage prod --target us-east4-prd-k8s --rendered`. Result: a `# rendered/apps/payments/my-app/prod/us-east4-prd-k8s/deployment.yaml` header followed by a Deployment. Add `--files platform` and the output shows the Argo CD params and the Kargo Stage instead.
- **Blocked by lint.** Introduce a lint error (see lint.md) and run `$V run show-bad show payments/my-app`. Result: exit 1, with stderr ending `Error: payments: 1 problem(s); fix them first (gcd lint)`.

## Gotchas

- `--json` and `-s` can't be combined with `--rendered` or `--diff`.
- `owners` is a reserved stage name. It is never a stage key in the output.
