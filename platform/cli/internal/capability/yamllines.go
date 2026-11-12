package capability

import "strings"

// yamlLines collects YAML lines, each with an optional trailing comment, and writes the comments in
// one aligned column.
type yamlLines struct{ ls []yamlLine }

type yamlLine struct{ text, comment string }

// maxPlain: lines longer than this don't widen the comment column.
const maxPlain = 72

func (y *yamlLines) add(indent int, text, comment string) {
	y.ls = append(y.ls, yamlLine{strings.Repeat(" ", indent) + text, comment})
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
		b.WriteString(l.text)
		if l.comment != "" {
			pad := 2
			if len(l.text) < w {
				pad = w - len(l.text) + 2
			}
			b.WriteString(strings.Repeat(" ", pad) + "# " + l.comment)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
