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

func TestChangedFilesGit(t *testing.T) {
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

	want := []string{"capabilities/a/owners.yml", "capabilities/c/owners.yml", "teams/new.yml", "teams/old.yml"}
	// explicit base and the default (no origin/main here, so main) fork from main
	for _, base := range []string{"main", ""} {
		id, err := ResolveForkBase(d, base)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ChangedFiles(d, id)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("base %q: got %v (%v) want %v", base, got, err, want)
		}
	}
	if _, err := ResolveForkBase(d, "nope"); err == nil {
		t.Error("want error for unknown base")
	}
}

func TestChangedFilesJJ(t *testing.T) {
	root := jjRepo(t) // base commit (trunk), then an uncommitted edit and a new file
	id, err := ResolveForkBase(root, "")
	if err != nil {
		t.Fatal(err)
	}
	// no trunk() bookmark: the fork point is the root commit, so the base commit's own files count
	if got, err := ChangedFiles(root, id); err != nil || len(got) != 4 {
		t.Errorf("from root: %v %v", got, err)
	}
	base, err := ResolveOne(root, "@-")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ChangedFiles(root, base)
	want := []string{"capabilities/a b/x y.yml", "capabilities/new.yml"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v (%v) want %v", got, err, want)
	}
	// explicit base: fork_point(base | @)
	if id, err := ResolveForkBase(root, "@-"); err != nil || id != base {
		t.Errorf("fork base = %q (%v), want %q", id, err, base)
	}
}
