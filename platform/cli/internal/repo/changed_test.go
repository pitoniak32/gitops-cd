package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Parallel()
	d := t.TempDir()
	run(t, d, "init", "-q", "-b", "main")
	write(t, d, "capabilities/a/owners.yml", "x: 1\n")
	write(t, d, "capabilities/b/owners.yml", "x: 1\n")
	write(t, d, "teams/old.yml", "x: 1\n")
	write(t, d, ".gitignore", "ignored/\n")
	run(t, d, "add", "-A")
	run(t, d, "commit", "-qm", "base")
	run(t, d, "checkout", "-qb", "feature")
	// committed on the branch, uncommitted, renamed, untracked, ignored
	write(t, d, "capabilities/a/owners.yml", "x: 2\n")
	run(t, d, "commit", "-qam", "a")
	// main moves on after the branch point: not a change of this branch
	run(t, d, "checkout", "-q", "main")
	write(t, d, "capabilities/b/owners.yml", "x: 3\n")
	run(t, d, "commit", "-qam", "b")
	run(t, d, "checkout", "-q", "feature")
	run(t, d, "mv", "teams/old.yml", "teams/new.yml")
	write(t, d, "capabilities/c/owners.yml", "x: 1\n")
	write(t, d, "ignored/x.yml", "x: 1\n")

	got, mb, err := Changed(d, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"capabilities/a/owners.yml", "capabilities/c/owners.yml", "teams/new.yml", "teams/old.yml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if mb == "" {
		t.Error("no merge-base")
	}
	// default bases: no origin/main here, so main
	if got2, _, err := Changed(d, ""); err != nil || !reflect.DeepEqual(got2, want) {
		t.Errorf("default base: %v %v", got2, err)
	}
	if _, _, err := Changed(d, "nope"); err == nil {
		t.Error("want error for unknown base")
	}
}
