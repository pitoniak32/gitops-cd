# Per-stage promoters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One `capabilities/<cap>/owners.yml` with `codeOwners`, per-stage `promoters`, `viewers`, `breakGlass` and an `apps:` override map; one Kargo promote SA+Role per (app, stage).

**Architecture:** Only the CUE renderer changes (`platform/render/`). Task 1 replaces the owners schema/merge and keeps the RBAC output identical: a temporary per-app union of all stages' promoters, so the golden output must not change. Task 2 splits `promote-<app>` into `promote-<app>-<stage>`. Task 3 updates the docs.

**Tech Stack:** CUE, bash test harness (`platform/render/scripts/test.sh`), yq, jj.

**Spec:** `docs/adr/0023-per-stage-promoters.md`

## Global Constraints

- Teams never write CUE. User-facing messages come from `error("...")` placed on the owners.yml field, so `mise run lint` prints `capabilities/<cap>/owners.yml:L:C: ...`.
- Watch for CUE field shadowing (`x: x`). Use `_`-prefixed aliases.
- Emails must match `#Email` (lowercase `@example.com`). Teams must be names from `teams/`.
- An empty `resourceNames` grants every Stage: never render a promote Role with no Stages.
- `rendered/` is never edited. Golden is updated only with `UPDATE=1 mise run test`, and the diff is reviewed before accepting.
- VCS is jj: `jj describe -m ... && jj new`. No git mutations, no push.
- Run `mise run schemas` after changing `#Owners`, and commit `platform/render/schemas/owners.json`.
- Run `mise run codeowners` and commit `.github/CODEOWNERS` if it changes.

## Review Focus

1. **App overrides only some stages:** stages it leaves out must inherit the capability's list, not end up with no promoters. Test: `promote-worker-staging` claims (Task 2).
2. **Shared control-flow Stage (`my-app-dev-done`):** it must land in the Role of every stage that consumes it, and the union of all `promote-my-app-*` resourceNames must equal the old `promote-my-app` list (Task 2).
3. **Old-shape files** (`owners:` key, list-form `promoters`, `apps/<app>/owners.yml`): lint must say how to migrate, not print a raw CUE conflict (lint cases, Task 1).
4. **A stage key that exists in `stages/` but no app uses it:** allowed, and no Role is rendered for it (Task 2 assertion: no `promote-*-<unused stage>`).
5. **Capability with no apps:** `_allEmails`/CODEOWNERS fallback still uses the capability fields (existing `len(_effective) == 0` branch, kept in Task 1).

---

### Task 1: New owners.yml schema, merge and validation (RBAC output unchanged)

**Files:**
- Modify: `platform/render/owners.cue` (whole file: `#Owners`, validation, `_effective`, exports)
- Modify: `platform/render/input.cue:26-34` (drop `appOwners` route; add legacy-file error)
- Modify: `platform/render/files.cue:38-43` (uses renamed `codeOwners`)
- Modify: `platform/render/delivery.cue:48,268` (read the new exports)
- Modify fixtures: `platform/render/testdata/root/capabilities/payments/owners.yml`, `.../search/owners.yml`; delete `.../payments/apps/worker/owners.yml`
- Modify: `platform/render/testdata/lint-cases.tsv` (owners rows)
- Modify: `platform/render/scripts/test.sh:181-190` ("override-all owners" case)
- Modify real data: `capabilities/payments/owners.yml`, `capabilities/search/owners.yml`
- Regenerate: `platform/render/schemas/owners.json`

**Interfaces:**
- Produces, used by Task 2 and the existing consumers (all keyed by app name `an`):
  - `codeOwners: [an]: [...team]`: renamed from `owners` (used in files.cue).
  - `_promoters: [an]: [stage]: [...email]`: merged and sorted, only for stages in `apps[an].stages`.
  - `_breakGlass: [...email]`: capability-wide, sorted. **No longer keyed by app**: update `delivery.cue:48` to `json.Marshal(_breakGlass)`.
  - `_canPromote: [an]: [...email]`: TEMPORARY. Sorted union of every stage in `_promoters[an]` + `_breakGlass`. Task 2 deletes it.
  - `_allOwners`, `_allEmails`: same meaning as before. `_allEmails` covers capability promoters (all stages), app-override promoters, viewers and breakGlass.

