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

// EvaluateDir holds the evaluator pages (why gitops-cd), relative to the repo root.
const EvaluateDir = "docs/evaluate"

// Dirs are the doc dirs gcd docs maintains: embeds rewritten, yaml fences checked.
var Dirs = []string{GuideDir, ReferenceDir, EvaluateDir}

var (
	embedOpenRx  = regexp.MustCompile(`^( *)<!-- embed(.*?) -->$`)
	embedCloseRx = regexp.MustCompile(`^ *<!-- /embed -->$`)
	fenceRx      = regexp.MustCompile("^ *```")
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

// Embed replaces the body of each (optionally indented) <!-- embed path=<p> [lines=A-B] --> … <!-- /embed --> block in text
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
		body, err := embedBody(page, m[2], fixture)
		if err != nil {
			diags = append(diags, at(i, err.Error()))
			out = append(out, lines[i:end+1]...)
		} else {
			out = append(out, l)
			for _, b := range body { // indented like the marker (e.g. inside a list item)
				if b != "" {
					b = m[1] + b
				}
				out = append(out, b)
			}
			out = append(out, lines[end])
		}
		i = end
	}
	return strings.Join(out, "\n"), diags
}

// embedBody returns the lines between the markers for the attributes attrs.
func embedBody(page, attrs string, fixture fs.FS) ([]string, error) {
	kv, err := embedAttrs(attrs)
	if err != nil {
		return nil, err
	}
	p := kv["path"]
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
	hunks := []hunk{{a: 1, z: len(src)}}
	label, anchor := strings.TrimPrefix(p, "capabilities/"), ""
	if rng, ok := kv["lines"]; ok {
		if hunks, err = parseHunks(rng, len(src)); err != nil {
			return nil, err
		}
		var spans []hunk // touching hunks merged, for the label
		for _, h := range hunks {
			if k := len(spans) - 1; k >= 0 && h.a == spans[k].z+1 {
				spans[k].z = h.z
				continue
			}
			spans = append(spans, h)
		}
		var parts []string
		for _, h := range spans {
			parts = append(parts, h.String())
		}
		label += ":" + strings.Join(parts, ",")
		anchor = fmt.Sprintf("#L%d-L%d", hunks[0].a, hunks[len(hunks)-1].z)
	}
	lang := strings.TrimPrefix(path.Ext(p), ".")
	if lang == "yml" {
		lang = "yaml"
	}
	link := strings.Repeat("../", strings.Count(page, "/")) + capability.FixtureRoot + "/" + p + anchor
	out := append([]string{"```" + lang}, renderHunks(src, hunks)...)
	return append(out, "```", "", "Source: [`"+label+"`]("+link+")"), nil
}

// embedAttrs parses space-separated key=value pairs; a value may be "double quoted" (no escapes).
func embedAttrs(s string) (map[string]string, error) {
	kv := map[string]string{}
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		end := strings.IndexAny(s, " =")
		if end < 0 || s[end] != '=' {
			k, _, _ := strings.Cut(s, " ")
			return nil, fmt.Errorf("unknown embed attribute %q", k)
		}
		k := s[:end]
		if k != "path" && k != "lines" {
			return nil, fmt.Errorf("unknown embed attribute %q", k)
		}
		s = s[end+1:]
		var v string
		if strings.HasPrefix(s, `"`) {
			i := strings.Index(s[1:], `"`)
			if i < 0 {
				return nil, errors.New("unterminated quote")
			}
			v, s = s[1:i+1], s[i+2:]
		} else {
			v, s, _ = strings.Cut(s, " ")
		}
		kv[k] = v
	}
	return kv, nil
}

// hunk is lines a..z (1-based, inclusive) of the source, with an optional comment above it.
type hunk struct {
	a, z int
	text string
}

func (h hunk) String() string {
	if h.a == h.z {
		return strconv.Itoa(h.a)
	}
	return fmt.Sprintf("%d-%d", h.a, h.z)
}

var (
	rangeRx = regexp.MustCompile(`^(\d+)(?:-(\d+))?$`)
	optRx   = regexp.MustCompile(`^(\w+)='([^']*)'$`)
)

// parseHunks parses lines="A-B,N,(A-B;text='comment'),…" for a source of n lines: hunks in order,
// without overlap.
func parseHunks(s string, n int) ([]hunk, error) {
	parts, err := splitTop(s, ',')
	if err != nil {
		return nil, err
	}
	var out []hunk
	for _, part := range parts {
		part = strings.TrimSpace(part)
		rng, opts := part, []string(nil)
		if strings.HasPrefix(part, "(") {
			if !strings.HasSuffix(part, ")") {
				return nil, errors.New("unbalanced parentheses")
			}
			fields, _ := splitTop(part[1:len(part)-1], ';')
			rng, opts = strings.TrimSpace(fields[0]), fields[1:]
		}
		m := rangeRx.FindStringSubmatch(rng)
		var h hunk
		if m != nil {
			h.a, _ = strconv.Atoi(m[1])
			h.z = h.a
			if m[2] != "" {
				h.z, _ = strconv.Atoi(m[2])
			}
		}
		if m == nil || h.a < 1 || h.a > h.z || h.z > n {
			return nil, fmt.Errorf("lines must be A-B with 1 <= A <= B <= %d", n)
		}
		for _, o := range opts {
			om := optRx.FindStringSubmatch(strings.TrimSpace(o))
			switch {
			case om == nil:
				return nil, fmt.Errorf("hunk options are key='value': %s", part)
			case om[1] != "text":
				return nil, fmt.Errorf("unknown hunk option %q", om[1])
			}
			h.text = om[2]
		}
		if len(out) > 0 && h.a <= out[len(out)-1].z {
			return nil, fmt.Errorf("hunks must be in order without overlap: %s", s)
		}
		out = append(out, h)
	}
	return out, nil
}

// splitTop splits s on sep outside parentheses and 'single quotes'.
func splitTop(s string, sep byte) ([]string, error) {
	var out []string
	depth, quoted, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	switch {
	case quoted:
		return nil, errors.New("unterminated quote")
	case depth != 0:
		return nil, errors.New("unbalanced parentheses")
	}
	return append(out, s[start:]), nil
}

// renderHunks returns the hunks' lines dedented together; a hunk after a gap starts with "# …", and
// its text as "# <text>", both at the indent of its first line.
func renderHunks(src []string, hunks []hunk) []string {
	var all []string
	for _, h := range hunks {
		all = append(all, src[h.a-1:h.z]...)
	}
	all = dedent(all)
	var out []string
	i := 0
	for k, h := range hunks {
		lines := all[i : i+h.z-h.a+1]
		i += len(lines)
		ind := ""
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				ind = l[:len(l)-len(strings.TrimLeft(l, " "))]
				break
			}
		}
		if k > 0 && h.a > hunks[k-1].z+1 {
			out = append(out, ind+"# …")
		}
		if h.text != "" {
			out = append(out, ind+"# "+h.text)
		}
		out = append(out, lines...)
	}
	return out
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
