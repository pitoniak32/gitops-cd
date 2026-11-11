// Package render writes the generated manifests (rendered/) from the CUE renderer (ADR 0026).
package render

import (
	cueerrors "cuelang.org/go/cue/errors"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cueload"
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

// Capability renders one capability into out, replacing only that capability's paths.
// Validation errors are returned unchanged (they carry CUE positions).
func Capability(root, capName, out string) ([]string, error) {
	if st, err := os.Stat(filepath.Join(root, "capabilities", capName)); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no capability dir %s", filepath.Join(root, "capabilities", capName))
	}
	v, err := cueload.Capability(root, capName)
	if err != nil {
		return nil, err
	}
	// Validate checks everything; evaluating only `files` would miss e.g. typo'd fields.
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, err
	}
	var files map[string]string
	if err := v.LookupPath(cueload.Lookup("files")).Decode(&files); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	for _, g := range []string{
		filepath.Join(out, "apps", capName),
		filepath.Join(out, "kargo", capName+".yaml"),
		filepath.Join(out, "argocd", "projects", "*", capName+".yaml"),
		filepath.Join(out, "argocd", "deploys", "*", capName),
		filepath.Join(out, "pipelines", capName+".json"),
		filepath.Join(out, "codeowners", capName),
	} {
		if err := removeGlob(g); err != nil {
			return nil, err
		}
	}
	return writeFiles(out, files)
}

// Regions writes the platform-level files (regions.cue regionFiles) under out.
func Regions(root, out string) error {
	v, err := cueload.Platform(root, "clusters.cue", "regions.cue")
	if err != nil {
		return err
	}
	var files map[string]string
	if err := v.LookupPath(cueload.Lookup("regionFiles")).Decode(&files); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(out, "infrastructure")); err != nil {
		return err
	}
	_, err = writeFiles(out, files)
	return err
}

// All renders every capability, regions, checks app names, assembles CODEOWNERS, copies the README.
func All(root, out string, w io.Writer) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, d := range []string{"apps", "argocd", "kargo", "pipelines", "codeowners", "infrastructure"} {
		if err := os.RemoveAll(filepath.Join(out, d)); err != nil {
			return err
		}
	}
	if err := Fragments(root, out, w); err != nil {
		return err
	}
	if err := Regions(root, out); err != nil {
		return Wrap(err)
	}
	if err := UniqueApps(out); err != nil {
		return err
	}
	fmt.Fprintln(w, "rendered regions")
	if err := CodeOwners(root, out); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(cueload.RenderDir(root), "rendered-README.md"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "README.md"), b, 0o644)
}

// Fragments renders every capability into out (no regions, no CODEOWNERS assembly).
func Fragments(root, out string, w io.Writer) error {
	caps, _ := filepath.Glob(filepath.Join(root, "capabilities", "*"))
	sort.Strings(caps)
	var names []string
	for _, c := range caps {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			names = append(names, filepath.Base(c))
		}
	}
	return Selected(root, names, out, w)
}

// Selected renders the named capabilities into out, continuing past failures and returning
// every error joined, each prefixed "<cap>:".
func Selected(root string, names []string, out string, w io.Writer) error {
	var errs []error
	for _, n := range names {
		if _, err := Capability(root, n, out); err != nil {
			errs = append(errs, fmt.Errorf("%s:\n%s", n, Wrap(err)))
			continue
		}
		fmt.Fprintln(w, "rendered", n)
	}
	return errors.Join(errs...)
}

// Wrap expands CUE errors into "path: message" lines with positions; other errors pass through.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	var ce cueerrors.Error
	if !errors.As(err, &ce) {
		return err
	}
	return fmt.Errorf("%s", strings.TrimSpace(cueerrors.Details(err, nil)))
}
