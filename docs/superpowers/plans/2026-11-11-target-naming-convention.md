# Target naming convention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Targets carry `environmentType` (`dev|npr|prd`) and a region from a fixed list. Kind clusters become `us-east4` and `dc1`, and every target is named `<region>-<env>-<platform>[-<qualifier>]`, enforced by lint.

**Architecture:** Changes are in the CUE renderer (`platform/render/`) and the region-derived config (`mise run regions`). There's no Go change: the CLI reads regions from `platform/cluster/regions`. Task 1 adds the fields, region lists and cluster rename. Task 2 adds the name rule and renames every target. Task 3 updates the docs. Each task migrates the real `systems/*` targets, because CI lints them.

**Tech Stack:** CUE, bash test harness, yq, jj.

**Spec:** `docs/adr/0025-target-naming-convention.md`

## Global Constraints

- `environment` stays a free name (`#Name`). `environmentType` is required: `"dev" | "npr" | "prd"`. Mapping for existing targets: `dev`→`dev`, `staging`/`perf`→`npr`, `prod`→`prd`.
- `#GcpRegion: "us-east1" | "us-east4" | "us-central1"`. `#DatacenterRegion: "dc1" | "dc2"`. A cluster `region` is one of either.
- Clusters: key and region `us` → `us-east4`, `onprem` → `dc1`, each with `environmentTypes: ["dev", "npr", "prd"]` (renamed from `environments`).
- `policy` is keyed by `environmentType`: `dev` 0, `npr` 1, `prd` 1.
- The prod autoPromote rule triggers on `environmentType == "prd"`.
- Name: `<region>-<environmentType>-<k8s|tf|cr>` with an optional `-[a-z0-9]+` qualifier. Region comes from `clusters[cluster].region` (K8S) or `config.region` (TERRAFORM).
- Namespaces and `environment` values keep their current values.
- Teams never write CUE. Messages come from `error("...")` placed on a YAML field, so lint gives `file:line:col`.
- Watch for CUE shadowing.
- Golden is updated only with `UPDATE=1`, after reviewing the diff.
- `mise run schemas` after schema changes. `mise run regions` after `clusters.cue` changes. `cue fmt ./platform/render/...` before every commit, since CI runs `cue fmt --check`.
- VCS is jj: `jj describe -m "<msg>\n\nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" && jj new`. No git mutations, no push.

## Review Focus

1. **A typo'd region on a TERRAFORM target** must give a located `unknown region; allowed: us-central1, us-east1, us-east4`, not a raw CUE conflict (Task 1 lint case).
2. **A target on a cluster that doesn't accept its `environmentType`**: the message names the cluster's `environmentTypes` (Task 1 lint case).
3. **The qualifier case**: `us-east4-npr-k8s-perf` must pass, and `us-east4-npr-k8sperf` and `us-east4-npr-k8s-` must fail (Task 2 lint cases).
4. **A name whose region doesn't match its cluster's region** (e.g. `dc1-dev-k8s` on cluster `us-east4`) must say which prefix was expected (Task 2 lint case).
5. **`test.sh` synth blocks that create targets on the fly** (copies to `*-qa`) must use convention names, or they'll fail on the name rule rather than on what they test (Task 2).

---

### Task 1: environmentType, region lists, cluster rename

**Files:**
- Modify: `platform/render/clusters.cue` (`#EnvironmentType`, `#GcpRegion`, `#DatacenterRegion`, `#Cluster.region` + `environmentTypes`, cluster keys, `policy`)
- Modify: `platform/render/team.cue:29-75` (`#Target.config`: `environment: #Name`, `environmentType`, `region` on TERRAFORM, cluster environment-type check, `_prodStages`)
- Modify: `platform/render/app.cue:142` (minReplicas policy by `environmentType`, message `"\(type) requires replicas >= \(n)"`)
- Modify: `platform/render/k8s.cue:27` (add label `"example.com/environment-type": d.tgt.environmentType`)
- Modify: `platform/render/terraform.cue` (add `region: d.tgt.region` after `gcpProject`)
- Modify: `platform/render/pipeline.cue:40` (add `environmentType` next to `environment`)
- Modify: every `target.yml` under `platform/render/testdata/root/` and `systems/` (`cluster:` renamed, `environmentType:` added; TERRAFORM targets get `region: us-east4`)
- Modify: `platform/render/scripts/test.sh` (region names `us`→`us-east4` and `onprem`→`dc1` in paths, shards and regions JSON assertions; target names unchanged in this task), `testdata/lint-cases.tsv`
- Regenerate: `platform/render/schemas/`, `mise run regions` outputs (`platform/cluster/regions`, Keycloak realm, Kargo `api.argocd.urls`), golden

