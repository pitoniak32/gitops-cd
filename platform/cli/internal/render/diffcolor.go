package render

import (
	"regexp"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
)

var (
	fromRe = regexp.MustCompile(`^--- (a/rendered/|/dev/null)`)
	toRe   = regexp.MustCompile(`^\+\+\+ (b/rendered/|/dev/null)`)
)

// ColorDiff paints finished Unified / FormatSelectedDiff / FormatFiles text: file headers (`# rendered/`
// lines and ---/+++ pairs) bold, then + green and - red until the next header, so full-file content
// under a `# rendered/` header stays plain.
func ColorDiff(text string, p color.P) string {
	if !p.On || text == "" {
		return text
	}
	ls := strings.SplitAfter(text, "\n")
	var b strings.Builder
	inDiff := false
	// paint colors the line but not its newline
	paint := func(f func(string) string, l string) {
		body := strings.TrimSuffix(l, "\n")
		b.WriteString(f(body) + l[len(body):])
	}
	for i := 0; i < len(ls); i++ {
		l := ls[i]
		switch {
		case fromRe.MatchString(l) && i+1 < len(ls) && toRe.MatchString(ls[i+1]):
			paint(p.Bold, l)
			paint(p.Bold, ls[i+1])
			i++
			inDiff = true
		case strings.HasPrefix(l, "# rendered/"):
			paint(p.Bold, l)
			inDiff = false
		case inDiff && strings.HasPrefix(l, "+"):
			paint(p.Green, l)
		case inDiff && strings.HasPrefix(l, "-"):
			paint(p.Red, l)
		default:
			b.WriteString(l)
		}
	}
	return b.String()
}

var statRe = regexp.MustCompile(`(\+\d+) (-\d+)$`)

// ColorStat paints the +added -removed counts of Stat output.
func ColorStat(text string, p color.P) string {
	if !p.On {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if m := statRe.FindStringSubmatchIndex(l); m != nil {
			lines[i] = l[:m[0]] + p.Green(l[m[2]:m[3]]) + " " + p.Red(l[m[4]:m[5]])
		}
	}
	return strings.Join(lines, "\n")
}
