package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
)

func runDocs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := NewRoot()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs(append([]string{"docs"}, args...))
	err := c.Execute()
	return out.String(), err
}

func TestDocsRewritesEmbed(t *testing.T) {
	root := badGuideRoot(t)
	if err := os.RemoveAll(filepath.Join(root, "docs", "guide")); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(root, "docs", "reference")
	if err := os.MkdirAll(ref, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(ref, "a.md")
	if err := os.WriteFile(p, []byte("<!-- embed path=capabilities/payments/stages/dev.yml -->\nold\n<!-- /embed -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewRoot()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs([]string{"docs", "--root", root, "--format", "text"})
	if err := c.Execute(); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "Source: [") {
		t.Errorf("not rewritten: %s", b)
	}
	if !strings.Contains(errOut.String(), "docs: updated 1 page(s)") {
		t.Errorf("stderr: %q", errOut.String())
	}
}

// badGuideRoot: the fixture as repo root (and at FixtureRoot) plus a guide with one bad example.
func badGuideRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fx := "../../../render/testdata/root"
	if err := os.CopyFS(filepath.Join(root, filepath.FromSlash(capability.FixtureRoot)), os.DirFS(fx)); err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(root, "docs", "guide")
	if err := os.MkdirAll(g, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "```yaml path=capabilities/payments/stages/dev.yml\nbogus: 1\n```\n"
	if err := os.WriteFile(filepath.Join(g, "x.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDocsGuideErrors(t *testing.T) {
	root := badGuideRoot(t)
	out, err := runDocs(t, "--root", root, "--format", "text")
	if err == nil || !strings.Contains(out, "docs/guide/x.md:2:1: bogus: field not allowed") {
		t.Errorf("text: err %v, out %q", err, out)
	}
	out, err = runDocs(t, "--root", root, "--format", "json")
	var d []capability.Diag
	if err == nil || json.Unmarshal([]byte(out), &d) != nil || len(d) != 1 {
		t.Errorf("json: err %v, out %q", err, out)
	}
}

func TestDocsSandboxRoot(t *testing.T) {
	if _, err := runDocs(t, "--root", "../../../render/testdata/root", "--format", "text"); err != nil {
		t.Fatal(err)
	}
}
