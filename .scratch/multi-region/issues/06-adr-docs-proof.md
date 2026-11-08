# ADR 0015, docs, clean up proof
Status: needs-triage
Blocked by: 01, 02, 03, 04, 05

- **ADR 0015:** multi-region topology. Cite https://docs.kargo.io/operator-guide/architecture#distributed-configuration and note that it supplements ADR 0012.
- **Docs:** update README, CLAUDE.md layout, GLOSSARY (hub, region, shard), and `docs/gotchas.md` (`spec.shard` vs label; hub shard release needs `dataPlane.install: false`; regional controller needs events RBAC in `argocd`).
- **Proof:** a clean `mise run up`.

## Comments
- 2026-11-08: ADR 0015 written (cites Kargo distributed-config docs; supplements 0012). GLOSSARY: Hub, Region, Shard; Cluster + Bootstrap updated. CLAUDE.md: intro, layout (cluster/, bootstrap/), up/down, regional addons + new-region rules. Gotchas were added by tickets 02/04/05. Clean `mise run up` proof: ran at the end of ticket 05 with all infra changes in place (exit 0); re-run `infra` idempotent; all UIs 200; `mise run test` 0. Not provable before push: regional workload apps (need CI render on main + first Kargo promotions).
