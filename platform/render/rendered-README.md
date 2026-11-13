# rendered

**Generated. This branch (`rendered`) is written only by CI on each push to main (ADR 0002). Do not edit.**
Source: the `render: <sha>` commit body links the main commit it was rendered from.
Read by the ApplicationSets in `platform/appsets/` (ADR 0017) at revision `rendered`: `kargo/` (hub),
`argocd/projects/<region>/` + `argocd/deploys/<region>/` (each region), `infrastructure/regions/` (hub-side
per-region add-ons). Kargo Warehouses watch `apps/<sys>/<app>/` here.

Workloads never sync from here directly: Kargo promotion copies
`apps/<sys>/<app>/<stage>/<target>/` onto branch `env/<sys>/<app>/<target>`, which Argo CD tracks.
Rollback: promote older Freight in Kargo, or revert the source change in `systems/` on main.
