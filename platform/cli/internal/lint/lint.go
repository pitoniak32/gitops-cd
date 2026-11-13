// Package lint validates system YAML and reports diagnostics as
// <file>:<line>:<col>: [<label>: ]<message>, paths relative to the repo root.
package lint

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/progress"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"golang.org/x/sync/errgroup"
)

// Diag is one diagnostic.
type Diag = system.Diag

// Scope is what to validate. The cross-system name check (system.CheckNames) always runs.
type Scope struct {
	Teams   bool     // the teams/ files themselves
	Systems []string // systems, each on its own
}

// Run validates scope under root with cfg: systems in parallel, teams/ read once. Diagnostics
// are sorted by file, line, col and deduped; the error joins I/O errors and missing system dirs,
// each prefixed "<sys>:". prog (nil: none) shows the phases: teams, names, systems.
func Run(cfg system.Config, root string, s Scope, prog *progress.Tracker) ([]Diag, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	teams, err := system.LoadTeams(root)
	if err != nil {
		return nil, err
	}
	var all []Diag
	names := system.Names(root)
	// one percentage for the whole lint (tab status): teams + names + systems
	if s.Teams {
		prog.Plan(1)
	}
	prog.Plan(len(names) + len(s.Systems))
	if s.Teams {
		p := prog.Start("teams", 1)
		all = append(all, teams.Lint()...)
		p.Step("teams/")
		p.Done()
	}
	p := prog.Start("names", len(names))
	all = append(all, system.CheckNames(root, p.Step)...)
	p.Done()
	diags := make([][]Diag, len(s.Systems))
	errs := make([]error, len(s.Systems))
	p = prog.Start("systems", len(s.Systems))
	var g errgroup.Group
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, c := range s.Systems {
		g.Go(func() error {
			d, err := system.Lint(cfg, root, c, teams)
			diags[i] = d
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", c, err)
			}
			p.Step(c)
			return nil // every system runs; errors are joined below
		})
	}
	_ = g.Wait()
	p.Done()
	for _, d := range diags {
		all = append(all, d...)
	}
	return system.SortDedupe(all), errors.Join(errs...)
}

// All lints systems (default: every systems/* dir) and the teams/ files, with the default config.
func All(root string, systems []string) ([]Diag, error) {
	if len(systems) == 0 {
		systems = system.Names(root)
	}
	return Run(system.Default(), root, Scope{Teams: true, Systems: systems}, nil)
}
