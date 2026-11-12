// Package lint validates capability YAML and reports diagnostics as
// <file>:<line>:<col>: [<label>: ]<message>, paths relative to the repo root.
package lint

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/progress"
	"golang.org/x/sync/errgroup"
)

// Diag is one diagnostic.
type Diag = capability.Diag

// Scope is what to validate. The cross-capability name check (capability.CheckNames) always runs.
type Scope struct {
	Teams bool     // the teams/ files themselves
	Caps  []string // capabilities, each on its own
}

// Run validates scope under root with cfg: capabilities in parallel, teams/ read once. Diagnostics
// are sorted by file, line, col and deduped; the error joins I/O errors and missing capability dirs,
// each prefixed "<cap>:". prog (nil: none) shows the phases: teams, names, capabilities.
func Run(cfg capability.Config, root string, s Scope, prog *progress.Tracker) ([]Diag, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	teams, err := capability.LoadTeams(root)
	if err != nil {
		return nil, err
	}
	var all []Diag
	if s.Teams {
		p := prog.Start("teams", 1)
		all = append(all, teams.Lint()...)
		p.Step("teams/")
		p.Done()
	}
	p := prog.Start("names", len(capability.Names(root)))
	all = append(all, capability.CheckNames(root, p.Step)...)
	p.Done()
	diags := make([][]Diag, len(s.Caps))
	errs := make([]error, len(s.Caps))
	p = prog.Start("capabilities", len(s.Caps))
	var g errgroup.Group
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, c := range s.Caps {
		g.Go(func() error {
			d, err := capability.Lint(cfg, root, c, teams)
			diags[i] = d
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", c, err)
			}
			p.Step(c)
			return nil // every capability runs; errors are joined below
		})
	}
	_ = g.Wait()
	p.Done()
	for _, d := range diags {
		all = append(all, d...)
	}
	return capability.SortDedupe(all), errors.Join(errs...)
}

// All lints caps (default: every capabilities/* dir) and the teams/ files, with the default config.
func All(root string, caps []string) ([]Diag, error) {
	if len(caps) == 0 {
		caps = capability.Names(root)
	}
	return Run(capability.Default(), root, Scope{Teams: true, Caps: caps}, nil)
}
