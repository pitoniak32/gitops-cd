package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs("../../../render/testdata/root")
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	err = filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestLintClean(t *testing.T) {
	r := fixture(t)
	d, err := All(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 0 {
		t.Fatalf("fixture must lint clean, got %v", d)
	}
}

func TestLintYamlExtension(t *testing.T) {
	r := fixture(t)
	st := filepath.Join(r, "capabilities/payments/stages")
	if err := os.Rename(filepath.Join(st, "dev.yml"), filepath.Join(st, "dev.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st, "dev.yaml"), []byte("# comment\n\ncolor: puce\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Capability(r, "payments")
	if err != nil {
		t.Fatal(err)
	}
	var hit *Diag
	for i := range d {
		if strings.HasSuffix(d[i].File, "stages/dev.yaml") {
			hit = &d[i]
		}
	}
	if hit == nil {
		t.Fatalf("no diagnostic on stages/dev.yaml: %v", d)
	}
	if hit.Line != 3 || hit.Col != 1 || hit.Label != "color" {
		t.Errorf("want dev.yaml:3:1 label color, got %s", hit)
	}
}

func TestDiagString(t *testing.T) {
	for _, c := range []struct {
		d    Diag
		want string
	}{
		{Diag{"a.yml", 2, 3, "x.y", "bad"}, "a.yml:2:3: x.y: bad"},
		{Diag{"a.yml", 1, 1, "", "bad"}, "a.yml:1:1: bad"},
	} {
		if got := c.d.String(); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestAllSortsAndDedupes(t *testing.T) {
	r := fixture(t)
	// a teams/ error is reported once per capability; All dedupes it
	if err := os.WriteFile(filepath.Join(r, "teams/team1.yml"), []byte("githubTeam: Bad_Name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := All(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i, x := range d {
		if seen[x.String()] {
			t.Errorf("duplicate %s", x)
		}
		seen[x.String()] = true
		if i > 0 && (d[i-1].File > x.File || d[i-1].File == x.File && d[i-1].Line > x.Line) {
			t.Errorf("unsorted: %s before %s", d[i-1], x)
		}
	}
	if len(d) == 0 {
		t.Fatal("want a teams/team1.yml diagnostic")
	}
}
