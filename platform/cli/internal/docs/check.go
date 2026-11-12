// Package docs checks the yaml examples in docs/guide/ against lint.
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

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/lint"
	"golang.org/x/sync/errgroup"
)

// GuideDir holds the hand-written guide pages, relative to the repo root.
const GuideDir = "docs/guide"

var (
	openRx   = regexp.MustCompile("^( *)```(\\w+)(.*)$")
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

// Check lints every annotated yaml block of docs/guide/*.md under root, each written over its own copy
// of the fixture (capability.FixtureRoot). Diagnostics point into the markdown: the snippet's own file
// at fence line + its line, any other file at the fence line. No guide dir: nothing to check.
func Check(root string) ([]capability.Diag, error) {
	pages, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(GuideDir), "*.md"))
	if err != nil {
		return nil, err
	}
	var blocks []block
	var diags []capability.Diag
	for _, p := range pages {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		bs, ds := parse(GuideDir+"/"+filepath.Base(p), string(b))
		blocks = append(blocks, bs...)
		diags = append(diags, ds...)
	}
	if len(blocks) == 0 && len(diags) == 0 {
		return nil, nil
	}
	fixture := os.DirFS(filepath.Join(root, filepath.FromSlash(capability.FixtureRoot)))
	res := make([][]capability.Diag, len(blocks))
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
	return capability.SortDedupe(diags), nil
}

// parse extracts the yaml blocks to check; unannotated yaml blocks are diagnostics.
func parse(page, text string) ([]block, []capability.Diag) {
	var blocks []block
	var diags []capability.Diag
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := openRx.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		start := i
		var body strings.Builder
		for i++; i < len(lines) && !closeRx.MatchString(lines[i]); i++ {
			body.WriteString(strings.TrimPrefix(lines[i], m[1]) + "\n")
		}
		if m[2] != "yaml" {
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

func (b block) at(line int, msg string) capability.Diag {
	return capability.Diag{File: b.page, Line: line, Col: 1, Msg: msg}
}

func checkBlock(fixture fs.FS, b block) ([]capability.Diag, error) {
	segs := strings.Split(b.path, "/")
	if !filepath.IsLocal(filepath.FromSlash(b.path)) || path.Clean(b.path) != b.path || len(segs) < 2 ||
		(segs[0] != "capabilities" && segs[0] != "teams") || (segs[0] == "capabilities" && len(segs) < 3) {
		return []capability.Diag{b.at(b.line, "path must be under capabilities/ or teams/")}, nil
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
	var caps []string
	if segs[0] == "capabilities" {
		caps = []string{segs[1]}
	}
	found, err := lint.All(tmp, caps)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: %w", b.page, b.line, err)
	}
	var out []capability.Diag
	for _, d := range found {
		if d.File == b.path {
			out = append(out, capability.Diag{File: b.page, Line: b.line + max(d.Line, 1), Col: d.Col, Label: d.Label, Msg: d.Msg})
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
	return []capability.Diag{b.at(b.line, fmt.Sprintf("expected lint error containing %q, got: %s", b.expect, strings.Join(got, "; ")))}, nil
}
