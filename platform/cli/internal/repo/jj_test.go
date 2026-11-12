package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func jjRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("jj", append([]string{"--config", "user.name=t", "--config", "user.email=t@example.com"}, args...)...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "JJ_CONFIG=")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
	}
	write := func(p, c string) {
		t.Helper()
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", ".")
	write("capabilities/a b/x y.yml", "base-cap\n")
	write("teams/t.yml", "base-team\n")
	write("other/o.txt", "other\n")
	run("commit", "-m", "base")
	write("capabilities/a b/x y.yml", "changed\n")
	write("capabilities/new.yml", "new\n")
	return root
}

func TestResolveOne(t *testing.T) {
	root := jjRepo(t)
	id, err := ResolveOne(root, "@-")
	if err != nil || len(id) < 20 {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := ResolveOne(root, "all()"); err == nil || !strings.Contains(err.Error(), "want exactly 1") {
		t.Errorf("all(): %v", err)
	}
	if _, err := ResolveOne(root, "none()"); err == nil || !strings.Contains(err.Error(), "resolves to 0") {
		t.Errorf("none(): %v", err)
	}
	if _, err := ResolveOne(root, "(("); err == nil {
		t.Error("bad revset: want error")
	}
}

func TestExport(t *testing.T) {
	root := jjRepo(t)
	id, err := ResolveOne(root, "@-")
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Export(root, id, dst, "capabilities", "teams", "absent"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "capabilities/a b/x y.yml"))
	if err != nil || string(got) != "base-cap\n" {
		t.Errorf("cap = %q, %v", got, err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "teams/t.yml")); err != nil || string(b) != "base-team\n" {
		t.Errorf("team = %q, %v", b, err)
	}
	for _, p := range []string{"other", "capabilities/new.yml"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err == nil {
			t.Errorf("%s exported", p)
		}
	}
}
