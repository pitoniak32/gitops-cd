package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

// ciFixture: a git repo of the fixture with origin/main at its first commit, then payments and search
// both broken; search's breakage is uncommitted (a PR change), payments' is on origin/main already.
func ciFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := yamledit.Set(filepath.Join(root, "systems/payments/owners.yml"), []string{"codeOwners"}, []any{"nobody"}); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "base")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	if err := yamledit.Set(filepath.Join(root, "systems/search/owners.yml"), []string{"codeOwners"}, []any{"nobody"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func runLintCI(t *testing.T, env map[string]string, args ...string) (stdout, stderr string) {
	t.Helper()
	old := getenv
	getenv = func(k string) string { return env[k] }
	t.Cleanup(func() { getenv = old })
	c := NewRoot()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs(append([]string{"lint"}, args...))
	_ = c.Execute()
	return out.String(), errOut.String()
}

func TestLintGitHubActions(t *testing.T) {
	root := ciFixture(t)
	event := filepath.Join(t.TempDir(), "event.json")
	write := func(labels string) {
		if err := os.WriteFile(event, []byte(`{"pull_request":{"labels":[`+labels+`]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pr := map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request", "GITHUB_BASE_REF": "main", "GITHUB_EVENT_PATH": event}
	const pay, search = "::error file=systems/payments/owners.yml,", "::error file=systems/search/owners.yml,"

	// PR: only what changed (search), as annotations
	write(`{"name":"bug"}`)
	out, errOut := runLintCI(t, pr, "--root", root)
	if !strings.Contains(out, search) || strings.Contains(out, pay) {
		t.Errorf("PR: want search only, as annotations:\n%s\n%s", out, errOut)
	}
	if !strings.Contains(errOut, "changes since") {
		t.Errorf("PR: no scope note: %q", errOut)
	}

	// PR labeled lint-all: everything
	write(`{"name":"lint-all"}`)
	if out, errOut := runLintCI(t, pr, "--root", root); !strings.Contains(out, search) || !strings.Contains(out, pay) {
		t.Errorf("lint-all: want both:\n%s\n%s", out, errOut)
	}

	// push: everything
	push := map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "push"}
	if out, _ := runLintCI(t, push, "--root", root); !strings.Contains(out, search) || !strings.Contains(out, pay) {
		t.Errorf("push: want both:\n%s", out)
	}

	// explicit selection wins over the PR default; --format text wins over auto
	write(``)
	out, _ = runLintCI(t, pr, "--root", root, "--format", "text", "payments")
	if !strings.HasPrefix(out, "systems/payments/owners.yml:") || strings.Contains(out, "search") {
		t.Errorf("explicit: want payments only, as text:\n%s", out)
	}
}
