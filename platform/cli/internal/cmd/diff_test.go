package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffStat(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("jj", append([]string{"--config", "user.name=t", "--config", "user.email=t@example.com"}, args...)...)
		c.Dir = root
		c.Env = append(os.Environ(), "JJ_CONFIG=")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", ".")
	run("commit", "-m", "base")
	p := filepath.Join(root, "capabilities/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "replicas: 2", "replicas: 5", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewRoot()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs([]string{"diff", "--stat", "--root", root})
	if err := c.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "my-app") || !strings.Contains(out.String(), "files changed") {
		t.Errorf("stat: %q", out.String())
	}
}

func TestDiffUnknownCapability(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("jj", "--config", "user.name=t", "--config", "user.email=t@example.com", "git", "init", ".")
	c.Dir = root
	c.Env = append(os.Environ(), "JJ_CONFIG=")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cmd := NewRoot()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"diff", "--capability", "nope", "--base", "root()", "--root", root})
	err := cmd.Execute()
	if err == nil || !strings.HasPrefix(err.Error(), "unknown capability nope; allowed: ") {
		t.Errorf("got %v", err)
	}
}
