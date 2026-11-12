package render

import (
	"os"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
)

func TestColorDiff(t *testing.T) {
	in := strings.Join([]string{
		"# rendered/a/full.yaml",
		"kind: A",
		"---",
		"- gone",
		"--- looks like a header",
		"---",
		"--- a/rendered/a/x.yaml",
		"+++ b/rendered/a/x.yaml",
		"--- removed line starting with dashes",
		"+new",
		"-old",
		"+new2",
		"# rendered/a/other.yaml",
		"---",
		"x: 1",
		"",
	}, "\n")
	got := ColorDiff(in, color.P{On: true})
	want := os.Getenv("UPDATE")
	const golden = "testdata/golden-diffcolor.txt"
	if want == "1" {
		os.WriteFile(golden, []byte(got), 0o644)
	}
	w, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(w) {
		t.Errorf("differs from %s:\n%q", golden, got)
	}
	// stripping the codes gives the input back; off is identity
	if strip := stripANSI(got); strip != in {
		t.Errorf("text changed:\n%q", strip)
	}
	if ColorDiff(in, color.P{}) != in {
		t.Error("off changed text")
	}
	// the separator and full-file content stay plain
	for _, plain := range []string{"\nkind: A\n---\n", "\n---\nx: 1\n"} {
		if !strings.Contains(got, plain) {
			t.Errorf("want plain %q", plain)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestColorStat(t *testing.T) {
	got := ColorStat("a.yaml | +2 -1\n1 files changed, +2 -1\n", color.P{On: true})
	if !strings.Contains(got, "\x1b[32m+2\x1b[0m \x1b[31m-1\x1b[0m\n") || strings.Count(got, "\x1b[32m") != 2 {
		t.Errorf("%q", got)
	}
}
