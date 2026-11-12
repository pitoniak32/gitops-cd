package render

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pmezard/go-difflib/difflib"
)

// FileDiff is one rendered file that differs between two roots (Old "" = added, New "" = removed).
type FileDiff struct {
	Path     string
	Old, New string
}

// readFiles maps slash paths under root/dir to contents (missing dir: empty).
func readFiles(root, dir string) (map[string]string, error) {
	m := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		m[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return m, err
}

// ChangedCaps lists the capabilities with any changed file under capabilities/<cap>/ between baseRoot
// and root (new, modified or deleted); a changed teams/ file selects every capability on both sides.
func ChangedCaps(baseRoot, root string) ([]string, error) {
	set := map[string]bool{}
	teams := false
	for _, dir := range []string{"capabilities", "teams"} {
		a, err := readFiles(baseRoot, dir)
		if err != nil {
			return nil, err
		}
		b, err := readFiles(root, dir)
		if err != nil {
			return nil, err
		}
		note := func(p string) {
			if dir == "teams" {
				teams = true
			} else if parts := strings.SplitN(p, "/", 3); len(parts) == 3 {
				set[parts[1]] = true
			}
		}
		for p, c := range a {
			if o, ok := b[p]; !ok || o != c {
				note(p)
			}
		}
		for p := range b {
			if _, ok := a[p]; !ok {
				note(p)
			}
		}
	}
	if teams {
		for _, r := range []string{baseRoot, root} {
			for _, n := range capability.Names(r) {
				set[n] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// RenderCap renders capName from root in memory (rendered/-relative path -> content); a capability
// absent from root yields no files.
func RenderCap(cfg capability.Config, root, capName string) (map[string]string, error) {
	if st, err := os.Stat(filepath.Join(root, "capabilities", capName)); err != nil || !st.IsDir() {
		return nil, nil
	}
	teams, err := capability.LoadTeams(root)
	if err != nil {
		return nil, err
	}
	return capability.Render(cfg, root, capName, teams)
}

// Diff renders caps from baseRoot and root in memory and returns the rendered files that differ,
// sorted by path. A working-side error is returned as is (diags); a base-side one is prefixed "base: ".
func Diff(cfg capability.Config, baseRoot, root string, caps []string) ([]FileDiff, error) {
	var out []FileDiff
	for _, c := range caps {
		now, err := RenderCap(cfg, root, c)
		if err != nil {
			return nil, err
		}
		was, err := RenderCap(cfg, baseRoot, c)
		if err != nil {
			return nil, fmt.Errorf("base: %w", err)
		}
		for p, n := range now {
			if o := was[p]; o != n {
				out = append(out, FileDiff{p, o, n})
			}
		}
		for p, o := range was {
			if _, ok := now[p]; !ok {
				out = append(out, FileDiff{p, o, ""})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// lines splits s into newline-terminated lines; "" = no lines, a missing final newline adds no blank line.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return difflib.SplitLines(strings.TrimSuffix(s, "\n"))
}

// Unified renders d as a unified diff (3 context lines) with a/rendered/ and b/rendered/ headers.
func Unified(d []FileDiff) string {
	var sb strings.Builder
	for _, f := range d {
		ud := difflib.UnifiedDiff{
			A:        lines(f.Old),
			B:        lines(f.New),
			FromFile: "a/rendered/" + f.Path,
			ToFile:   "b/rendered/" + f.Path,
			Context:  3,
		}
		if f.Old == "" {
			ud.FromFile = "/dev/null"
		}
		if f.New == "" {
			ud.ToFile = "/dev/null"
		}
		s, err := difflib.GetUnifiedDiffString(ud)
		if err != nil {
			continue
		}
		sb.WriteString(s)
	}
	return sb.String()
}

// Stat summarizes d: "<path> | +a -d" per file, then a total line.
func Stat(d []FileDiff) string {
	var sb strings.Builder
	ta, tr := 0, 0
	for _, f := range d {
		a, r := 0, 0
		m := difflib.NewMatcher(lines(f.Old), lines(f.New))
		for _, op := range m.GetOpCodes() {
			switch op.Tag {
			case 'r':
				a += op.J2 - op.J1
				r += op.I2 - op.I1
			case 'i':
				a += op.J2 - op.J1
			case 'd':
				r += op.I2 - op.I1
			}
		}
		ta, tr = ta+a, tr+r
		fmt.Fprintf(&sb, "%s | +%d -%d\n", f.Path, a, r)
	}
	fmt.Fprintf(&sb, "%d files changed, +%d -%d\n", len(d), ta, tr)
	return sb.String()
}
