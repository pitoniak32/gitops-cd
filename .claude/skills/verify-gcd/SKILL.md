---
name: verify-gcd
description: Drive the gitops-cd `gcd` CLI (lint, show, show --diff, render, schema) against a disposable jj sandbox copy of systems/ + teams/ and capture evidence. Use to prove a renderer/lint/show change or a systems/ YAML change behaves as an app team would see it, beyond `gcd test`. Live kind clusters are read-only here.
---

# Verify gcd

Surface: the `gcd` CLI an app team runs on its YAML (`gcd lint`, `show`, `show --diff`, `render`, `schema`). Secondary surface: the live kind clusters (Kargo, Argo CD UIs). Those are a singleton the user owns. Observe them read-only (see `features/live-cluster.md`). Never run `gcd up`, `down`, `infra`, `kargo-*`, `creds` or `certs` for verification: kind cluster names are fixed, so no second instance can exist, and these commands rebuild or rewire the user's clusters.

Feature map: `features/README.md`. Read it, then follow the feature file for what you are proving.

Helper: `.claude/skills/verify-gcd/verify-gcd` (call it `V` below). Run it from the repo root. Each Bash call is a fresh shell, so re-export `VERIFY_RUN` in every call that uses a run (or `source` a file you saved it to).

## Launch

```bash
V=.claude/skills/verify-gcd/verify-gcd
eval "$($V launch)"          # real systems/ + teams/ ; or: eval "$($V launch --fixture)"  (platform/render/testdata/root)
echo "$VERIFY_RUN"           # keep it; e.g. echo "export VERIFY_RUN=$VERIFY_RUN" > <scratchpad>/run.env
```

`launch` does these steps:
1. Runs `mise run build`, which rebuilds `.bin/gcd` when `platform/cli` changed. Without this you would test a stale binary.
2. Copies the inputs to `.verify/<run>/sandbox/`. For the repo source that is `systems/`, `teams/` and `.github/CODEOWNERS.tmpl`. For the fixture source it is the whole fixture root.
3. Renders the baseline into `sandbox/rendered/`.
4. Runs `jj git init`, describes the baseline commit `verify baseline (<src>)`, and runs `jj new`.

After launch, `@-` is the baseline, so `--base @-` and `--against @-` diff your sandbox edits only. Ready = `launch` printed `export VERIFY_RUN=…` and `$V doctor` is all `ok`. Runs never share a sandbox, so parallel runs are safe.

`--root <sandbox>` is how gcd reads the copy. The real repo is never an input. Always pass explicit `--base @-` / `--against @-`. The sandbox has no trunk remote, so the default fork point is meaningless there.

## Doctor

```bash
export VERIFY_RUN=<run>; $V doctor
```

Read-only. It checks four things: `.bin/gcd` exists, it is newer than every `platform/cli` source, jj is on PATH, and the sandbox exists with the baseline at `@-`. It then lists the sandbox's current edits against the baseline. Any `FAIL` means stop. Fix it or relaunch. Don't drive the run.

## Drive

| Command | What it does |
|---|---|
| `$V run <label> <gcd args…>` | Runs `gcd --root <sandbox> --color never <args>` with cwd = sandbox. Saves `<label>.{cmd,stdout,stderr,exit}` and prints the exit code and output. The helper itself always exits 0. Read the `exit=` it prints. |
| `$V edit <label> <path-in-sandbox> '<sed expr>'` | Edits a sandbox input the way a user would edit YAML, and saves `<label>.diff` (jj git diff of that file). |
| `$V sh <label> '<shell cmd>'` | Runs any command in the sandbox with `.bin` on PATH, captured the same way, e.g. inspecting `rendered/` or `jj diff`. |
| `$V reset` | `jj restore`: all sandbox edits go back to the baseline. |

Example (proven 2026-11-18):
```bash
$V edit replicas systems/payments/apps/my-app/app.yml 's/^replicas: 1/replicas: 4/'
$V run diff-stat show --diff --rendered --against @- --stat   # 4 deployment.yaml +1 -1 (dev targets override replicas)
```

Exact recipes per feature: `features/*.md`.

## Evidence

Location: `.verify/<run>/evidence/` in the repo (gitignored via `/.verify/`, so jj does not track it). It survives cleanup. `00-baseline-render.txt` is the launch log.

Proof standards:
- Drive real user commands (`gcd lint|show|render|schema`) on real YAML edits. Don't call Go functions and don't use `go test` as proof. Tests are a separate check.
- Capture the action (the `edit` diff and the `.cmd`) and the result (`stdout`, `stderr`, `exit`). An exit code alone is not proof. Assert on the message text or file contents.
- Check side effects. For `render`, inspect the files written under the sandbox (`$V sh … 'cat rendered/…'`, `jj diff --summary`), not just the `rendered …` log lines.
- Show both sides: a failing journey (lint error, exit 1) and the fixed state (exit 0) after the edit is reverted or corrected.
- Cite evidence paths in your report.

## Cleanup

```bash
export VERIFY_RUN=<run>; $V cleanup     # deletes .verify/<run>/sandbox only; evidence stays
```

Run it after every run, including failed attempts. Delete old evidence (`rm -rf .verify/<run>`) only when the user asks. Nothing runs in the background, so there are no processes to kill.

## Gotchas

- `gcd render --out` is relative to the **cwd**, not `--root`. `$V run` sets cwd = sandbox. Plain `gcd --root <sb> render` from the repo root overwrites the real `rendered/`.
- Per-file schema errors hide cross-file errors. A bad `replicas` masks a bad `envFrom` until it is fixed. Fix one, then re-lint.
- `show` and `render` refuse to run on lint errors (`Error: <sys>: N problem(s); fix them first`, exit 1).
- Narrower layers win. Editing top-level `replicas` does not change targets that override it (my-app dev targets). That is correct behavior, not a bug.
- `gcd` won't run outside the repo without `--root`: `no platform/cli/go.mod above cwd`. `$V` always passes `--root`.
