package yamledit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const src = "# yaml-language-server: $schema=../x.json\n\ncodeOwners: [team1]\n# about promoters\npromoters:\n    dev: [a@example.com]\n"

func write(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.yml")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetKeepsCommentsAndIndent(t *testing.T) {
	t.Parallel()
	f := write(t, src)
	if err := Set(f, []string{"promoters", "dev"}, []string{"b@example.com"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, f)
	for _, w := range []string{"# yaml-language-server: $schema=../x.json", "# about promoters", "\n  dev:\n    - b@example.com\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestSetCreatesMissing(t *testing.T) {
	t.Parallel()
	f := write(t, src)
	if err := Set(f, []string{"apps", "worker", "viewers"}, []string{"c@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f); !strings.Contains(got, "apps:\n  worker:\n    viewers:\n      - c@example.com\n") {
		t.Errorf("not created:\n%s", got)
	}
	// replacing a scalar with a map on the way
	if err := Set(f, []string{"s"}, "v"); err != nil {
		t.Fatal(err)
	}
	if err := Set(f, []string{"s", "x"}, "z"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f); !strings.Contains(got, "s:\n  x: z\n") {
		t.Errorf("scalar not replaced by map:\n%s", got)
	}
}

func TestSetEmptyFile(t *testing.T) {
	t.Parallel()
	f := write(t, "")
	if err := Set(f, []string{"a", "b"}, 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f); got != "a:\n  b: 1\n" {
		t.Errorf("got %q", got)
	}
}

func TestDel(t *testing.T) {
	t.Parallel()
	f := write(t, src)
	if err := Del(f, []string{"promoters", "dev"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f); strings.Contains(got, "a@example.com") || !strings.Contains(got, "codeOwners") {
		t.Errorf("wrong delete:\n%s", got)
	}
	before := read(t, f)
	if err := Del(f, []string{"nope", "x"}); err != nil {
		t.Fatalf("missing path must be a no-op: %v", err)
	}
	if read(t, f) != before {
		t.Error("no-op delete rewrote the file")
	}
}

func TestRename(t *testing.T) {
	t.Parallel()
	f := write(t, src)
	if err := Rename(f, []string{"codeOwners"}, "owners"); err != nil {
		t.Fatal(err)
	}
	got := read(t, f)
	if !strings.Contains(got, "owners: [team1]") || strings.Contains(got, "codeOwners") {
		t.Errorf("not renamed:\n%s", got)
	}
	if err := Rename(f, []string{"missing"}, "x"); err == nil {
		t.Error("renaming a missing key must fail")
	}
}
