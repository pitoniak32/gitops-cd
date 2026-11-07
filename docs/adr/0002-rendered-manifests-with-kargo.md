# Rendered manifests promoted by Kargo

CI renders every app/stage/target to `rendered/` on main, and Kargo promotes by copying a stage's folder from a Freight commit onto its stage branch, which Argo CD syncs. Clusters never sync from `main` directly, so a merged change (including a platform change) reaches each stage only through promotion, and reviewers see exact manifest diffs instead of templates.
