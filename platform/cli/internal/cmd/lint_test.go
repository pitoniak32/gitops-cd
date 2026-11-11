package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
