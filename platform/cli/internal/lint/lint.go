// Package lint validates capability YAML and reports diagnostics as
// <file>:<line>:<col>: [<label>: ]<message>, paths relative to the repo root.
package lint

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
)

// Diag is one diagnostic.
type Diag = capability.Diag

// Capability lints one capability under root. The error is for I/O and a missing capability dir.
func Capability(root, capName string) ([]Diag, error) {
	return CapabilityWith(capability.Default(), root, capName)
}

// CapabilityWith is Capability with a platform configuration other than the default.
func CapabilityWith(cfg capability.Config, root, capName string) ([]Diag, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return capability.Lint(cfg, root, capName)
}

// All lints caps (default: every capabilities/* dir), sorted by file, line, col and deduped
// (teams/ errors show up once per capability).
func All(root string, caps []string) ([]Diag, error) {
	if len(caps) == 0 {
		caps = capability.Names(root)
	}
	var all []Diag
	var errs []error
	for _, c := range caps {
		d, err := Capability(root, c)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c, err))
		}
		all = append(all, d...)
	}
	return capability.SortDedupe(all), errors.Join(errs...)
}
