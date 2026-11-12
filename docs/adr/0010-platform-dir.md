# Platform code under `platform/`

Everything only the platform team edits lives in `platform/`: `cli/` (`gcd`, which includes the renderer), `render/` (fixtures and golden files), `bootstrap/`, `appsets/`, `infrastructure/`, `cluster/`, `mise-tasks/` and `tests/`. App teams see `capabilities/` at the root, plus a `platform/` they can ignore. One CODEOWNERS line (`/platform/`) covers all of it. Mise finds tasks through `[task_config] includes` in `.mise.toml`, and they run from the repo root.

## Considered Options

- **`teams/` into `platform/`**: it's a registry that teams read next to `capabilities/`, and CODEOWNERS already makes it platform-owned.
- **`docs/` into `platform/`**: the ADRs, glossary and gotchas are read by both audiences.
- **`rendered/` into `platform/`**: Argo CD and Kargo paths on `rendered` and the `env/*` branches would change for no gain.
