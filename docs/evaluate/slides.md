---
theme:
  name: catppuccin-macchiato
  override:
    footer:
      style: empty
    default:
      alignment: center
    code:
      alignment: center
      minimum_size: 58
      padding:
        horizontal: 2
        vertical: 1
---

<!--
Condensed why.md. Run:
  presenterm -c docs/evaluate/presenterm.yaml docs/evaluate/slides.md
new_lines pads center each slide's content in the config's max_rows: 26 box;
re-measure them after changing a slide's height.
gcd docs skips this deck (presenterm rejects fence
annotations), so snippets aren't lint-checked.
-->

<!-- jump_to_middle -->

Why **Kargo**?
===

<!-- alignment: center -->

_One config model, one promotion flow, every platform_

<!-- end_slide -->

The Problem
===

<!-- alignment: left -->

<!-- new_lines: 1 -->

<span style="color: #8aadf4"></span>

<!-- new_lines: 1 -->

- `Pipeline Drift`
  - each team hand-builds their own CD pipelines
  - every setup is a one-off, making debugging by the platform team very difficult
  - when an improvement / patch is discovered, every team has to update their own pipeline
- `Access Sprawl`
  - every team owning all aspects of their CD pipeline makes enforcing access controls very difficult
- `Policy Enforcement`
  - teams can edit or skip checks

<!-- end_slide -->

What is **gitops-cd**?
===

<!-- alignment: left -->

<!--
speaker_note: |
  Drift is eliminated, one schema, every app has the same pipeline structure and tools.
  Access is handled by `owners.yaml`, not invisible controls in a UI. We can see who is allowed to do what.
  Policy → platform-owned, teams can't edit it.
  A whole class of errors are eliminated by lint and render tooling.
  Toolchains converge so its easier for the platform team to support application teams.
-->

**`TLDR; its the glue between CI, and applying your manifests.`**

- Teams (or agents!) write YAML.
- a platform-owned scaffolder lints and renders it.
- Kargo enabled fearless promotions.
- GHA (***Your tool of choice here***) applys the manifests (deploys).

```mermaid +render
%%{init: {'theme': 'dark'}}%%
flowchart LR
  yaml["YAML<br/>(PR)"] --> scaf["Scaffolder<br/>lint + render"]
  scaf --> kargo["Kargo<br/>promote"]
  kargo --> gha["GHA<br/>deploy"]
  gha --> k8s[K8S] & cr[Cloud Run] & tf[Terraform]
```

<!-- end_slide -->

Compared
===

<!-- new_lines: 5 -->

| | gitops-cd | Kargo alone | GHA alone |
|---|---|---|---|
| Governance | <span style="color: #40a02b">✓</span> | <span style="color: #df8e1d">~</span> | <span style="color: #df8e1d">~</span> |
| Enforcement | <span style="color: #40a02b">✓</span> | <span style="color: #d20f39">✗</span> | <span style="color: #df8e1d">~</span> |
| Guardrails | <span style="color: #40a02b">✓</span> | <span style="color: #d20f39">✗</span> | <span style="color: #df8e1d">~</span> |
| Consistency | <span style="color: #40a02b">✓</span> | <span style="color: #d20f39">✗</span> | <span style="color: #d20f39">✗</span> |
| Flexibility | <span style="color: #40a02b">✓</span> | <span style="color: #40a02b">✓</span> | <span style="color: #40a02b">✓</span> |

<span style="color: #40a02b">✓</span> built in · <span style="color: #df8e1d">~</span> partly · <span style="color: #d20f39">✗</span> no

<!-- end_slide -->

Governance + Access
===

<!-- alignment: left -->

<!-- new_lines: 2 -->


```yaml
# owners.yaml (excerpt)
codeOwners: [team1]
promoters: {prod: [doug@example.com]}
breakGlass: [amelia@example.com]
```

Generates CODEOWNERS + Kargo access, matched on **email**.

- **Pre-deploy checks** before every `prd` deploy
- **No auto-promotion** into prod; replica floors
- **PR only**; UIs read-only (admission policy)
- **Break-glass** approval: named on-call only

(***these are all fully configurable by platform team***)

<!-- end_slide -->

Learn with guardrails
===

<!-- alignment: left -->

Scaffolder CLI: errors say what and where, before review.

```sh
gcd schema app            # every field + rules
gcd lint                  # file:line:col: message
gcd show payments/web -s  # merged config + sources
gcd --root <sandbox> lint # try anything safely
```

(***These checks also run in CI, and comment issues / renderings on the PR***)

<!-- pause -->

```
$ gcd lint payments
apps/web/app.yml:2:1: image: invalid value
  "ghcr.io/example/web:1.2.3" (must match ...)
```

**Agents too**: `system-config` skill edits YAML, then
verifies with `gcd lint` + `gcd show --diff --rendered`

<!-- end_slide -->

One model, every platform
===

<!-- column_layout: [6, 4] -->
<!-- column: 0 -->

```yaml
# apps/web/app.yml
image: ghcr.io/example/web
stages:
  dev:
    targets: {us-east4-dev-k8s: {}}  # K8S
  staging:
    targets: {us-east4-npr-cr: {}}   # Cloud Run
```

```yaml
# apps/infra/app.yml
module: ghcr.io/example/tf-bucket
moduleConstraint: ^1
stages:
  dev:
    targets: {us-east4-dev-tf: {}}   # Terraform
```

<!-- column: 1 -->

```mermaid +render +width:100%
%%{init: {'theme': 'dark'}}%%
flowchart TB
  onprem[On-prem K8s] --> k8s[Cloud K8s]
  onprem --> cr[Cloud Run]
```

<!-- new_lines: 2 -->

- Every field overridable per stage or target
- Terraform: `module:` instead of `image:`
- Migrate one stage at a time, same pipeline

<!-- reset_layout -->

<!-- end_slide -->

Demo
===

![image:width:60%](slides-playground.png)

<!-- end_slide -->

Trade-offs and future
===

<!-- new_lines: 4 -->

<!-- column_layout: [1, 1] -->
<!-- column: 0 -->

**Trade-offs**

- New field = change by platform
- Scaffold is owned code
- Rendered manifests artifact

<!-- column: 1 -->

**Future**

- Required stage templates
- Team-owned config repos
- Checks per stage / target

<!-- reset_layout -->

<!-- pause -->

**Questions?** Full write-up: `docs/evaluate/why.md`
