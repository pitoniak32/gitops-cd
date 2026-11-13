---
name: system-config
description: Change system config. Use when editing any YAML under systems/ or teams/ (apps, stages, targets, env vars, owners, replicas, resources, promoters).
---

# System config

Run `gcd` in the repo (`mise run build` builds `.bin/gcd`, on PATH via mise). Teams write YAML only; `gcd lint` is the only validator.

## Workflow

1. **Find the value**: `gcd show -s <sys>[/<app>] [--stage s] [--target t]` (YAML). `-s` comments each value with `file:line` (relative to `systems/<sys>/`) where it is set, or a note (`platform default (<P>)`, `global default`, `forced by …`). The `owners` block shows effective owners.
2. **Check allowed fields**: `gcd schema <kind>` (kinds: owners, stage, app, target, env-vars). Prints a YAML skeleton: placeholders (`<int>`, `a|b`), comments for required, pattern, platforms, description. `-s` adds `(e.g. file:line)` of a real use. `--json` for machines.
3. **Edit** the YAML at the narrowest layer that matches the request (below).
4. **Lint until clean**: `gcd lint <sys>` (`--format json` for machine-readable diags). If `gcd show` errors, run `gcd lint <sys>` first. Output is `file:line:col: message`; fix each, re-run.
5. **Review rendered effect**: `gcd show --diff --stat` (every changed system), then `gcd show <sys>[/<app>] [--stage s] [--target t] --diff` for detail (changed files only; empty = no change; `--rendered` for the full manifest; `--include-platform` only if asked about Argo CD/Kargo). Default base is the branch fork point (jj `fork_point(trunk() | @)`, git merge-base with `origin/main`/`main`), so committed and uncommitted branch work both show; `--base <jj revset|git rev>` overrides. Both sides use the current gcd, so it shows input changes only.
6. **Owners or `teams/` changed**: `gcd codeowners`, commit the regenerated `.github/CODEOWNERS`.

## Narrowest layer

Pick the smallest scope that covers the request; `gcd show -s` source comments reveal where a value lives today.
- one target of one app: `stages.<stage>.targets.<target>` in `app.yml`
- one stage of one app: `stages.<stage>` in `app.yml`
- one app: top level of `app.yml`
- every app on one target: `targets/<t>/target.yml`; `environment-variables/<name>.yml` reaches only apps that opt in with `envFrom: [<name>]`
- pipeline shape (order, autoPromote; default manual): `stages/<stage>.yml`; one app's autoPromote: `app.yml` `stages.<stage>.autoPromote`; access: `owners.yml` (`apps:` for one app)

If a value is overridden at a narrower layer, editing a wider one changes nothing; `gcd show` after the edit confirms the effective value.

## Rules

- Edit only `systems/` and `teams/`. `rendered/` and `.github/CODEOWNERS` are generated.
- `owners.yml` and `teams/*.yml` changes need platform approval: say so in the PR description.
- Target dir names follow `<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]` (ADR 0025, lint-enforced). Owners model and `apps:` overrides: ADR 0007.
- `owners.yml` `apps.<app>.codeOwners|viewers|promoters.<stage>` REPLACE the system-wide list for that key (promoters per stage). To add one person, copy the current entries from `gcd show <sys>/<app>` (`owners` block), then add. `promoters` keys are stage names (`prod`), not environmentType (`prd`).
- New target is inert until an app uses it: copy a sibling `targets/<t>/` dir (`gcd schema target` for required and per-platform fields), then add `<t>` under the app's `stages.<stage>.targets`.
- Cap and app names are folder names.

## Examples and sandbox

Reference systems `payments` and `search` live in `platform/render/testdata/root`. Every `gcd schema` field cites an example line there. Try changes without touching real config: add `--root platform/render/testdata/root` to `show`, `lint`, `render`, `schema` (global flag). `show --diff` and `lint --changed` need the root to be a jj or git repo, so not the sandbox.

Done when `gcd lint <sys>` prints nothing, `gcd show --diff --stat` lists only the files you meant to change, and `gcd show` reports the intended value with the `file:line` (`-s`) you edited.
