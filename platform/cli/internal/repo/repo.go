// Package repo locates the repository root.
package repo

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root returns MISE_PROJECT_ROOT, else the nearest ancestor of cwd holding .mise.toml.
func Root() (string, error) {
	if r := os.Getenv("MISE_PROJECT_ROOT"); r != "" {
		return r, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("repo root: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".mise.toml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root: no .mise.toml above cwd")
		}
		dir = parent
	}
}
