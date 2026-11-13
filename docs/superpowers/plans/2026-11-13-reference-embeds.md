# Reference Embeds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hand-written Kargo-style `docs/reference/` pages whose examples `gcd docs` splices from the fixture.

**Architecture:** New `internal/docs/embed.go` rewrites `<!-- embed … -->` blocks (pure function + dir walker). `docs.Run` = rewrite embeds + lint fixture + check annotated fences, over `docs/guide` and `docs/reference`. `gcd docs` calls `Run(root, true)`; `TestRepoDocs` calls `Run(repo, false)`. `SchemaMarkdown` is deleted.

**Tech Stack:** Go (cobra CLI `gcd`, `platform/cli`), markdown.

**Spec:** `docs/superpowers/specs/2026-11-13-reference-embeds-design.md`

## Global Constraints

- VCS is jj: one change (`docs(gcd): hand-written reference with embedded examples`) off `main`; `jj describe`/`jj new` per task, no git mutations, no push until asked.
- Embed marker: `<!-- embed path=<fixture-relative> [lines=A-B] -->` … `<!-- /embed -->`, each alone on its line, no indent.
- `path` relative to `capability.FixtureRoot` (`platform/render/testdata/root`).
- Output between markers, exactly: "```yaml\n" + dedented lines + "```\n\nSource: [`<label>`](<link>)\n" where label = path minus `capabilities/` + `:A-B` (no `:…` for whole file), link = `../`×(slashes in page path) + FixtureRoot + "/" + path + `#LA-LB` (none for whole file). Fence language: `yaml` for `.yml`/`.yaml`, else the extension.
- Embed errors are `capability.Diag{File: page, Line: marker line, Col: 1}`; text inside a broken embed is left unchanged.
- `gcd docs` flags: `--format` only.
- Docs prose: concise, user's style; no "TODO".

## Review Focus

- Embed marker shown inside a code fence (documenting the syntax) must not be rewritten → test `TestEmbedInsideFenceIgnored` (Task 1).
- Source file without trailing newline → fence still closes on its own line → `TestEmbedNoTrailingNewline` (Task 1).
- `lines=5-3` / `lines=0-2` / past EOF → diag, page unchanged → `TestEmbedErrors` (Task 1).
- Fence inside an embed is not flagged as unannotated, but an unannotated fence after `<!-- /embed -->` is → `TestCheckEmbedFenceExempt` (Task 2).
- Root with no `docs/guide` or `docs/reference` (sandbox `--root`) → `gcd docs` succeeds, writes nothing → `TestDocsSandboxRoot` (Task 3).

---

### Task 1: Embed rewriting

**Files:**
- Create: `platform/cli/internal/docs/embed.go`, `platform/cli/internal/docs/embed_test.go`

**Interfaces:**
- Produces:
  - `const ReferenceDir = "docs/reference"`; `var Dirs = []string{GuideDir, ReferenceDir}`
  - `func Embed(page, text string, fixture fs.FS) (string, []capability.Diag)`: rewrite one page (page = repo-relative, e.g. `docs/reference/app.md`).
  - `func Embeds(root string, write bool) (changed []string, diags []capability.Diag, err error)`: every `*.md` in `Dirs` under root; `changed` = repo-relative pages whose text differs; writes them only if `write`. Missing dirs are skipped.
  - `var embedOpenRx, embedCloseRx` (shared with Task 2).

