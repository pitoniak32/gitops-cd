# Renderer: regions, spec.shard, per-region Argo CD output
Status: needs-triage

- **Cluster registry:** `clusters.cue` registers `us` and `onprem` with `region`, replacing `in-cluster`.
- **Targets:** reshape to the spec "Demo targets" list (one per region per stage); delete the eu targets.
- **Stages:** target Stages get `spec.shard`.
- **Argo CD output:** Applications and AppProjects go to `rendered/argocd/<region>/`.
- **Housekeeping:** update `files.cue`, golden, lint cases, schemas, and `test.sh` (refs `gcp-eu-prod`).

Done when: `mise run test`/`lint` pass and golden shows shards + per-region dirs.

## Comments
- `#Cluster`: required `region` (= shard = Argo CD instance); `argocdName` defaults `"in-cluster"` (name inside the regional Argo CD). Registry: `us`, `onprem` (kind, all envs, no secretStore).
- Target Stages `spec.shard: <region>`; gates/Warehouses unsharded. Argo CD out: `argocd/<region>/<cap>.yaml` (AppProject w/ only that region's dests + Apps); regions without deploys get no file. `render.sh` also prunes `argocd/*/<cap>.yaml` and the legacy `argocd/<cap>.yaml`.
- Targets reshaped per spec (demo + testdata/root); test.sh + lint-cases updated; new asserts for shards, per-region files, destinations; lint case for unknown cluster.
- Bootstrap (ticket 04): `platform/bootstrap/rendered.yaml` `rendered-argocd` path `rendered/argocd` no longer matches (files now under `<region>/`); needs one root App per region (path `rendered/argocd/<region>`, dest in-cluster of that region's Argo CD). Stale `rendered/argocd/{payments,search}.yaml` removed on next CI render.
