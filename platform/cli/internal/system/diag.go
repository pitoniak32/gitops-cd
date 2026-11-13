package system

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Diag is one diagnostic: <file>:<line>:<col>: [<label>: ]<message>, file relative to the repo root.
// Label is the dotted path of the offending value inside the file.
type Diag struct {
	File  string `json:"file"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Label string `json:"label,omitempty"`
	Msg   string `json:"msg"`
}

func (d Diag) String() string {
	label := ""
	if d.Label != "" {
		label = d.Label + ": "
	}
	return fmt.Sprintf("%s:%d:%d: %s%s", d.File, d.Line, d.Col, label, d.Msg)
}

// SortDedupe orders diagnostics by file, line, column, text and drops repeats.
func SortDedupe(d []Diag) []Diag {
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

// Error joins diagnostics into one error, one per line.
type Error []Diag

func (e Error) Error() string {
	l := make([]string, len(e))
	for i, d := range e {
		l[i] = d.String()
	}
	return strings.Join(l, "\n")
}

// DiagsOf collects the diagnostics of every Error in err's tree (wrapped or joined), in order.
func DiagsOf(err error) []Diag {
	var out []Diag
	var walk func(error)
	walk = func(e error) {
		switch x := e.(type) {
		case nil:
		case Error:
			out = append(out, x...)
		case interface{ Unwrap() []error }:
			for _, c := range x.Unwrap() {
				walk(c)
			}
		case interface{ Unwrap() error }:
			walk(x.Unwrap())
		}
	}
	walk(err)
	return out
}

// reporter collects diagnostics for one file.
type reporter struct {
	f     *file
	diags *[]Diag
}

// at reports msg for the value at path. where is the node to point at: a map field's key, a list
// item, or nil for the nearest thing that exists (walking path from the document).
func (r reporter) at(where *yaml.Node, path []string, msg string) {
	if where == nil || where.Line == 0 {
		where = r.nearest(path)
	}
	*r.diags = append(*r.diags, Diag{File: r.f.path, Line: where.Line, Col: where.Column, Label: strings.Join(path, "."), Msg: msg})
}

// nearest is the key (or item) of the deepest existing prefix of path, the document for none.
func (r reporter) nearest(path []string) *yaml.Node {
	best := r.f.doc
	if best == nil || best.Line == 0 {
		best = &yaml.Node{Line: 1, Column: 1}
	}
	n := r.f.doc
	for _, s := range path {
		n = deref(n)
		if n == nil {
			break
		}
		if n.Kind == yaml.SequenceNode {
			i := 0
			if _, err := fmt.Sscanf(s, "%d", &i); err != nil || i >= len(n.Content) {
				break
			}
			n = n.Content[i]
			best = n
			continue
		}
		k, v := getKV(n, s)
		if k == nil {
			break
		}
		best, n = k, v
	}
	return best
}

func join(path []string, more ...string) []string {
	return append(append([]string{}, path...), more...)
}
