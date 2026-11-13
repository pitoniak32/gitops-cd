package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRoot(t *testing.T) {
	top := t.TempDir()
	t.Setenv("GCD_ROOT", "")
	if err := os.MkdirAll(filepath.Join(top, "platform/cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, "platform/cli/go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(top, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	got, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(top)
	g, _ := filepath.EvalSymlinks(got)
	if g != want {
		t.Fatalf("got %s want %s", g, want)
	}
}
