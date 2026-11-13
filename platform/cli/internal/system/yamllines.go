package system

import (
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
)

// yamlLines collects YAML lines, each with an optional trailing comment, and writes the comments in
// one aligned column.
type yamlLines struct {
	ls []yamlLine
	p  color.P
}

// text is the plain line (widths are computed on it); ctext the same line painted.
type yamlLine struct{ text, ctext, comment string }

// maxPlain: lines longer than this don't widen the comment column.
const maxPlain = 72

func (y *yamlLines) add(indent int, text, comment string) {
	y.addC(indent, text, text, comment)
}

// addC is add with a painted version of text (same visible characters).
func (y *yamlLines) addC(indent int, text, ctext, comment string) {
	sp := strings.Repeat(" ", indent)
	y.ls = append(y.ls, yamlLine{sp + text, sp + ctext, comment})
}

// String pads each comment to the widest commented line (those over maxPlain chars excepted, which
// just get two spaces).
func (y *yamlLines) String() string {
	w := 0
	for _, l := range y.ls {
		if n := len(l.text); l.comment != "" && n <= maxPlain && n > w {
			w = n
		}
	}
	var b strings.Builder
	for _, l := range y.ls {
		b.WriteString(l.ctext)
		if l.comment != "" {
			pad := 2
			if len(l.text) < w {
				pad = w - len(l.text) + 2
			}
			b.WriteString(strings.Repeat(" ", pad) + y.p.Dim("# "+l.comment))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
