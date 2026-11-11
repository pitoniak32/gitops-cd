# One Kargo project per app

Status: proposed. Amends ADR 0007 (Kargo side), 0012 and 0019 (Stage names), 0016 (Freight `create` consequence), 0023 (RBAC names, viewer scope).

Each capability had one Kargo project, `kargo-<cap>`, which held every app's Warehouse, Stages and RBAC. Kargo's own model is a project per pipeline, which here means per app. A capability-wide project also leaked access:

- **Freight assembly** (ADR 0016): RBAC can't limit Freight `create` to one Warehouse, because Freight names are content hashes. So any promoter in a capability could assemble Freight for any app in it.
- **Viewers**: project read went to every email in the capability (`_allEmails`). `apps.<app>.viewers` in `owners.yml` therefore narrowed nothing.

## Decision

- **Kargo project `kargo-<cap>-<app>` per app.** Each project holds the app's Namespace (labeled `example.com/capability`, `example.com/app`), Project, ProjectConfig (that app's auto-promote Stages), Warehouse `<app>`, Stages, `promote-<stage>` RBAC and `viewers`. All of a capability's projects stay in one file, `rendered/kargo/<cap>.yaml`.
- **Stage names lose the `<app>-` prefix**, since the project already scopes them. A target Stage is `<target>`. A control-flow Stage is `<D>` (fan-in or entry), `<S>-done` (fan-out) or `<S1>-<S2>…-done`.
- **Per-app access.** `viewers` and Argo CD read access go to the app's emails: its promoters (stages it uses, after overrides), its viewers (app override, else the capability's) and the capability's break-glass emails. `users-<cap>` (list project names) stays capability-wide.
- **Argo CD AppProjects stay per capability per region** (Argo CD's usual per-tenant grouping). The single `viewer` role becomes one `viewer-<app>` role per app, with policies naming that app's Applications (`<cap>/<cap>-<app>-<target>`) exactly. A glob such as `<cap>-api-*` would also match an app named `api-v2`.
- **Unchanged:** Argo CD Application names `<cap>-<app>-<target>`, `env/<cap>/<app>/<target>` branches, Promotion vars, and the shared ClusterPromotionTasks.

## Considered Options

- **Keep one project per capability, scope RBAC finer**: rejected. Freight `create` can't be scoped below a project.
- **One Argo CD AppProject per app**: rejected. That means many near-identical projects with the same repos and destinations. Per-app roles already give per-app visibility.
- **Share one gitops (config) Warehouse**: rejected. Warehouses and Freight are project-scoped, so a Warehouse can't be shared across projects. Inside one project, a shared config Warehouse plus per-app image Warehouses would split config and image into separate Freight, which loses ADR 0016's chosen pairing. All it would save is git polling.
- **Drop or change the `kargo-` project prefix** (`<cap>-<app>`, `kp-…`): rejected. The prefix keeps project namespaces apart from the hub's add-on namespaces. Capabilities are platform-approved, so no reserved-name lint.
- **Keep the `<app>-` prefix on Stages**: rejected. It's redundant inside a per-app project and uses up the 63-character Stage-name budget.

## Consequences

- The project name must fit a namespace (63 characters): lint rejects `kargo-<cap>-<app>` when it is longer.
- `kargo-<cap>-<app>` can collide across capabilities (cap `a-b` + app `c` vs cap `a` + app `b-c`). `gcd render` rejects duplicate project names, alongside duplicate Application names.
- A stage named like one of its app's targets (or `<S>-done`) would collide with that Stage name. Lint rejects it.
- Migration loses Kargo history. `rendered-kargo` prunes `kargo-<cap>`, which deletes its Freight and Promotions. Workloads and `env/*` branches don't change. Run `mise run kargo-restore` afterwards.
- More projects in the Kargo UI, and no single pipeline view for a capability.
