---
name: capability-config
description: Change capability config. Use when editing any YAML under capabilities/ or teams/ (apps, stages, targets, env vars, owners, replicas, resources, promoters).
---

# Capability config

Run `gcd` from repo root (`mise run cli` builds `.bin/gcd`). Teams write YAML only; `gcd lint` is the only validator.

## Workflow

1. **Find the value**: `gcd show -s <cap>[/<app>] [--stage s] [--target t]` (YAML). `-s` comments each value with `file:line` (relative to `capabilities/<cap>/`) where it is set, or a note (`platform default (<P>)`, `global default`, `forced by …`). The `owners` block shows effective owners.
2. **Check allowed fields**: `gcd schema <kind>` (kinds: owners, stage, app, target, env-vars). Prints a YAML skeleton: placeholders (`<int>`, `a|b`), comments for required, pattern, platforms, description. `-s` adds `(e.g. file:line)` of a real use. `--json` for machines.
3. **Edit** the YAML at the narrowest layer that matches the request (below).
4. **Lint until clean**: `gcd lint <cap>` (`--format json` for machine-readable diags). If `gcd show` errors, run `gcd lint <cap>` first. Output is `file:line:col: message`; fix each, re-run.
5. **Review rendered effect**: `gcd diff --stat`, then `gcd diff --capability <cap>` for detail. Default base: jj `@-` (parent of the working copy), git `HEAD`. Once committed the default diff is empty: use `--base @--`/`trunk()` (jj) or `HEAD~1`/`main` (git). Both sides use the current gcd, so it shows input changes only.
   "Effective"/"new" manifest = rendered output: `gcd show <cap>/<app> [--stage s] [--target t] --diff` (diff where changed, full manifest where not; `--base` as above). Add `--include-platform` only if asked about Argo CD/Kargo.
6. **Owners or `teams/` changed**: `mise run codeowners`, commit the regenerated `.github/CODEOWNERS`.

## Narrowest layer

Pick the smallest scope that covers the request; `gcd show -s` source comments reveal where a value lives today.
- one target of one app: `stages.<stage>.targets.<target>` in `app.yml`
- one stage of one app: `stages.<stage>` in `app.yml`
- one app: top level of `app.yml`
- every app on one target: `targets/<t>/target.yml`; `environment-variables/<name>.yml` reaches only apps that opt in with `envFrom: [<name>]`
- pipeline shape (order, autoPromote): `stages/<stage>.yml`; access: `owners.yml` (`apps:` for one app)

If a value is overridden at a narrower layer, editing a wider one changes nothing; `gcd show` after the edit confirms the effective value.

## Rules

- Edit only `capabilities/` and `teams/`. `rendered/` and `.github/CODEOWNERS` are generated.
- `owners.yml` and `teams/*.yml` changes need platform approval: say so in the PR description.
- Target dir names follow `<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]` (ADR 0025, lint-enforced). Owners model and `apps:` overrides: ADR 0007.
- `owners.yml` `apps.<app>.codeOwners|viewers|promoters.<stage>` REPLACE the capability-wide list for that key (promoters per stage). To add one person, copy the current entries from `gcd show <cap>/<app>` (`owners` block), then add. `promoters` keys are stage names (`prod`), not environmentType (`prd`).
- New target is inert until an app uses it: copy a sibling `targets/<t>/` dir (`gcd schema target` for required and per-platform fields), then add `<t>` under the app's `stages.<stage>.targets`.
- Cap and app names are folder names.

## Examples and sandbox

Reference capabilities `payments` and `search` live in `platform/render/testdata/root`. Every `gcd schema` field cites an example line there. Try changes without touching real config: add `--root platform/render/testdata/root` to `show`, `lint`, `render`. `schema` and `diff` `--root` is a repo root (jj or git), not the sandbox.

Done when `gcd lint <cap>` prints nothing, `gcd diff --stat` lists only the files you meant to change, and `gcd show` reports the intended value with the `file:line` (`-s`) you edited.
