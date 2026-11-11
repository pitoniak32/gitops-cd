package yamlpos

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.yml")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFind(t *testing.T) {
	t.Parallel()
	f := write(t, "# yaml-language-server: $schema=../x.json\n\nimage: foo\nstages:\n  \"dev\":\n    targets:\n      a-b: {}\nenvFrom:\n  - one\n  - two\n")
	for _, c := range []struct {
		path      []string
		line, col int
		ok        bool
	}{
		{nil, 3, 1, true},
		{[]string{"image"}, 3, 1, true},
		{[]string{"stages", "dev"}, 5, 3, true},
		{[]string{"stages", "dev", "targets", "a-b"}, 7, 7, true},
		{[]string{"envFrom", "1"}, 10, 5, true},
		{[]string{"envFrom", "2"}, 0, 0, false},
		{[]string{"missing"}, 0, 0, false},
		{[]string{"image", "x"}, 0, 0, false},
	} {
		l, c2, ok := Find(f, c.path)
		if l != c.line || c2 != c.col || ok != c.ok {
			t.Errorf("Find(%v) = %d:%d %v, want %d:%d %v", c.path, l, c2, ok, c.line, c.col, c.ok)
		}
	}
	if _, _, ok := Find(filepath.Join(t.TempDir(), "nope.yml"), nil); ok {
		t.Error("missing file must not be found")
	}
}
