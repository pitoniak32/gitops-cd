// Package repo locates the repository root.
package repo

import (
	"fmt"
	"os"
	"path/filepath"
)

// marker is the file that identifies the repo root: gcd's own module.
const marker = "platform/cli/go.mod"

// Root returns GCD_ROOT (set by the ./gcd wrapper), else the nearest ancestor of cwd holding
// platform/cli/go.mod.
func Root() (string, error) {
	if r := os.Getenv("GCD_ROOT"); r != "" {
		return r, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("repo root: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root: no %s above cwd (run gcd inside the repo)", marker)
		}
		dir = parent
	}
}
