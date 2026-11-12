// Package color paints human output with plain ANSI codes. The zero P paints nothing.
package color

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// P paints strings; the zero value (off) returns them unchanged.
type P struct{ On bool }

func (p P) wrap(code, s string) string {
	if !p.On || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p P) Bold(s string) string   { return p.wrap("1", s) }
func (p P) Dim(s string) string    { return p.wrap("2", s) }
func (p P) Red(s string) string    { return p.wrap("31", s) }
func (p P) Green(s string) string  { return p.wrap("32", s) }
func (p P) Yellow(s string) string { return p.wrap("33", s) }
func (p P) Blue(s string) string   { return p.wrap("34", s) }
func (p P) Cyan(s string) string   { return p.wrap("36", s) }

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool { return f != nil && term.IsTerminal(int(f.Fd())) }

// Detect resolves --color (auto|always|never) for output w: auto = w is a terminal and NO_COLOR is
// unset or empty.
func Detect(mode string, w io.Writer) (P, error) {
	switch mode {
	case "always":
		return P{true}, nil
	case "never":
		return P{}, nil
	case "auto", "":
		f, _ := w.(*os.File)
		return P{IsTerminal(f) && os.Getenv("NO_COLOR") == ""}, nil
	}
	return P{}, fmt.Errorf("invalid --color %q: want auto, always or never", mode)
}
