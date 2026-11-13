# Reference: hand-written pages, embedded fixture examples

Supersedes the generated-table reference of [2026-11-12-app-team-docs-design](2026-11-12-app-team-docs-design.md)
(and closed PR #28). Style model: Kargo reference docs (promotion-templates, expressions).

## Goal

`docs/reference/{owners,stage,app,target,env-vars}.md` read like Kargo's reference: what the file is,
"Defining a …" with a full example, then sections walking through its concepts. Tables only where they
help (enum values, variable lists). Examples can't rot: they are spliced from the sandbox fixture by
`gcd docs`.

## Embed blocks

```markdown
<!-- embed path=capabilities/payments/apps/my-app/app.yml lines=1-12 -->
```yaml
…replaced by gcd docs…
```
<!-- /embed -->
```

- `path` relative to the fixture root (`platform/render/testdata/root`), same convention as `path=` fences.
- `lines=A-B` inclusive, 1-based. No `lines` = whole file. Several hunks:
  `lines="1-2,(9-10;text='dev overrides'),30"`: comma-separated, in order, no overlap; `(range;text='…')`
  adds a `# …` comment line above the hunk (keyword options only). Hunks are dedented together; a gap
  between hunks shows as `# …`. The source label lists the hunks; the link spans first to last line.
- `gcd docs` replaces everything between the markers with: a fence (language from the file extension,
  `yaml` for `.yml`/`.yaml`) holding those lines, dedented by their common indent, then a source line
  ``Source: [`payments/apps/my-app/app.yml:1-12`](../../platform/render/testdata/root/capabilities/payments/apps/my-app/app.yml#L1-L12)``
  (link relative to the page).
- Allowed in `docs/guide/*.md` and `docs/reference/*.md`.
- Errors (as diags at the marker line, like lint): file missing, path outside the fixture, bad or
  out-of-range `lines`, unclosed or nested marker, unknown attribute.
- A fence inside an embed is exempt from the `path=` / `expect=` / `fragment` rule.

## `gcd docs`

One command, no flags beyond `--format`:

1. Rewrite embeds in place in `docs/guide/` and `docs/reference/` (writes only files that change).
2. Lint the fixture once (`lint.All`): every embed source passes lint.
3. Check annotated yaml fences (current `docs.Check`) in both dirs.
4. Print problems like lint; exit 1 if any.

Removed: `capability.SchemaMarkdown` + tests, `--out`. `gcd schema <kind>` remains the full field list;
each reference page links to it.

## Tests

- `internal/docs`: embed parse/rewrite unit tests (whole file, range, dedent, source link, each error,
  idempotent, fence-in-embed not flagged).
- `TestRepoDocs` (replaces `TestRepoGuides` + `TestDocsReferenceGolden`): repo docs are up to date
  (rewrite in memory = no change) and check clean. In `test-cli`'s `-count=1` run.
- `cmd`: `gcd docs` rewrites a stale embed, reports a bad one, exits 1.

## Reference pages

Hand-written, one per kind. Each: intro; `## Defining a <kind>` with a whole-file embed; concept
sections (app: artifact, layers, env, replicas/resources, deploy with GHA; target: naming, environment
type, platforms; owners: roles, per-app overrides; stage: dependsOn, autoPromote, color; env-vars:
format, secrets, precedence). Required/defaults stated in prose; tables where they compress (platform
field matrix, enum values). Small partial snippets use `fragment` or `path=`/`expect=` fences.

The guide keeps its how-to framing and links to reference sections; verbatim fixture copies in the
guide may become embeds (optional, same PR only if trivial).

## Known risk

Editing a fixture file can shift a line range silently; the `gcd docs` diff shows it in review.

## Wiring

`CLAUDE.md` docs lines, `docs/README.md`, `cli.yaml` paths (already cover `docs/reference/**`,
`docs/guide/**`), `test-cli` test names.
