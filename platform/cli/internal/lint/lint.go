// Package lint validates capability YAML with the full CUE rules and reports diagnostics as
// <file>:<line>:<col>: [<label>: ]<message>, paths relative to the repo root.
package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	cueerrors "cuelang.org/go/cue/errors"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cueload"
)

// Diag is one diagnostic.
type Diag struct {
	File      string
	Line, Col int
	Label     string
	Msg       string
}

func (d Diag) String() string {
	label := ""
	if d.Label != "" {
		label = d.Label + ": "
	}
	return fmt.Sprintf("%s:%d:%d: %s%s", d.File, d.Line, d.Col, label, d.Msg)
}

// raw is one CUE error as `cue vet` prints it: header line "<path>: <msg>[:]" + first YAML position.
type raw struct {
	header     string // full header line
	path, msg  string
	yfile      string // absolute file of the first .yml/.yaml position ("" = none)
	yline, ycl int
}

var yamlErrRe = regexp.MustCompile(`^(\S+\.ya?ml):(\d+)(?::(\d+))?: (.+)$`)

var yamlExt = regexp.MustCompile(`\.ya?ml$`)

func collect(err error, cwd string) []raw {
	cfg := &cueerrors.Config{OmitPath: true, Cwd: cwd}
	var out []raw
	for _, e := range cueerrors.Errors(cueerrors.Sanitize(cueerrors.Promote(err, ""))) {
		r := raw{}
		msg := cueerrors.StringWithConfig(e, cfg)
		r.header = msg
		if p := strings.Join(e.Path(), "."); p != "" {
			r.header = p + ": " + msg
		}
		pos := cueerrors.Positions(e)
		if len(pos) > 0 {
			r.header += ":"
		}
		for _, p := range pos {
			pp := p.Position()
			if pp.Line > 0 && yamlExt.MatchString(pp.Filename) {
				r.yfile, r.yline, r.ycl = pp.Filename, pp.Line, pp.Column
				break
			}
		}
		out = append(out, r)
	}
	return out
}

// Capability lints one capability under root. The error is for I/O, non-CUE load failures, and
// CUE errors none of which map to a capability YAML path (lint.sh exited 1 with no output there).
func Capability(root, capName string) ([]Diag, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(filepath.Join(root, "capabilities", capName)); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no capability dir %s", filepath.Join(root, "capabilities", capName))
	}
	v, err := cueload.Capability(root, capName)
	if err != nil {
		return loadDiags(root, err)
	}
	err = v.Validate(cue.Concrete(true))
	if err == nil {
		return nil, nil
	}
	var ce cueerrors.Error
	if !errors.As(err, &ce) {
		return nil, err
	}
	all := collect(err, cueload.RenderDir(root))

	// keep: real errors (not disjunction headers), not nested under another reported path
	var kept []raw
	for _, r := range all {
		if strings.HasSuffix(r.header, "errors in empty disjunction:") || !strings.Contains(r.header, ": ") {
			continue
		}
		i := strings.Index(r.header, ": ")
		r.path, r.msg = r.header[:i], strings.TrimSuffix(r.header[i+2:], ":")
		kept = append(kept, r)
	}
	var diags []Diag
	l := locator{root: root, cap: capName}
	for i, r := range kept {
		nested := false
		for j, o := range kept {
			if j != i && strings.HasPrefix(r.path, o.path+".") {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		// disjunction noise ("conflicting values "dev" and "qa"") when a custom error exists
		if strings.HasPrefix(r.msg, "conflicting values") && hasOther(all, r.path+": ") {
			continue
		}
		loc, ok := l.locate(r.path)
		if !ok {
			continue
		}
		d := Diag{File: loc.file, Label: loc.label, Msg: r.msg, Line: 1, Col: 1}
		if r.yfile != "" {
			if rel, err := filepath.Rel(root, r.yfile); err == nil && !strings.HasPrefix(rel, "..") {
				d.File, d.Line, d.Col = filepath.ToSlash(rel), r.yline, r.ycl
				diags = append(diags, d)
				continue
			}
		}
		if line, col, ok := loc.find(filepath.Join(root, loc.file)); ok {
			d.Line, d.Col = line, col
		}
		diags = append(diags, d)
	}
	if len(diags) == 0 {
		// lint.sh failed (exit 1, no lines) here; never pass silently
		return nil, fmt.Errorf("CUE errors with no locatable YAML path:\n%s", strings.TrimSpace(cueerrors.Details(err, nil)))
	}
	return sortDedupe(diags), nil
}

// loadDiags turns a load/YAML-syntax error into diagnostics at the error's own position (path
// relative to root). Errors without any position pass through as plain errors.
func loadDiags(root string, err error) ([]Diag, error) {
	var ce cueerrors.Error
	if !errors.As(err, &ce) {
		// YAML syntax errors arrive as "<abs file>:<line>[:<col>]: <message>"
		if m := yamlErrRe.FindStringSubmatch(err.Error()); m != nil {
			line, _ := strconv.Atoi(m[2])
			col := 1
			if m[3] != "" {
				col, _ = strconv.Atoi(m[3])
			}
			f := m[1]
			if rel, rerr := filepath.Rel(root, f); filepath.IsAbs(f) && rerr == nil {
				f = rel
			}
			return []Diag{{File: filepath.ToSlash(f), Line: line, Col: col, Msg: m[4]}}, nil
		}
		return nil, err
	}
	var diags []Diag
	for _, e := range cueerrors.Errors(err) {
		var pos *struct {
			file      string
			line, col int
		}
		for _, p := range cueerrors.Positions(e) {
			pp := p.Position()
			if pp.Line > 0 && pp.Filename != "" {
				pos = &struct {
					file      string
					line, col int
				}{pp.Filename, pp.Line, pp.Column}
				break
			}
		}
		if pos == nil {
			return nil, err
		}
		f := pos.file
		if filepath.IsAbs(f) {
			if rel, rerr := filepath.Rel(root, f); rerr == nil {
				f = rel
			}
		}
		format, args := e.Msg()
		diags = append(diags, Diag{File: filepath.ToSlash(f), Line: pos.line, Col: pos.col, Msg: fmt.Sprintf(format, args...)})
	}
	return sortDedupe(diags), nil
}

func hasOther(all []raw, needle string) bool {
	for _, o := range all {
		if !strings.Contains(o.header, "conflicting values") && strings.Contains(o.header, needle) {
			return true
		}
	}
	return false
}

func sortDedupe(d []Diag) []Diag {
	sort.SliceStable(d, func(i, j int) bool {
		a, b := d[i], d[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return a.String() < b.String()
	})
	out := d[:0]
	for i, x := range d {
		if i > 0 && x.String() == out[len(out)-1].String() {
			continue
		}
		out = append(out, x)
	}
	return out
}

// All lints caps (default: every capabilities/* dir), sorted by file, line, col and deduped
// (teams/ errors show up once per capability).
func All(root string, caps []string) ([]Diag, error) {
	if len(caps) == 0 {
		dirs, _ := filepath.Glob(filepath.Join(root, "capabilities", "*"))
		sort.Strings(dirs)
		for _, d := range dirs {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				caps = append(caps, filepath.Base(d))
			}
		}
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
	return sortDedupe(all), errors.Join(errs...)
}
