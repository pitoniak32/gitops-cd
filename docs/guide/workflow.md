# Working loop

You want to change system YAML and know, before you push, that it's valid and does what you meant.

Run everything as `gcd` inside the repo: `mise install && mise run build` puts it on your `PATH`. You need Go (`mise install`, version
pinned in `.mise.toml`) and git or jj; nothing else for editing YAML
([prerequisites](../../README.md#prerequisites)).
`gcd lint` is the only validator: there are no editor schemas
([ADR 0030](../adr/0030-renderer-in-go.md)).

## 1. Find where a value lives

`gcd show <sys>[/<app>] [--stage s] [--target t] -s` prints the effective, merged config, with a comment
saying where each value came from (paths are relative to `systems/<sys>/`):

```sh
gcd show payments/web --stage dev -s
```

```text
web:
  owners:
    codeOwners: [team1]                                        # owners.yml:1
    promoters:
      dev: [doug@example.com]                                  # owners.yml:3
  dev:
    us-east4-dev-k8s:
      image: ghcr.io/example/web                               # apps/web/app.yml:2
      cluster: us-east4                                        # targets/us-east4-dev-k8s/target.yml:2
      namespace: payments-us-dev                               # targets/us-east4-dev-k8s/target.yml:3
      replicas: 2                                              # global default
      deployWith: argocd                                       # apps/web/app.yml:4
      env:
        GREETING: hello                                        # apps/web/app.yml:6
```

If a value is set at a narrower layer than the one you edit, your edit changes nothing. `-s` tells you
which layer to edit ([configure apps](apps.md#layers)).

## 2. Check which fields exist

`gcd schema <kind>` prints an annotated skeleton of a file kind (`owners`, `stage`, `app`, `target`,
`env-vars`); `-s` adds a real example line for each field. The same information, as tables, is in
[reference/](../reference/).

```sh
gcd schema stage
```

```text
# stage: systems/{sys}/stages/*.yml — a stage of the system's promotion pipeline
dependsOn:           # stages that must be verified before this one
  - <str>
autoPromote: <bool>  # promote automatically once upstream stages are verified
color: red|salmon|orange|…   # Kargo UI color for this stage's Kargo Stages; default picked per stage
```

## 3. Lint until clean

```sh
gcd lint payments          # one system (+ teams/)
gcd lint --changed         # only what your branch can affect
```

No output means valid. Otherwise each problem is one line, `file:line:col: field.path: message`:

```text
systems/payments/stages/dev.yml:3:1: bogus: field not allowed
```

`--format json` gives machine-readable output. Common messages and fixes: [troubleshooting](troubleshooting.md).

## 4. See the rendered effect

```sh
gcd show --diff --stat                        # every system your branch changes, file counts
gcd show payments/web --stage dev --diff      # the manifest diff for one app/stage
gcd show payments/web --stage dev --rendered  # the full rendered manifests
```

The diff base is where your branch forked from `main`, so committed and uncommitted work both show.
An empty diff means your change has no rendered effect, which usually means a narrower layer overrides it.

## 5. Open the PR

- If you changed `owners.yml` or `teams/`, run `gcd codeowners` and commit the regenerated
  `.github/CODEOWNERS`. Say in the PR description that it needs platform approval.
- CI (`config.yaml`) lints the systems your PR can affect and annotates errors on the changed lines,
  checks CODEOWNERS is up to date, renders, and comments the rendered diff on the PR (`_no changes_` if
  there is none).
- Your system's `codeOwners` approve. On merge, CI writes the rendered manifests to the branch
  `rendered`; nothing deploys until someone promotes ([promote and access](promote-access.md)).

## Try things in the sandbox

The reference systems `payments` and `search` live in `platform/render/testdata/root`. Add
`--root platform/render/testdata/root` to `gcd show`, `gcd lint`, `gcd schema` or `gcd render` to
experiment without touching real config. Every example in these guides is checked against that sandbox.