- [ ] **Step 1: Write failing tests** in `embed_test.go` using `os.DirFS(fixture)` (`fixture` const from `check_test.go`):
  - `TestEmbedWholeFile`: page `"x\n<!-- embed path=capabilities/payments/stages/dev.yml -->\nold\n<!-- /embed -->\ny\n"` → output equals `"x\n<!-- embed …-->\n```yaml\n" + fixtureFile(dev.yml) + "```\n\nSource: [`payments/stages/dev.yml`](../../platform/render/testdata/root/capabilities/payments/stages/dev.yml)\n<!-- /embed -->\ny\n"`, no diags.
  - `TestEmbedRange`: `lines=2-3` of `capabilities/payments/apps/my-app/app.yml` → body = those 2 lines; source label `payments/apps/my-app/app.yml:2-3`, link `…#L2-L3`.
  - `TestEmbedDedent`: range of indented lines (e.g. lines under `stages:` in my-app) → common indent removed.
  - `TestEmbedIdempotent`: `Embed(Embed(p))` == `Embed(p)`.
  - `TestEmbedNoTrailingNewline`: use `fstest.MapFS{"a.yml": {Data: []byte("a: 1")}}` → body `"a: 1\n"`.
  - `TestEmbedInsideFenceIgnored`: marker lines inside a ```` ```markdown ```` fence → text unchanged, no diags.
  - `TestEmbedErrors` (table): missing file, `path=../x.yml`, `lines=5-3`, `lines=0-2`, `lines=1-999`, `lines=abc`, `foo=1`, unclosed marker, nested open → one diag each at the marker line (`docs/guide/x.md:<n>:1: …`), text unchanged.
  - `TestEmbedsWalk`: temp root (guideRoot helper) + `docs/reference/a.md` with stale embed → `Embeds(root,false)` returns `["docs/reference/a.md"]`, file untouched; `Embeds(root,true)` writes it; second call returns none.
- [ ] **Step 2: Run** `go test ./internal/docs -run 'TestEmbed' ` → FAIL (undefined: Embed).
- [ ] **Step 3: Implement** `embed.go`. Line scan tracking fences with existing `openRx`/`closeRx` (skip markers inside fences). Attributes parsed as space-separated `k=v`; only `path`, `lines`. Path validity: `filepath.IsLocal` + `path.Clean(p)==p`.
- [ ] **Step 4: Run** `go test ./internal/docs` → PASS.
- [ ] **Step 5: Commit** (`jj describe` already set; nothing else).

### Task 2: `docs.Run`: both dirs, embed-exempt fences, fixture lint

**Files:**
- Modify: `platform/cli/internal/docs/check.go`, `check_test.go`

**Interfaces:**
- Consumes: Task 1 `Dirs`, `Embeds`, `embedOpenRx`, `embedCloseRx`.
- Produces: `func Check(root string) ([]capability.Diag, error)` now scans every `Dirs` page; `func Run(root string, write bool) (changed []string, diags []capability.Diag, err error)` = `Embeds` → if any docs dir exists, `lint.All(<root>/FixtureRoot, nil)` with `File` prefixed `FixtureRoot+"/"` → `Check`; diags `SortDedupe`d.

- [ ] **Step 1: Failing tests:**
  - `TestCheckReferenceDir`: bad `path=` fence in `docs/reference/x.md` → `docs/reference/x.md:2:1: bogus: field not allowed`.
  - `TestCheckEmbedFenceExempt`: page `"<!-- embed path=capabilities/payments/stages/dev.yml -->\n```yaml\na: 1\n```\n<!-- /embed -->\n```yaml\nb: 1\n```\n"` → only `docs/guide/x.md:6:1: yaml block needs …`.
  - `TestRunFixtureLint`: guideRoot, then append `bogus: 1\n` to the copied fixture's `capabilities/payments/stages/dev.yml` → `Run(root,false)` diags include `platform/render/testdata/root/capabilities/payments/stages/dev.yml:…: bogus: field not allowed`.
  - `TestRepoDocs` replaces `TestRepoGuides`: `Run("../../../..", false)` → no changed, no diags (`t.Errorf("stale: %v; run gcd docs")`).
- [ ] **Step 2: Run** `go test ./internal/docs` → the new tests FAIL.
- [ ] **Step 3: Implement**: `Check` globs each of `Dirs`; `parse` skips the annotation rule between embed markers (fences still parsed so they close); add `Run`.
- [ ] **Step 4: Run** `go test -count=1 ./internal/docs` → PASS except `TestRepoDocs` may fail only if repo docs have problems (none yet: no embeds) → expect PASS.
- [ ] **Step 5: Commit.**

### Task 3: `gcd docs` command; delete SchemaMarkdown

**Files:**
- Modify: `platform/cli/internal/cmd/docs.go`, `docs_test.go`, `platform/mise-tasks/test-cli`
- Delete: `platform/cli/internal/capability/schemamd.go`, `schemamd_test.go`

**Interfaces:**
- Consumes: `docs.Run`.

- [ ] **Step 1: Failing tests** (`docs_test.go`; delete `TestDocsReferenceGolden`, `TestDocsIdempotent`, `referencePages`; drop `--out` from the rest):
  - `TestDocsRewritesEmbed`: badGuideRoot-style root with `docs/reference/a.md` holding a stale embed of `capabilities/payments/stages/dev.yml` → `gcd docs` succeeds; file now contains `Source: [`; stderr says `docs: updated 1 page(s)`.
  - `TestDocsGuideErrors`: unchanged except no `--out`.
  - `TestDocsSandboxRoot`: `--root ../../../render/testdata/root` → no error.
- [ ] **Step 2: Run** `go test ./internal/cmd -run TestDocs` → FAIL (unknown flag / no rewrite).
- [ ] **Step 3: Implement**: `RunE` = resolveRoot → diagFormat → `docs.Run(root, true)` → print `docs: updated N page(s)` to stderr when N>0 → writeDiags → exit 1 `docs: N problem(s)`. New `Short`/`Long` describing embeds + fence annotations. Delete schemamd files. `test-cli` last line: `-run 'TestRepoDocs'  ./internal/docs`.
- [ ] **Step 4: Run** `mise run test-cli` → PASS (gofmt, vet, all tests).
- [ ] **Step 5: Commit.**

### Task 4: Hand-written reference pages

**Files:**
- Rewrite: `docs/reference/{owners,stage,app,target,env-vars}.md`

Each page (read the fixture and `gcd schema <kind>` first; every field in `gcd schema <kind>` gets mentioned):
- `# <kind>`: one-paragraph intro (what the file is, where it lives, who owns it).
- `## Defining a(n) <kind>`: whole-file embed of the richest fixture file, then a bullet list of the top-level parts.
- Concept sections (H2/H3) with prose; required/defaults/allowed values in sentences; ranged embeds or `path=`/`expect=`/`fragment` fences for partials; a table only where it compresses:
  - **app**: artifact (`image`/`module`, constraints, `port`, `sourceRepo`), layers (table: field × app/stage/target levels, merge rule), env + `secret:` + `envFrom`, replicas/resources (defaults), deploy with GitHub Actions (`deployWith`, `workflow`).
  - **target**: naming, `environment`/`environmentType` (table of types → policy), platforms (table: field × K8S/CLOUD_RUN/TERRAFORM), auth/billing.
  - **owners**: roles (`codeOwners`, `promoters`, `viewers`, `breakGlass`), per-app overrides (replace semantics).
  - **stage**: `dependsOn` graph, `autoPromote` (prd rule, with `expect=` example), `color`.
  - **env-vars**: file location, flat map, `secret:` values, precedence vs `env`.
- End: `Full field list: gcd schema <kind>`.

- [ ] **Step 1:** Write the 5 pages with empty embed bodies.
- [ ] **Step 2: Run** `.bin/gcd docs` (after `mise run cli`) → embeds filled, exit 0. Inspect a page for correct ranges.
- [ ] **Step 3: Run** `go test -count=1 -run TestRepoDocs ./internal/docs` → PASS.
- [ ] **Step 4: Commit.**

### Task 5: Wiring

**Files:**
- Modify: `CLAUDE.md` (docs layout line: reference hand-written, `<!-- embed -->` blocks; `gcd docs` command line), `docs/README.md` (reference description), guide pages only if a link anchor broke.

- [ ] **Step 1:** Edit; `grep -rn 'generated by gcd docs\|--out' CLAUDE.md docs/README.md docs/guide` → nothing stale.
- [ ] **Step 2: Run** `mise run test-cli` → PASS.
- [ ] **Step 3: Commit.**
