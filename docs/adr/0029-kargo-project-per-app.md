# One Kargo project per app

Kargo's model is a project per pipeline, which here means per app. A system-wide project would leak access: RBAC can't limit Freight `create` to one Warehouse (Freight names are content hashes), and project read can't be narrower than the project.

- **Kargo project `kargo-<sys>-<app>`** holds the app's Namespace (labeled `example.com/system`, `example.com/app`), Project, ProjectConfig (auto-promote Stages), Warehouse `<app>`, Stages and RBAC (ADR 0011). All of a system's projects stay in one file, `rendered/kargo/<sys>.yaml`.
- **Stage names have no app prefix**, because the project scopes them (ADR 0012).
- **Argo CD AppProjects stay per system per region**, which is Argo CD's usual tenant grouping. Per-app visibility comes from `viewer-<app>` roles that name that app's Applications (`<sys>/<sys>-<app>-<target>`) exactly. A glob such as `<sys>-api-*` would also match an app named `api-v2`.
- Argo CD Application names `<sys>-<app>-<target>` and `env/<sys>/<app>/<target>` branches are scoped by system.

## Considered Options

- **One project per system with finer RBAC**: Freight `create` can't be scoped below a project.
- **One Argo CD AppProject per app**: many near-identical projects with the same repos and destinations.
- **A shared config Warehouse**: Warehouses and Freight are project-scoped. Inside one project, it would split config and image into separate Freight and lose ADR 0016's pairing.
- **Drop the `kargo-` prefix**: the prefix keeps project namespaces apart from the hub's add-on namespaces.

## Consequences

- Lint rejects a `kargo-<sys>-<app>` longer than 63 characters (the namespace limit).
- `kargo-<sys>-<app>` can collide across systems (system `a-b` + app `c` vs system `a` + app `b-c`). `gcd render` rejects duplicate project names and duplicate Application names.
- The Kargo UI shows more projects, and there's no single pipeline view for a system.
