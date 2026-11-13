// Package docs maintains docs/guide/ and docs/reference/: embed blocks spliced from the fixture
// (embed.go) and yaml examples checked against lint.
//
// A yaml fence must say what it is: path=<repo-relative file> (full file, must lint clean),
// path=… expect="<substring>" (lint must report a matching error) or fragment (not checked).
package docs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/lint"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"golang.org/x/sync/errgroup"
)

// GuideDir holds the hand-written guide pages, relative to the repo root.
const GuideDir = "docs/guide"

var (
	openRx   = regexp.MustCompile("^( *)```(\\w*)(.*)$")
	closeRx  = regexp.MustCompile("^ *```\\s*$")
	pathRx   = regexp.MustCompile(`(?:^|\s)path=(\S+)`)
	expectRx = regexp.MustCompile(`(?:^|\s)expect="([^"]*)"`)
	fragRx   = regexp.MustCompile(`(?:^|\s)fragment(?:\s|$)`)
)

type block struct {
	page     string // docs/guide/<page>.md
	line     int    // the opening fence
	path     string
	expect   string
	fragment bool
	body     string
}

// Run is gcd docs: rewrite embeds (Embeds; written only if write), lint the fixture they come from,
// then Check. No doc dir under root: nothing to do.
func Run(root string, write bool) (changed []string, diags []system.Diag, err error) {
	has := false
	for _, d := range Dirs {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err == nil && fi.IsDir() {
			has = true
		}
	}
	if !has {
		return nil, nil, nil
	}
	changed, diags, err = Embeds(root, write)
	if err != nil {
		return nil, nil, err
	}
	found, err := lint.All(filepath.Join(root, filepath.FromSlash(system.FixtureRoot)), nil)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range found {
		d.File = system.FixtureRoot + "/" + d.File
		diags = append(diags, d)
	}
	checked, err := Check(root)
	if err != nil {
		return nil, nil, err
	}
	return changed, system.SortDedupe(append(diags, checked...)), nil
}

// Check lints every annotated yaml block of the *.md pages in Dirs under root, each written over its own copy
// of the fixture (system.FixtureRoot). Diagnostics point into the markdown: the snippet's own file
// at fence line + its line, any other file at the fence line. No guide dir: nothing to check.
func Check(root string) ([]system.Diag, error) {
	var blocks []block
	var diags []system.Diag
	for _, dir := range Dirs {
		pages, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.md"))
		if err != nil {
			return nil, err
		}
		for _, p := range pages {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			bs, ds := parse(dir+"/"+filepath.Base(p), string(b))
			blocks = append(blocks, bs...)
			diags = append(diags, ds...)
		}
	}
	if len(blocks) == 0 && len(diags) == 0 {
		return nil, nil
	}
	fixture := os.DirFS(filepath.Join(root, filepath.FromSlash(system.FixtureRoot)))
	res := make([][]system.Diag, len(blocks))
	errs := make([]error, len(blocks))
	var g errgroup.Group
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, b := range blocks {
		g.Go(func() error {
			res[i], errs[i] = checkBlock(fixture, b)
			return nil
		})
	}
	_ = g.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	for _, d := range res {
		diags = append(diags, d...)
	}
	return system.SortDedupe(diags), nil
}

// parse extracts the yaml blocks to check; unannotated yaml blocks are diagnostics.
func parse(page, text string) ([]block, []system.Diag) {
	var blocks []block
	var diags []system.Diag
	lines := strings.Split(text, "\n")
	inEmbed := false
	for i := 0; i < len(lines); i++ {
		switch {
		case embedOpenRx.MatchString(lines[i]):
			inEmbed = true
		case embedCloseRx.MatchString(lines[i]):
			inEmbed = false
		}
		m := openRx.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		start := i
		var body strings.Builder
		for i++; i < len(lines) && !closeRx.MatchString(lines[i]); i++ {
			body.WriteString(strings.TrimPrefix(lines[i], m[1]) + "\n")
		}
		if m[2] != "yaml" || inEmbed {
			continue
		}
		b := block{page: page, line: start + 1, body: body.String(), fragment: fragRx.MatchString(m[3])}
		if pm := pathRx.FindStringSubmatch(m[3]); pm != nil {
			b.path = pm[1]
		}
		if em := expectRx.FindStringSubmatch(m[3]); em != nil {
			b.expect = em[1]
		}
		switch {
		case b.fragment:
		case b.path == "":
			diags = append(diags, b.at(b.line, "yaml block needs path=…, expect=… or fragment"))
		default:
			blocks = append(blocks, b)
		}
	}
	return blocks, diags
}

func (b block) at(line int, msg string) system.Diag {
	return system.Diag{File: b.page, Line: line, Col: 1, Msg: msg}
}

func checkBlock(fixture fs.FS, b block) ([]system.Diag, error) {
	segs := strings.Split(b.path, "/")
	if !filepath.IsLocal(filepath.FromSlash(b.path)) || path.Clean(b.path) != b.path || len(segs) < 2 ||
		(segs[0] != "systems" && segs[0] != "teams") || (segs[0] == "systems" && len(segs) < 3) {
		return []system.Diag{b.at(b.line, "path must be under systems/ or teams/")}, nil
	}
	tmp, err := os.MkdirTemp("", "gcd-docs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.CopyFS(tmp, fixture); err != nil {
		return nil, err
	}
	dst := filepath.Join(tmp, filepath.FromSlash(b.path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst, []byte(b.body), 0o644); err != nil {
		return nil, err
	}
	var systems []string
	if segs[0] == "systems" {
		systems = []string{segs[1]}
	}
	found, err := lint.All(tmp, systems)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: %w", b.page, b.line, err)
	}
	var out []system.Diag
	for _, d := range found {
		if d.File == b.path {
			out = append(out, system.Diag{File: b.page, Line: b.line + max(d.Line, 1), Col: d.Col, Label: d.Label, Msg: d.Msg})
			continue
		}
		msg := d.Msg
		if d.Label != "" {
			msg = d.Label + ": " + msg
		}
		out = append(out, b.at(b.line, d.File+": "+msg))
	}
	if b.expect == "" {
		return out, nil
	}
	var got []string
	for _, d := range out {
		if strings.Contains(d.Label+": "+d.Msg, b.expect) {
			return nil, nil
		}
		got = append(got, d.String())
	}
	if len(got) == 0 {
		got = []string{"no errors"}
	}
	return []system.Diag{b.at(b.line, fmt.Sprintf("expected lint error containing %q, got: %s", b.expect, strings.Join(got, "; ")))}, nil
}
