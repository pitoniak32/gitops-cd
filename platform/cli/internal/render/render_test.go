package render

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const renderDir = "../../../render"

func abs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(filepath.Join(renderDir, p))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func repoRoot(t *testing.T) string { return abs(t, "../..") }

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		m[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("line %d: want ", i+1) + x + " | got " + y
		}
	}
	return ""
}

func renderTestRoot(t *testing.T, root, out string, caps ...string) {
	t.Helper()
	for _, c := range caps {
		if _, err := Capability(root, c, out); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
	}
}

func TestGolden(t *testing.T) {
	t.Parallel()
	root := abs(t, "testdata/root")
	out := t.TempDir()
	renderTestRoot(t, root, out, "payments", "search")
	if err := Regions(repoRoot(t), out); err != nil {
		t.Fatal(err)
	}
	golden := abs(t, "testdata/golden")
	if os.Getenv("UPDATE") == "1" {
		if err := os.RemoveAll(golden); err != nil {
			t.Fatal(err)
		}
		copyTree(t, out, golden)
		t.Skip("updated")
	}
	want, got := readTree(t, golden), readTree(t, out)
	for p, w := range want {
		g, ok := got[p]
		switch {
		case !ok:
			t.Errorf("missing: %s", p)
		case g != w:
			t.Errorf("differs: %s: %s", p, firstDiff(w, g))
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("extra: %s", p)
		}
	}
}

func TestCodeOwnersGolden(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	renderTestRoot(t, root, out, "payments", "search")
	if err := CodeOwners(root, out); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, ".github", "CODEOWNERS"))
	want, _ := os.ReadFile(abs(t, "testdata/golden-codeowners"))
	if string(got) != string(want) {
		t.Errorf("CODEOWNERS differs: %s", firstDiff(string(want), string(got)))
	}
	// deleted capability drops out on the next render
	if err := os.RemoveAll(filepath.Join(root, "capabilities", "search")); err != nil {
		t.Fatal(err)
	}
	out2 := t.TempDir()
	renderTestRoot(t, root, out2, "payments")
	if err := CodeOwners(root, out2); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(root, ".github", "CODEOWNERS"))
	if strings.Contains(string(got), "search") {
		t.Error("deleted capability still in CODEOWNERS")
	}
}

func TestCodeOwnersNeedsOneMarker(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "CODEOWNERS.tmpl"), []byte("* @a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CodeOwners(root, t.TempDir()); err == nil || !strings.Contains(err.Error(), "#GENERATED") {
		t.Fatalf("want #GENERATED error, got %v", err)
	}
}

func TestUniqueApps(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	renderTestRoot(t, abs(t, "testdata/root"), out, "payments")
	if err := UniqueApps(out); err != nil {
		t.Fatalf("clean render rejected: %v", err)
	}
	src := filepath.Join(out, "argocd/deploys/us-east4/payments/my-app-us-east4-dev-k8s.json")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(out, "argocd/deploys/us-east4/payments-my")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-us-east4-dev-k8s.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	err = UniqueApps(out)
	if err == nil || !strings.Contains(err.Error(), "duplicate workload Application name payments-my-app-us-east4-dev-k8s") {
		t.Fatalf("duplicate not rejected: %v", err)
	}
}

func TestCapabilityEmptyGlobs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "capabilities", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Capability(root, "empty", t.TempDir()); err == nil {
		t.Fatal("expected a validation error for a capability with no inputs")
	}
	if _, err := Capability(root, "missing", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no capability dir") {
		t.Fatalf("want no capability dir, got %v", err)
	}
}

func TestRepoTag(t *testing.T) {
	// not parallel: t.Setenv
	t.Setenv("GITOPS_REPO", "https://example.com/x")
	out := t.TempDir()
	renderTestRoot(t, abs(t, "testdata/root"), out, "payments")
	b, err := os.ReadFile(filepath.Join(out, "kargo", "payments.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "repoURL: https://example.com/x") {
		t.Error("Warehouse git repoURL not taken from GITOPS_REPO")
	}
}

func TestRenderPrunes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	out := t.TempDir()
	renderTestRoot(t, root, out, "payments")
	if _, err := os.Stat(filepath.Join(out, "apps/payments/worker")); err != nil {
		t.Fatal("worker not rendered: ", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "capabilities/payments/apps/worker")); err != nil {
		t.Fatal(err)
	}
	ow := filepath.Join(root, "capabilities/payments/owners.yml")
	b, err := os.ReadFile(ow)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(b), "apps:\n  worker:")
	if err := os.WriteFile(ow, b[:i], 0o644); err != nil {
		t.Fatal(err)
	}
	renderTestRoot(t, root, out, "payments")
	if _, err := os.Stat(filepath.Join(out, "apps/payments/worker")); !os.IsNotExist(err) {
		t.Fatalf("apps/payments/worker not pruned: %v", err)
	}
}

func TestAll(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	out := t.TempDir()
	if err := All(root, out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Error(err)
	}
}

func TestAllReportsEveryBrokenCapability(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	for _, c := range []string{"payments", "search"} {
		if err := os.Remove(filepath.Join(root, "capabilities", c, "owners.yml")); err != nil {
			t.Fatal(err)
		}
	}
	err := All(root, t.TempDir(), io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, w := range []string{"payments:", "search:"} {
		if !strings.Contains(msg, w) {
			t.Errorf("missing %q in:\n%s", w, msg)
		}
	}
	if strings.Count(msg, "capability needs owners.yml") < 2 {
		t.Errorf("want both messages:\n%s", msg)
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()
	if Wrap(nil) != nil {
		t.Fatal("nil")
	}
	plain := io.EOF
	if Wrap(plain) != plain {
		t.Fatal("non-CUE error must pass through")
	}
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	if err := os.Remove(filepath.Join(root, "capabilities/payments/owners.yml")); err != nil {
		t.Fatal(err)
	}
	_, err := Capability(root, "payments", t.TempDir())
	if w := Wrap(err); w == nil || !strings.Contains(w.Error(), "capability needs owners.yml") {
		t.Fatalf("got %v", w)
	}
}

func TestFragmentsAndSelectedReportEveryBrokenCapability(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	for _, c := range []string{"payments", "search"} {
		if err := os.Remove(filepath.Join(root, "capabilities", c, "owners.yml")); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
		msg := err.Error()
		for _, w := range []string{"payments:", "search:"} {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: missing %q in:\n%s", name, w, msg)
			}
		}
		if strings.Count(msg, "capability needs owners.yml") < 2 {
			t.Errorf("%s: want both messages:\n%s", name, msg)
		}
	}
	check("Fragments", Fragments(root, t.TempDir(), io.Discard))
	check("Selected", Selected(root, []string{"payments", "search"}, t.TempDir(), io.Discard))
}
