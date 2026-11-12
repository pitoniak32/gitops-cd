package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
)

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// ColorDiff paints finished Unified / FormatFiles text: file headers (`# rendered/`
// lines and ---/+++ pairs) bold, @@ cyan, + green, - red. Hunk line counts decide where a hunk ends,
// so YAML `---` separators and full-file content outside hunks stay plain.
func ColorDiff(text string, p color.P) string {
	if !p.On || text == "" {
		return text
	}
	ls := strings.SplitAfter(text, "\n")
	var b strings.Builder
	oldN, newN := 0, 0
	// paint colors the line but not its newline
	paint := func(f func(string) string, l string) {
		body := strings.TrimSuffix(l, "\n")
		b.WriteString(f(body) + l[len(body):])
	}
	for i := 0; i < len(ls); i++ {
		l := ls[i]
		if oldN > 0 || newN > 0 {
			switch {
			case strings.HasPrefix(l, "+"):
				newN--
				paint(p.Green, l)
			case strings.HasPrefix(l, "-"):
				oldN--
				paint(p.Red, l)
			default:
				oldN--
				newN--
				b.WriteString(l)
			}
			continue
		}
		switch {
		case strings.HasPrefix(l, "--- ") && i+1 < len(ls) && strings.HasPrefix(ls[i+1], "+++ "):
			paint(p.Bold, l)
			paint(p.Bold, ls[i+1])
			i++
		case strings.HasPrefix(l, "# rendered/"):
			paint(p.Bold, l)
		case hunkRe.MatchString(l):
			m := hunkRe.FindStringSubmatch(l)
			oldN, newN = hunkLen(m[1]), hunkLen(m[2])
			paint(p.Cyan, l)
		default:
			b.WriteString(l)
		}
	}
	return b.String()
}

func hunkLen(s string) int {
	if s == "" {
		return 1
	}
	var n int
	fmt.Sscan(s, &n)
	return n
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
