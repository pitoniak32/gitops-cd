package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

// An error lint can't turn into a diagnostic (here: a missing capability) must not hide the
// other capabilities' diagnostics.
func TestLintPrintsDiagsBeforeError(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	owners := filepath.Join(root, "capabilities/payments/owners.yml")
	if err := yamledit.Set(owners, []string{"promoters", "dev"}, []any{"doug@evil.com"}); err != nil {
		t.Fatal(err)
	}
	c := NewRoot()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs([]string{"lint", "--root", root, "payments", "nope"})
	if err := c.Execute(); err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(out.String(), "capabilities/payments/owners.yml:") {
		t.Errorf("payments diagnostic missing from stdout: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "nope") {
		t.Errorf("error for capability nope missing from stderr: %q", errOut.String())
	}
}

func TestLintChangedFlagRules(t *testing.T) {
	for _, args := range [][]string{
		{"lint", "--changed", "payments"},
		{"lint", "--base", "main"},
	} {
		c := NewRoot()
		var out, errOut bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&errOut)
		c.SetArgs(append(args, "--root", t.TempDir()))
		if err := c.Execute(); err == nil {
			t.Errorf("%v: want error", args)
		}
	}
}

// --all wins over --changed and named capabilities: no git needed, every capability linted.
func TestLintAllOverrides(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	// broken search: must be reported although only payments is named
	if err := yamledit.Set(filepath.Join(root, "capabilities/search/owners.yml"), []string{"codeOwners"}, []any{"nobody"}); err != nil {
		t.Fatal(err)
	}
	c := NewRoot()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs([]string{"lint", "--root", root, "--all", "--changed", "--base", "nope", "payments"})
	if err := c.Execute(); err == nil {
		t.Fatal("want lint failure")
	}
	if !strings.Contains(out.String(), "capabilities/search/owners.yml:") {
		t.Errorf("search not linted: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "--all: everything") {
		t.Errorf("no override note: %q", errOut.String())
	}
}

// --format json: the diagnostics as a JSON array (same entries as text mode), [] when clean.
func TestLintJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	if err := yamledit.Set(filepath.Join(root, "capabilities/payments/owners.yml"), []string{"promoters", "dev"}, []any{"doug@evil.com"}); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		c := NewRoot()
		var out, errOut bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&errOut)
		c.SetArgs(append([]string{"lint", "--root", root, "--no-progress"}, args...))
		err := c.Execute()
		return out.String(), err
	}
	text, err := run("payments")
	if err == nil {
		t.Fatal("text: want lint failure")
	}
	js, err := run("payments", "--format", "json")
	if err == nil {
		t.Fatal("json: want lint failure")
	}
	var diags []capability.Diag
	if err := json.Unmarshal([]byte(js), &diags); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, js)
	}
	var lines []string
	for _, d := range diags {
		lines = append(lines, d.String())
	}
	if want := strings.TrimSpace(text); strings.Join(lines, "\n") != want {
		t.Errorf("json diags differ from text:\njson: %q\ntext: %q", lines, want)
	}
	if len(diags) == 0 || diags[0].File == "" || diags[0].Line == 0 {
		t.Errorf("diag fields not populated: %+v", diags)
	}

	clean := t.TempDir()
	if err := os.CopyFS(clean, os.DirFS("../../../render/testdata/root")); err != nil {
		t.Fatal(err)
	}
	c := NewRoot()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"lint", "--root", clean, "--no-progress", "--format", "json"})
	if err := c.Execute(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("clean json = %q, want []", got)
	}
}
