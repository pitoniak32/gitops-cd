package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(p, c string) {
		t.Helper()
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main", ".")
	write("systems/a b/x y.yml", "base-sys\n")
	write("other/o.txt", "other\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	write("systems/a b/x y.yml", "changed\n")
	write("systems/new.yml", "new\n")

	id, err := ResolveBase(root, "")
	if err != nil || len(id) != 40 {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := ResolveBase(root, "nope"); err == nil {
		t.Error("bad rev: want error")
	}
	dst := t.TempDir()
	if err := ExportBase(root, id, dst, "systems", "teams"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "systems/a b/x y.yml")); err != nil || string(b) != "base-sys\n" {
		t.Errorf("sys = %q, %v", b, err)
	}
	for _, p := range []string{"other", "systems/new.yml"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err == nil {
			t.Errorf("%s exported", p)
		}
	}
}

func TestResolveBaseNoVCS(t *testing.T) {
	if _, err := ResolveBase(t.TempDir(), ""); err == nil {
		t.Error("want not a jj or git repo error")
	}
}
