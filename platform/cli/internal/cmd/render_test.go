package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --all wins over named systems and --platform: every system, platform files, README.
func TestRenderAllOverrides(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	c := NewRoot()
	var stdout, errOut bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&errOut)
	c.SetArgs([]string{"render", "--root", root, "--all", "payments", "--platform", "--out", out})
	if err := c.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	for _, p := range []string{"apps/search", "apps/payments", "README.md", "kargo/_promotion-tasks.yaml"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	if !strings.Contains(errOut.String(), "--all: everything") {
		t.Errorf("no override note: %q", errOut.String())
	}
}

// Named systems render alone (no platform files, no other system) into --out.
func TestRenderPositionalSystems(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	c := NewRoot()
	var stdout, errOut bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&errOut)
	c.SetArgs([]string{"render", "--root", root, "payments", "--out", out})
	if err := c.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(out, "apps/payments")); err != nil {
		t.Errorf("payments not rendered: %v", err)
	}
	for _, p := range []string{"apps/search", "README.md", "kargo/_promotion-tasks.yaml"} {
		if _, err := os.Stat(filepath.Join(out, p)); err == nil {
			t.Errorf("%s rendered, want payments only", p)
		}
	}
}

// Validation errors print like gcd lint (file:line:col: message).
func TestRenderPrintsDiags(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "replicas: 2", "replicas: x", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewRoot()
	var stdout, errOut bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&errOut)
	c.SetArgs([]string{"render", "--root", root, "payments", "--out", t.TempDir()})
	if err := c.Execute(); err == nil || !strings.Contains(errOut.String(), "systems/payments/apps/my-app/app.yml:") {
		t.Errorf("err=%v stderr=%q", err, errOut.String())
	}
}
