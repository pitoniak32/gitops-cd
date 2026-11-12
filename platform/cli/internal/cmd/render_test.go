package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --all wins over --capability and --platform: every capability, platform files, README.
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
	c.SetArgs([]string{"render", "--root", root, "--all", "--capability", "payments", "--platform", out})
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
