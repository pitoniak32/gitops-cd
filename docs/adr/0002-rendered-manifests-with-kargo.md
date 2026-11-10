# Rendered manifests promoted by Kargo

_Rendering moved from a post-merge commit on main to the PR: ADR 0022._

CI renders every app/stage/target to `rendered/` on main, and Kargo promotes by copying a target's folder from a Freight commit onto its target branch, which Argo CD syncs (originally a stage folder onto a stage branch; per-target since ADR 0012). Clusters never sync from `main` directly, so a merged change (including a platform change) reaches each stage only through promotion, and reviewers see exact manifest diffs instead of templates.
