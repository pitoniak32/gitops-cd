// Package render writes the generated manifests (rendered/) built by package capability.
package render

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
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
// Validation errors are returned as capability.Error (the diagnostics, one per line).
func Capability(root, capName, out string) ([]string, error) {
	files, err := capability.Render(capability.Default(), root, capName)
	if err != nil {
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

// PromotionTasksFile is the platform-level ClusterPromotionTasks file under rendered/ (ADR 0027).
const PromotionTasksFile = capability.PromotionTasksFile

// Platform writes the platform-level files (no capability input) under out: region files and the
// promotion tasks.
func Platform(root, out string) error {
	for _, p := range []string{"infrastructure", PromotionTasksFile} {
		if err := os.RemoveAll(filepath.Join(out, p)); err != nil {
			return err
		}
	}
	_, err := writeFiles(out, capability.PlatformFiles(capability.Default()))
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

// All renders every capability, platform files, checks app names, assembles CODEOWNERS, copies the README.
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
	if err := Platform(root, out); err != nil {
		return err
	}
	if err := UniqueApps(out); err != nil {
		return err
	}
	fmt.Fprintln(w, "rendered platform (regions, promotion tasks)")
	if err := CodeOwners(root, out); err != nil {
		return err
	}
	b, err := readme(root)
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

// Wrap is the text shown for a render error: validation errors already carry "<file>:<line>:<col>:
// <message>" lines, so every error passes through.
func Wrap(err error) error { return err }
