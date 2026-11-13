package docs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
)

// ReferenceDir holds the hand-written reference pages, relative to the repo root.
const ReferenceDir = "docs/reference"

// Dirs are the doc dirs gcd docs maintains: embeds rewritten, yaml fences checked.
var Dirs = []string{GuideDir, ReferenceDir}

var (
	embedOpenRx  = regexp.MustCompile(`^<!-- embed(.*?) -->$`)
	embedCloseRx = regexp.MustCompile(`^<!-- /embed -->$`)
	fenceRx      = regexp.MustCompile("^ *```")
	linesRx      = regexp.MustCompile(`^(\d+)-(\d+)$`)
)

// Embeds rewrites the embed blocks of every *.md in Dirs under root from the fixture
// (capability.FixtureRoot). changed lists the repo-relative pages whose text differs; they are
// written only if write. Missing dirs are skipped.
func Embeds(root string, write bool) (changed []string, diags []capability.Diag, err error) {
	fixture := os.DirFS(filepath.Join(root, filepath.FromSlash(capability.FixtureRoot)))
	for _, dir := range Dirs {
		pages, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.md"))
		if err != nil {
			return nil, nil, err
		}
		for _, p := range pages {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, nil, err
			}
			page := dir + "/" + filepath.Base(p)
			out, d := Embed(page, string(b), fixture)
			diags = append(diags, d...)
			if out == string(b) {
				continue
			}
			changed = append(changed, page)
			if write {
				if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	return changed, diags, nil
}

// Embed replaces the body of each <!-- embed path=<p> [lines=A-B] --> … <!-- /embed --> block in text
// (page = its repo-relative path) with a fence of those lines of the fixture file p, dedented, and a
// source link. Markers inside code fences are ignored. A broken block is left as is, with a diag at
// its opening marker.
func Embed(page, text string, fixture fs.FS) (string, []capability.Diag) {
	at := func(i int, msg string) capability.Diag {
		return capability.Diag{File: page, Line: i + 1, Col: 1, Msg: msg}
	}
	lines := strings.Split(text, "\n")
	var out []string
	var diags []capability.Diag
	inFence := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if fenceRx.MatchString(l) {
			inFence = !inFence
		}
		if inFence {
			out = append(out, l)
			continue
		}
		if embedCloseRx.MatchString(l) {
			diags = append(diags, at(i, "/embed without embed"))
			out = append(out, l)
			continue
		}
		m := embedOpenRx.FindStringSubmatch(l)
		if m == nil {
			out = append(out, l)
			continue
		}
		end, msg := -1, ""
		for j := i + 1; j < len(lines) && end < 0 && msg == ""; j++ {
			switch {
			case embedCloseRx.MatchString(lines[j]):
				end = j
			case embedOpenRx.MatchString(lines[j]):
				msg = "embed inside embed"
			}
		}
		if end < 0 && msg == "" {
			msg = "embed without <!-- /embed -->"
		}
		if msg != "" {
			diags = append(diags, at(i, msg))
			out = append(out, lines[i:]...)
			break
		}
		body, err := embedBody(page, m[1], fixture)
		if err != nil {
			diags = append(diags, at(i, err.Error()))
			out = append(out, lines[i:end+1]...)
		} else {
			out = append(append(append(out, l), body...), lines[end])
		}
		i = end
	}
	return strings.Join(out, "\n"), diags
}

// embedBody returns the lines between the markers for the attributes attrs.
func embedBody(page, attrs string, fixture fs.FS) ([]string, error) {
	var p, rng string
	for _, f := range strings.Fields(attrs) {
		k, v, _ := strings.Cut(f, "=")
		switch k {
		case "path":
			p = v
		case "lines":
			rng = v
		default:
			return nil, fmt.Errorf("unknown embed attribute %q", k)
		}
	}
	if p == "" {
		return nil, errors.New("embed needs path=")
	}
	if !filepath.IsLocal(filepath.FromSlash(p)) || path.Clean(p) != p {
		return nil, errors.New("path must be a file under the fixture")
	}
	b, err := fs.ReadFile(fixture, p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: no such file in the fixture", p)
	}
	if err != nil {
		return nil, err
	}
	src := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	label, anchor := strings.TrimPrefix(p, "capabilities/"), ""
	if rng != "" {
		m := linesRx.FindStringSubmatch(rng)
		var a, z int
		if m != nil {
			a, _ = strconv.Atoi(m[1])
			z, _ = strconv.Atoi(m[2])
		}
		if m == nil || a < 1 || a > z || z > len(src) {
			return nil, fmt.Errorf("lines must be A-B with 1 <= A <= B <= %d", len(src))
		}
		src = src[a-1 : z]
		label += fmt.Sprintf(":%d-%d", a, z)
		anchor = fmt.Sprintf("#L%d-L%d", a, z)
	}
	lang := strings.TrimPrefix(path.Ext(p), ".")
	if lang == "yml" {
		lang = "yaml"
	}
	link := strings.Repeat("../", strings.Count(page, "/")) + capability.FixtureRoot + "/" + p + anchor
	out := append([]string{"```" + lang}, dedent(src)...)
	return append(out, "```", "", "Source: [`"+label+"`]("+link+")"), nil
}

func dedent(ls []string) []string {
	n := -1
	for _, l := range ls {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if k := len(l) - len(strings.TrimLeft(l, " ")); n < 0 || k < n {
			n = k
		}
	}
	out := make([]string, len(ls))
	for i, l := range ls {
		switch {
		case strings.TrimSpace(l) == "":
			l = ""
		case n > 0:
			l = l[n:]
		}
		out[i] = l
	}
	return out
}