**Interfaces:**
- Produces, for Task 2: `#GcpRegion`, `#DatacenterRegion`, `clusters[c].region`, `targets[t].config.{environmentType, region?}`, and `_platformSuffix: {K8S: "k8s", TERRAFORM: "tf"}` in `team.cue` (Task 2 uses it, and ADR 0024 adds `CLOUD_RUN: "cr"`).

Error messages:

| Condition | Field | Message |
|---|---|---|
| missing `environmentType` | `config.environmentType` | `expected dev, npr or prd` |
| TERRAFORM `region` not in list | `config.region` | `unknown region; allowed: us-central1, us-east1, us-east4` |
| TERRAFORM without `region` | `config.region` | required (CUE `!` field; reuse the `gcpProject!` style) |
| `region` on K8S | `config.region` | `region not allowed on a K8S target (it's the cluster's)` |
| cluster doesn't accept type | `config.environmentType` | `cluster <c> accepts environment types: dev, npr, prd` |
| replicas below policy | existing field | `<type> requires replicas >= <n>` |

- [ ] **Step 1: Write the failing lint cases** in `lint-cases.tsv` (file = a payments fixture `target.yml`, ERE prefix `^systems/payments/targets/<t>/target\.yml:[0-9]+:[0-9]+: `): `del(.config.environmentType)`, `.config.environmentType = "staging"`, `.config.region = "us-east9"` (TERRAFORM fixture), `.config.region = "us-east4"` (K8S fixture). Update any existing case that relied on `environment: prod` semantics (replicas policy, autoPromote rule) so it triggers through `environmentType`.
- [ ] **Step 2: Migrate the fixtures and real targets.** Rename `cluster:`, add `environmentType` per the mapping, and add `region: us-east4` to TERRAFORM targets. In `test.sh`, update the region-derived assertions (lines ~11-13, 16, 24, 86-97: `regions/us-east4.json` = `{"region":"us-east4"}`, `ls` = `dc1.json us-east4.json`, `projects/dc1/.keep`, `deploys/<r>/`, shard values `us-east4`/`dc1`). Run `mise run test`. Expected: FAIL.
- [ ] **Step 3: Implement the CUE changes** listed under Files.
- [ ] **Step 4: Run `mise run regions`, `mise run schemas`, `cue fmt ./platform/render/...`.** Then run `cd platform/render && bash scripts/test.sh` and read the golden diff. Expected changes only: region dir names and region JSONs, shard values, cluster names in params, the new `environment-type` label, `environmentType` in pipeline JSON, and `region` in `terraform.yaml`. Then run `UPDATE=1 mise run test` followed by `mise run test`. Expected: pass.
- [ ] **Step 5: Run `mise run lint`, `mise run render` and `mise run codeowners`** (expect no CODEOWNERS drift), and confirm `cue fmt --check ./platform/render/...` exits 0.
- [ ] **Step 6: Commit** with the message `feat(render): environmentType, region lists, kind clusters us-east4/dc1 (ADR 0025)`.

### Task 2: Name rule + rename every target

**Files:**
- Modify: `platform/render/team.cue` (name check)
- Rename dirs (`jj` tracks the moves; use `mv`) under `platform/render/testdata/root/systems/*/targets/` and `systems/*/targets/`, using the tables below
- Modify: every `app.yml` that references a target key (fixtures + real), plus `test.sh` (all target-derived names: Stage names, branches, params paths, appName, resourceNames, synth blocks), `lint-cases.tsv` (file paths)
- Regenerate: golden

Fixture renames (`platform/render/testdata/root/systems/`):

| old | new |
|---|---|
| payments/us-dev | us-east4-dev-k8s |
| payments/us-stg | us-east4-npr-k8s |
| payments/us-perf | us-east4-npr-k8s-perf |
| payments/us-prod | us-east4-prd-k8s |
| payments/onprem-dev | dc1-dev-k8s |
| payments/onprem-stg | dc1-npr-k8s |
| payments/onprem-prod | dc1-prd-k8s |
| payments/tf-dev | us-east4-dev-tf |
| payments/tf-prod | us-east4-prd-tf |
| search/us-dev | us-east4-dev-k8s |