New fixture `testdata/root/capabilities/payments/owners.yml` (worker's union stays `{alice, doug}`, so the golden output is unchanged):

```yaml
# yaml-language-server: $schema=../../render/schemas/owners.json
codeOwners: [team1]
promoters:
  dev: [doug@example.com]
  staging: [doug@example.com]
  perf: [doug@example.com]
  prod: [doug@example.com]
viewers: [bob@example.com]
breakGlass: [carol@example.com]
apps:
  worker:
    codeOwners: [team1, team2]
    promoters:
      dev: [alice@example.com, doug@example.com]
      prod: [alice@example.com]
```

`testdata/root/capabilities/search/owners.yml`: `codeOwners: [team2]`, `promoters: {dev: [alice@example.com]}`.

Error messages (exact text, each placed on the named field):

| Condition | Field | Message |
|---|---|---|
| no `codeOwners` | `codeOwners` | `capability owners.yml needs codeOwners: [team, ..]` |
| no `promoters` | `promoters` | `capability owners.yml needs promoters: {<stage>: [email, ..]}` |
| `owners:` key present | `owners` | `renamed to codeOwners` |
| `promoters` is a list | `promoters` | `expected a map: <stage>: [email, ..]` |
| stage key not in `stages/` | `promoters.<k>` (or `apps.<a>.promoters.<k>`) | `unknown stage; allowed: <sorted stage names, ", ">` |
| app key not in `apps/` | `apps.<k>` | `unknown app; allowed: <sorted app names, ", ">` |
| an app uses stage S with no promoter after the merge | `promoters` | `app <an> uses stage <S>: needs promoters.<S> (or apps.<an>.promoters.<S>)` |
| empty list (`codeOwners`, any stage's promoters) | that field | `needs at least one <team\|email>` (existing wording) |
| repeat in a list | that field | `<team\|email>s must not repeat` (existing wording) |
| `breakGlass` under `apps.<a>` | `apps.<a>.breakGlass` | `breakGlass is capability-wide; set it at the top level` |
| file `apps/<a>/owners.yml` exists | via `legacyAppOwners: (a)` like `legacyTargets` | `move into capabilities/<cap>/owners.yml apps.<a>` |

`inputs.sh` must keep passing `apps/*/owners.yml` so the legacy error fires. Check this and keep it if it's already there.

- [ ] **Step 1: Write the failing lint cases.** Replace the owners rows in `lint-cases.tsv` (lines with `owners.yml`) with rows against the new fixture. Format: `<file>\t<yq -i expr>\t<ERE>`. File = `capabilities/payments/owners.yml`, ERE prefix `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: `.

  | yq expr | ERE tail |
  |---|---|
  | `.codeOwners = ["nobody"]` | `.*unknown team; allowed: team1, team2` |
  | `.apps.worker.codeOwners = []` | `.*needs at least one team` |
  | `.apps.worker.codeOwners = ["team1", "team1"]` | `.*teams must not repeat` |
  | `.promoters.dev = ["doug@evil.com"]` | `promoters\.dev\.0: .*expected a lowercase @example\.com email` |
  | `.viewers = ["Bob@example.com"]` | `viewers\.0: .*expected a lowercase @example\.com email` |
  | `del(.promoters)` | `.*needs promoters` |
  | `.promoters = ["doug@example.com"]` | `.*expected a map` |
  | `.owners = .codeOwners \| del(.codeOwners)` | `.*renamed to codeOwners` |
  | `.promoters.qa = ["doug@example.com"]` | `.*unknown stage; allowed: dev, perf, prod, staging` |
  | `.apps.nope = {}` | `.*unknown app; allowed: infra, my-app, payments, worker` |
  | `del(.promoters.staging)` | `.*app my-app uses stage staging: needs promoters\.staging` |
  | `.promoters.dev = []` | `.*needs at least one email` |
  | `.apps.worker.promoters.dev = ["doug@example.com", "doug@example.com"]` | `.*emails must not repeat` |
  | `.apps.worker.breakGlass = ["carol@example.com"]` | `.*breakGlass is capability-wide` |
  | `.breakGlass = ["Oncall@example.com"]` | `breakGlass\.0: .*expected a lowercase @example\.com email` |
  | `.breakGlass = ["carol@example.com", "carol@example.com"]` | `.*emails must not repeat` |

  Add the legacy-file case to `test.sh` next to the "override-all owners" block (the TSV can't create files): copy root, `printf 'owners: [team1]\n' > $r/capabilities/payments/apps/worker/owners.yml`, run lint, then `grep -qE 'apps/worker/owners\.yml:[0-9]+:[0-9]+: .*move into capabilities/payments/owners\.yml apps\.worker'`. If lint can't locate the error in a file it never reads, accept `owners\.yml.*move into` and note it.

  Rewrite the "override-all owners" block in `test.sh:181-190` to the new shape: search `owners.yml` = `codeOwners: [team1]`, `promoters: {dev: [carol@example.com]}`, `apps: {api: {codeOwners: [team2], promoters: {dev: [dave@example.com]}}}`. Leave its claims assertion on `promote-api` for now (Task 2 renames it). The CODEOWNERS assertion `/capabilities/search/ @acme/team2` is unchanged.

- [ ] **Step 2: Update the fixtures** (above) and delete `testdata/root/capabilities/payments/apps/worker/owners.yml`. Run `mise run test`. Expected: FAIL (old schema rejects `codeOwners`).

- [ ] **Step 3: Implement `#Owners`, validation and exports in `owners.cue`, and the `input.cue`/`files.cue`/`delivery.cue` edits.** Validation reads the raw YAML (`_capOwnerFiles[0]`), as the current code does, to avoid cycles. Merge: `_promoters[an][sn]` = `apps[an].promoters[sn]` if set, else capability `promoters[sn]`, for each `sn` in `apps[an].stages`. Keep `if generic` editor-schema constraints (`minItems`, `uniqueItems`) on every list, including map values and `apps.<a>.*`.

- [ ] **Step 4: Run `mise run test`.** Expected: all pass, **golden unchanged** (`golden ok`), `codeowners ok`, every new `lint case ok`. Any golden diff means the merge is wrong: fix it, don't run `UPDATE=1`.

- [ ] **Step 5: Migrate the real files.** Rewrite `capabilities/payments/owners.yml` and `capabilities/search/owners.yml` to the new shape: every stage the capability's apps use gets that capability's current promoters, and the `$schema` comment stays. Run `mise run schemas`, `mise run lint`, `mise run render` (no errors), and `mise run codeowners` (expect no `.github/CODEOWNERS` diff).

- [ ] **Step 6: Commit.**
```bash
jj describe -m "feat(render): single owners.yml with codeOwners, per-stage promoters, apps overrides (ADR 0023)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" && jj new
```

### Task 2: Per-stage promote roles

**Files:**
- Modify: `platform/render/delivery.cue:253-297` (`_kargoRBAC`)
- Modify: `platform/render/owners.cue` (delete `_canPromote`)
- Modify: `platform/render/scripts/test.sh:33-37,74-75` and the "override-all owners" block
- Update: `platform/render/testdata/golden/` (`UPDATE=1`)

**Interfaces:**
- Consumes: `_promoters[an][sn]`, `_breakGlass` (Task 1); `_graph[an][n]` and `_gp[an].grp` (`graph.cue`).
- Produces: `_nodeStages: [an]: [node]: [...stage]`, the capability stages a Kargo Stage belongs to:
  - `kind: "target"` or entry control-flow (`n == "\(an)-\(x.stage)"`): `[x.stage]`.
  - shared control-flow (`n == g.name` for a `g` in `_gp[an].grp` with `g.shared`): `g.mem` (its consumer stages).

Rendering per app `an` and each `sn` in `_promoters[an]`, if the Stage list is non-empty: ServiceAccount/Role/RoleBinding `promote-<an>-<sn>`.
- Claims: `{email: sorted unique(_promoters[an][sn] + _breakGlass)}`.
- Rule 0: `promote` on `stages`, with `resourceNames` = sorted `[n for n in _graph[an] if list.Contains(_nodeStages[an][n], sn)]`.
- Rules 1–2: the same `promotions` and `freights` rules as today. Update the header comment to describe per-stage roles.

- [ ] **Step 1: Write the failing assertions in `test.sh`** (replace lines 34-37 and 74-75; `$k` = rendered `kargo/payments.yaml`):
  - `promote-worker-dev` claims == `{"email":["alice@example.com","carol@example.com","doug@example.com"]}`
  - `promote-worker-staging` claims == `{"email":["carol@example.com","doug@example.com"]}` (inherited)
  - `promote-worker-prod` claims == `{"email":["alice@example.com","carol@example.com"]}`
  - `promote-worker-dev` `.rules[0].resourceNames | join(",")` == `worker-us-dev`; `-staging` == `worker-us-stg`; `-prod` == `worker-us-prod`
  - no Role `promote-worker-perf` (worker doesn't use perf), and no Role named `promote-worker` or `promote-my-app`
  - `my-app-dev-done` is in the resourceNames of both `promote-my-app-staging` and `promote-my-app-perf`
  - the sorted unique union of resourceNames over `promote-my-app-*` == `my-app-dev,my-app-dev-done,my-app-onprem-dev,my-app-onprem-prod,my-app-onprem-stg,my-app-prod,my-app-staging,my-app-us-dev,my-app-us-perf,my-app-us-prod,my-app-us-stg` (the old list)
  - every Role whose name starts with `promote-` creates `promotions` (generalize line 34)
  - "override-all owners" block: `promote-api-dev` claims == `{"email":["dave@example.com"]}`

- [ ] **Step 2: Run `mise run test`.** Expected: FAIL at `golden ok` (or the new assertions).

- [ ] **Step 3: Implement `_nodeStages` and the per-stage `_kargoRBAC`; delete `_canPromote`.**

- [ ] **Step 4: Review the golden diff, then accept it.** Run `cd platform/render && bash scripts/test.sh`. The diff should show only `kargo/*.yaml`: each `promote-<app>` SA/Role/RoleBinding replaced by `promote-<app>-<stage>`, and nothing else. Then run `UPDATE=1 mise run test` followed by `mise run test`. Expected: all pass.

- [ ] **Step 5: Run `mise run lint` and `mise run render`** on the real capabilities. Expected: no errors.

- [ ] **Step 6: Commit.**
```bash
jj describe -m "feat(render): promote-<app>-<stage> Kargo roles from per-stage promoters (ADR 0023)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" && jj new
```

### Task 3: Docs

**Files:** `docs/adr/0023-per-stage-promoters.md` (Status → accepted), `docs/adr/0011-access-by-email.md` and `docs/adr/0013-break-glass-approval.md` (one line at the top: "Amended by ADR 0023"), `CLAUDE.md` (Layout `owners.yml` bullet + GitOps-only rule: `promote-<app>-<stage>`, `codeOwners`, `apps:` map, no app owners files), `GLOSSARY.md`, `README.md`, `platform/render/README.md`, `docs/operations.md`, `docs/break-glass.md`, `docs/gotchas.md`, `docs/sync-boundaries.md`.

- [ ] **Step 1: Find stale references.** `grep -rnE "promote-<app>[^-]|apps/<app>/owners|owners\.yml.*owners:|\bowners: \[|app owners file" CLAUDE.md GLOSSARY.md README.md docs platform/render/README.md`. Fix each hit to match the ADR. Don't rewrite history in other ADRs beyond the "Amended by" line.
- [ ] **Step 2: Rerun the grep.** Expected: no hits outside `docs/adr/0011`, `0013`, `0023`, `docs/superpowers/`.
- [ ] **Step 3: Run `mise run test`** (docs don't affect it; sanity check). Expected: pass.
- [ ] **Step 4: Commit.**
```bash
jj describe -m "docs: per-stage promoters, codeOwners, single owners.yml (ADR 0023)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" && jj new
```
