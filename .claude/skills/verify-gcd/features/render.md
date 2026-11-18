# Render

`gcd render` writes every system's manifests, the platform files and the CODEOWNERS fragments into `rendered/` (or `--out`), and regenerates `.github/CODEOWNERS`. CI pushes this tree to the `rendered` branch, which Argo CD and Kargo read.

## Sub-features

- `render-all`: full render. Prints `rendered <sys>` per system, then `rendered platform (regions, promotion tasks)`.
- `render-tree`: `apps/<sys>/<app>/<stage>/<target>/*`, `argocd/{deploys,projects}`, `kargo`, `pipelines`, `infrastructure/regions`, `codeowners`, `README.md`.
- `render-deterministic`: re-rendering unchanged inputs gives a byte-identical tree.
- `render-one`: `gcd render <sys>` renders only that system: no wipe of the others, no platform files, no CODEOWNERS.
- `render-codeowners`: `.github/CODEOWNERS` regenerated from `owners.yml` `codeOwners` + `teams/`.

## How to get to it (user POV)

- `gcd render` locally. CI on every push to main.

## Driving it with verify-gcd

Preconditions:

- Fresh launch. The baseline `rendered/` and `.github/CODEOWNERS` are committed at `@-`.

- **Deterministic full render.** Run `$V run render-all render --no-progress`, then `$V sh render-same 'jj diff --summary'`. Result: render exit 0 with three `rendered …` lines, and empty jj diff output.
- **Tree.** Run `$V sh render-tree 'find rendered -maxdepth 2 -type d | sort'`. Result: the directories listed under `render-tree`.
- **Edit propagates.** Run `$V edit r systems/payments/apps/my-app/app.yml 's/^replicas: 1/replicas: 4/'`, `$V run render-edit render --no-progress`, then `$V sh render-diff 'jj diff --summary; grep -n "replicas:" rendered/apps/payments/my-app/prod/us-east4-prd-k8s/deployment.yaml'`. Result: `M` for the 4 deployment.yaml files (plus the app.yml) and `replicas: 4`.
- **CODEOWNERS.** Run `$V edit co systems/search/owners.yml 's/codeOwners: \[team2\]/codeOwners: [team1]/'`, `$V run render-co render --no-progress`, then `$V sh co 'jj diff --git -- .github/CODEOWNERS'`. Result: the `/systems/search/` lines switch to `@acme/team1`.
- **Single system.** Run `$V run render-one render search --no-progress`. Result: exactly one `rendered search` line, and no platform line.

## Gotchas

- `--out` is relative to cwd. `$V run` uses cwd = sandbox. Never run a bare `gcd render` with `--root` from the repo root, because it overwrites the real `rendered/`.
- Render refuses on lint errors (exit 1, `Error: render: N problem(s)`).
