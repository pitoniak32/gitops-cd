package regions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
)

var files = []string{regionsFile, realmFile, kargoValues}

func TestWriteMatchesCommitted(t *testing.T) {
	real, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(real, f))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(tmp, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
	}
	cfg, err := Load(real)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ { // second pass = idempotent
		if err := Write(tmp, cfg); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			want, _ := os.ReadFile(filepath.Join(real, f))
			got, _ := os.ReadFile(filepath.Join(tmp, f))
			if string(want) != string(got) {
				t.Errorf("pass %d: %s differs:\n%s", pass, f, got)
			}
		}
	}
}

func TestList(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "platform/cluster"), 0o755)
	os.WriteFile(filepath.Join(tmp, regionsFile), []byte("# c\nonprem\n\nus\n"), 0o644)
	got, err := List(tmp)
	if err != nil || len(got) != 2 || got[0] != "onprem" || got[1] != "us" {
		t.Fatalf("%v %v", got, err)
	}
}
