package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
)

func paths(d []FileDiff) []string {
	var p []string
	for _, f := range d {
		p = append(p, f.Path)
	}
	return p
}

func TestDiffReplicas(t *testing.T) {
	base, root := fixture(t), fixture(t)
	p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "replicas: 2", "replicas: 5", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Diff(system.Default(), base, root, []string{"payments"})
	if err != nil || len(d) == 0 {
		t.Fatalf("d=%v err=%v", paths(d), err)
	}
	for _, f := range d {
		if !strings.Contains(f.Path, "my-app") || f.Old == "" || f.New == "" || f.Old == f.New {
			t.Errorf("unexpected %s", f.Path)
		}
	}
	u := Unified(d)
	if !strings.Contains(u, "--- a/rendered/"+d[0].Path+"\n+++ b/rendered/"+d[0].Path+"\n") {
		t.Errorf("headers: %s", u)
	}
}

func TestDiffNoChange(t *testing.T) {
	d, err := Diff(system.Default(), fixture(t), fixture(t), []string{"payments"})
	if err != nil || len(d) != 0 {
		t.Fatalf("d=%v err=%v", paths(d), err)
	}
}

func TestDiffNewSystem(t *testing.T) {
	base, root := fixture(t), fixture(t)
	if err := os.RemoveAll(filepath.Join(base, "systems/search")); err != nil {
		t.Fatal(err)
	}
	d, err := Diff(system.Default(), base, root, []string{"search"})
	if err != nil || len(d) == 0 {
		t.Fatalf("d=%v err=%v", paths(d), err)
	}
	for _, f := range d {
		if f.Old != "" || f.New == "" {
			t.Errorf("%s not added", f.Path)
		}
	}
}

func TestDiffDeletedSystem(t *testing.T) {
	base, root := fixture(t), fixture(t)
	if err := os.RemoveAll(filepath.Join(root, "systems/search")); err != nil {
		t.Fatal(err)
	}
	d, err := Diff(system.Default(), base, root, []string{"search"})
	if err != nil || len(d) == 0 {
		t.Fatalf("d=%v err=%v", paths(d), err)
	}
	for _, f := range d {
		if f.New != "" || f.Old == "" {
			t.Errorf("%s not removed", f.Path)
		}
	}
}

func TestDiffBaseError(t *testing.T) {
	base, root := fixture(t), fixture(t)
	p := filepath.Join(base, "systems/payments/owners.yml")
	if err := os.WriteFile(p, []byte("codeOwners: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Diff(system.Default(), base, root, []string{"payments"}); err == nil || !strings.HasPrefix(err.Error(), "base: ") {
		t.Fatalf("err=%v", err)
	}
}

func TestChangedSystems(t *testing.T) {
	base, root := fixture(t), fixture(t)
	if c, err := ChangedSystems(base, root); err != nil || len(c) != 0 {
		t.Fatalf("c=%v err=%v", c, err)
	}
	os.RemoveAll(filepath.Join(root, "systems/search"))
	p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, '\n'), 0o644)
	if c, _ := ChangedSystems(base, root); strings.Join(c, ",") != "payments,search" {
		t.Errorf("c=%v", c)
	}
	// teams change → all systems from both sides
	base, root = fixture(t), fixture(t)
	os.RemoveAll(filepath.Join(root, "systems/search"))
	ents, _ := os.ReadDir(filepath.Join(root, "teams"))
	tp := filepath.Join(root, "teams", ents[0].Name())
	tb, _ := os.ReadFile(tp)
	os.WriteFile(tp, append(tb, '\n'), 0o644)
	if c, _ := ChangedSystems(base, root); strings.Join(c, ",") != "payments,search" {
		t.Errorf("teams: c=%v", c)
	}
}

func TestStat(t *testing.T) {
	d := []FileDiff{
		{Path: "a.yaml", Old: "x\ny\n", New: "x\nz\nw\n"},
		{Path: "b.yaml", New: "n\n"},
		{Path: "c.yaml", Old: "o\n"},
	}
	want := "a.yaml | +2 -1\nb.yaml | +1 -0\nc.yaml | +0 -1\n3 files changed, +3 -2\n"
	if got := Stat(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnifiedAddedRemoved(t *testing.T) {
	got := Unified([]FileDiff{{Path: "a.yaml", New: "x\ny"}, {Path: "b.yaml", Old: "p\nq\n"}})
	want := "--- /dev/null\n+++ b/rendered/a.yaml\n@@ -0,0 +1,2 @@\n+x\n+y\n" +
		"--- a/rendered/b.yaml\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-p\n-q\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}
