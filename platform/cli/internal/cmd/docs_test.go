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

var referencePages = []string{"owners.md", "stage.md", "app.md", "target.md", "env-vars.md"}

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

func TestDocsReferenceGolden(t *testing.T) {
	repo, _ := filepath.Abs("../../../..")
	tmp := t.TempDir()
	if _, err := runDocs(t, "--root", repo, "--out", tmp, "--format", "text"); err != nil {
		t.Fatal(err)
	}
	for _, f := range referencePages {
		got, err := os.ReadFile(filepath.Join(tmp, f))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(repo, "docs", "reference", f)
		if os.Getenv("UPDATE") == "1" {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("docs/reference/%s is stale: run gcd docs (or UPDATE=1)", f)
		}
	}
}

func TestDocsIdempotent(t *testing.T) {
	repo, _ := filepath.Abs("../../../..")
	tmp := t.TempDir()
	other := filepath.Join(tmp, "keep.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		var b strings.Builder
		for _, f := range referencePages {
			c, err := os.ReadFile(filepath.Join(tmp, f))
			if err != nil {
				t.Fatal(err)
			}
			b.Write(c)
		}
		return b.String()
	}
	for range 2 {
		if _, err := runDocs(t, "--root", repo, "--out", tmp, "--format", "text"); err != nil {
			t.Fatal(err)
		}
	}
	first := read()
	if _, err := runDocs(t, "--root", repo, "--out", tmp, "--format", "text"); err != nil {
		t.Fatal(err)
	}
	if read() != first {
		t.Error("second run changed the pages")
	}
	if b, err := os.ReadFile(other); err != nil || string(b) != "x" {
		t.Error("unrelated file in --out touched")
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
	out, err := runDocs(t, "--root", root, "--out", t.TempDir(), "--format", "text")
	if err == nil || !strings.Contains(out, "docs/guide/x.md:2:1: bogus: field not allowed") {
		t.Errorf("text: err %v, out %q", err, out)
	}
	out, err = runDocs(t, "--root", root, "--out", t.TempDir(), "--format", "json")
	var d []capability.Diag
	if err == nil || json.Unmarshal([]byte(out), &d) != nil || len(d) != 1 {
		t.Errorf("json: err %v, out %q", err, out)
	}
}

func TestDocsSandboxRoot(t *testing.T) {
	tmp := t.TempDir()
	if _, err := runDocs(t, "--root", "../../../render/testdata/root", "--out", tmp, "--format", "text"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "app.md")); err != nil {
		t.Error(err)
	}
}