Real renames: the ADR 0025 table (`systems/`).

**Interfaces:**
- Consumes: Task 1's `clusters[c].region`, `config.region`, `config.environmentType`, `_platformSuffix`.

Rule: for target `tn` with `p = "\(region)-\(environmentType)-\(_platformSuffix[platform])"`, `tn` must equal `p` or match `^` + `p` + `-[a-z0-9]+$`. Skip the check when the region can't be resolved (unknown cluster: that already errors). Otherwise place the error on the target's `config.platform` field with the message `name must be <region>-<env>-<platform>[-<qualifier>]: expected \(p)[-<qualifier>]`.

- [ ] **Step 1: Write the failing lint cases.** The harness only edits file contents, so put dir-rename cases in `test.sh` next to the "bad name" loop. Copy root, `mv` a target dir, then lint and grep for the located message. Cases:
  - `us-east4-npr-k8s` → `dc1-npr-k8s-x`: expects `expected us-east4-npr-k8s`
  - → `us-east4-npr-k8sperf`: fails
  - → `us-east4-npr-k8s-`: fails (also an invalid `#Name`; either message is OK)
  - → `us-east4-npr-k8s-blue`: passes lint
  - Edit-based cases in `lint-cases.tsv`: `.config.environmentType = "dev"` on `us-east4-npr-k8s/target.yml` → `expected us-east4-dev-k8s`
- [ ] **Step 2: Rename the dirs and references** (fixtures, then real), and rewrite `test.sh` names. Synth blocks that create targets must use convention names, e.g. `us-east4-npr-k8s-qa` and `dc1-npr-k8s-qa` instead of `us-qa`/`onprem-qa`. For the "target/stage name collision" test, rename the colliding target or stage so the collision is still what's under test (e.g. a stage named `us-east4-dev-k8s`). Run `mise run test`. Expected: FAIL only on the new rule's cases.
- [ ] **Step 3: Implement the name check in `team.cue`.**
- [ ] **Step 4: Review the golden diff, then accept it.** Expected changes: target-derived names only (Stage names, branches, Application names, params file names, resourceNames). Namespaces don't change. Then run `UPDATE=1 mise run test` followed by `mise run test`. Expected: pass.
- [ ] **Step 5: Run `mise run lint`, `mise run render`, `mise run codeowners` and `cue fmt --check ./platform/render/...`.** Expected: clean, no drift.
- [ ] **Step 6: Commit** with the message `feat(render): enforce <region>-<env>-<platform> target names, rename all targets (ADR 0025)`.

### Task 3: Docs + rollout steps

**Files:** `docs/adr/0025-target-naming-convention.md` (Status → accepted), `docs/adr/0015-multi-region-kargo-shards.md` (one line: "Amended by ADR 0025: regions renamed us→us-east4, onprem→dc1"), `README.md` (URL table `argocd-us-east4.localhost`, `argocd-dc1.localhost`; cluster names; target examples), `CLAUDE.md` (Layout target bullet: `environmentType`, name rule), `GLOSSARY.md` (Environment, Environment type, Region, Target name), `docs/gotchas.md`, `docs/operations.md` (a new "Rename targets / regions" runbook: `mise run down && mise run up`, then delete old `env/*` branches with `gh api -X DELETE repos/<owner>/<repo>/git/refs/heads/env/<sys>/<app>/<old-target>`, listed by `gh api repos/<owner>/<repo>/git/matching-refs/heads/env/`).

- [ ] **Step 1: Find stale references:** `grep -rnE 'argocd-(us|onprem)\b|\bonprem\b|cluster `us`|us-stg|us-dev\b|onprem-|environment: (staging|perf|prod)' README.md CLAUDE.md GLOSSARY.md docs --exclude-dir=superpowers`. Fix each hit outside ADR history (besides the "Amended by" line).
- [ ] **Step 2: Rerun the grep.** Expected: hits only in `docs/adr/` history.
- [ ] **Step 3: Run `mise run test`.** Expected: pass.
- [ ] **Step 4: Commit** with the message `docs: target naming convention, regions us-east4/dc1, rename runbook (ADR 0025)`.
