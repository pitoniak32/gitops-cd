// Package render writes the generated manifests (rendered/) built by package system.
package render

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/progress"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"golang.org/x/sync/errgroup"
)

func writeFiles(out string, files map[string]string) ([]string, error) {
	names := make([]string, 0, len(files))
	for f := range files {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, f := range names {
		p := filepath.Join(out, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(files[f]), 0o644); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func removeGlob(pattern string) error {
	m, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, p := range m {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

// System renders one system into out, replacing only that system's paths.
// Validation errors are returned as system.Error (the diagnostics, one per line).
func System(root, sysName, out string) ([]string, error) {
	return systemWith(root, sysName, out, nil)
}

// systemWith is System with teams/ already read (nil: read here). Systems write disjoint
// paths, so several may run at once.
func systemWith(root, sysName, out string, teams *system.Teams) ([]string, error) {
	files, err := system.Render(system.Default(), root, sysName, teams)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	for _, g := range []string{
		filepath.Join(out, "apps", sysName),
		filepath.Join(out, "kargo", sysName+".yaml"),
		filepath.Join(out, "argocd", "projects", "*", sysName+".yaml"),
		filepath.Join(out, "argocd", "deploys", "*", sysName),
		filepath.Join(out, "pipelines", sysName+".json"),
		filepath.Join(out, "codeowners", sysName),
	} {
		if err := removeGlob(g); err != nil {
			return nil, err
		}
	}
	return writeFiles(out, files)
}

// PromotionTasksFile is the platform-level ClusterPromotionTasks file under rendered/ (ADR 0027).
const PromotionTasksFile = system.PromotionTasksFile

// Platform writes the platform-level files (no system input) under out: region files and the
// promotion tasks.
func Platform(root, out string) error {
	for _, p := range []string{"infrastructure", PromotionTasksFile} {
		if err := os.RemoveAll(filepath.Join(out, p)); err != nil {
			return err
		}
	}
	_, err := writeFiles(out, system.PlatformFiles(system.Default()))
	return err
}

// readme is rendered/README.md's source: <root>/platform/render/rendered-README.md, or this repo's for
// fixture roots that hold only YAML inputs.
func readme(root string) ([]byte, error) {
	const rel = "platform/render/rendered-README.md"
	if b, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
		return b, nil
	}
	r, err := repo.Root()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(r, rel))
}

// All renders every system, platform files, checks app names, assembles CODEOWNERS, copies the README.
// prog (nil: none) shows the phases; with it, the per-system lines on w are left out.
func All(root, out string, w io.Writer, prog *progress.Tracker) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, d := range []string{"apps", "argocd", "kargo", "pipelines", "codeowners", "infrastructure"} {
		if err := os.RemoveAll(filepath.Join(out, d)); err != nil {
			return err
		}
	}
	prog.Plan(3) // platform files, unique names, CODEOWNERS; Selected plans the systems
	if err := Fragments(root, out, w, prog); err != nil {
		return err
	}
	if err := step(prog, "platform files", func() error { return Platform(root, out) }); err != nil {
		return err
	}
	if err := step(prog, "unique names", func() error { return UniqueApps(out) }); err != nil {
		return err
	}
	if prog == nil {
		fmt.Fprintln(w, "rendered platform (regions, promotion tasks)")
	}
	if err := step(prog, "CODEOWNERS", func() error { return CodeOwners(root, out) }); err != nil {
		return err
	}
	b, err := readme(root)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "README.md"), b, 0o644)
}

// AllFiles is what All writes under out, in memory (rendered/-relative path -> content): every
// system, the platform files and the README. CODEOWNERS (outside rendered/) is not included.
func AllFiles(root string) (map[string]string, error) {
	teams, err := system.LoadTeams(root)
	if err != nil {
		return nil, err
	}
	if d := teams.Lint(); len(d) > 0 {
		return nil, fmt.Errorf("teams:\n%w", system.Error(d))
	}
	out := system.PlatformFiles(system.Default())
	var errs []error
	for _, n := range system.Names(root) {
		files, err := system.Render(system.Default(), root, n, teams)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s:\n%w", n, Wrap(err)))
			continue
		}
		for p, c := range files {
			out[p] = c
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	b, err := readme(root)
	if err != nil {
		return nil, err
	}
	out["README.md"] = string(b)
	return out, nil
}

// step runs f as a one-step phase of prog.
func step(prog *progress.Tracker, name string, f func() error) error {
	p := prog.Start(name, 1)
	defer p.Done()
	if err := f(); err != nil {
		return err
	}
	p.Step("")
	return nil
}

// Fragments renders every system into out (no regions, no CODEOWNERS assembly).
func Fragments(root, out string, w io.Writer, prog *progress.Tracker) error {
	systems, _ := filepath.Glob(filepath.Join(root, "systems", "*"))
	sort.Strings(systems)
	var names []string
	for _, c := range systems {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			names = append(names, filepath.Base(c))
		}
	}
	return Selected(root, names, out, w, prog)
}

// Selected renders the named systems into out in parallel (teams/ read once), continuing past
// failures and returning every error joined: teams/ problems first ("teams:"), then each system's
// ("<sys>:"), in names order. "rendered <sys>" lines go to w in names order, unless prog (nil: none)
// shows a "systems" phase instead.
func Selected(root string, names []string, out string, w io.Writer, prog *progress.Tracker) error {
	prog.Plan(len(names))
	teams, err := system.LoadTeams(root)
	if err != nil {
		return err
	}
	var errs []error
	if d := teams.Lint(); len(d) > 0 {
		errs = append(errs, fmt.Errorf("teams:\n%w", system.Error(d)))
	}
	sysErrs := make([]error, len(names))
	p := prog.Start("systems", len(names))
	var g errgroup.Group
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, n := range names {
		g.Go(func() error {
			if _, err := systemWith(root, n, out, teams); err != nil {
				sysErrs[i] = fmt.Errorf("%s:\n%w", n, Wrap(err))
			}
			p.Step(n)
			return nil // every system runs; errors are joined below
		})
	}
	_ = g.Wait()
	p.Done()
	for i, n := range names {
		if sysErrs[i] != nil {
			errs = append(errs, sysErrs[i])
			continue
		}
		if prog == nil {
			fmt.Fprintln(w, "rendered", n)
		}
	}
	return errors.Join(errs...)
}

// Wrap is the text shown for a render error: validation errors already carry "<file>:<line>:<col>:
// <message>" lines, so every error passes through.
func Wrap(err error) error { return err }
