package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const fixtureRoot = "../../../render/testdata/root"

func runShow(t *testing.T, root string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c := NewRoot()
	var o, e bytes.Buffer
	c.SetOut(&o)
	c.SetErr(&e)
	c.SetArgs(append([]string{"show", "--root", root}, args...))
	err = c.Execute()
	return o.String(), e.String(), err
}

func TestShowGolden(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	for name, args := range map[string][]string{
		"show-payments.yaml":          {"payments"},
		"show-my-app-prod.yaml":       {"payments/my-app", "--stage", "prod"},
		"show-my-app-sources.yaml":    {"payments/my-app", "--sources"},
		"show-target.yaml":            {"payments", "--target", "dc1-dev-k8s"},
		"show-my-app.json":            {"payments/my-app", "--json"},
		"show-rendered.yaml":          {"payments/my-app", "--stage", "dev", "--rendered"},
		"show-rendered-platform.yaml": {"payments/my-app", "--stage", "dev", "--target", "dc1-dev-k8s", "--rendered", "--include-platform"},
	} {
		got, _, err := runShow(t, root, args...)
		if err != nil {
			t.Fatal(err)
		}
		p := schemaGolden + name
		if os.Getenv("UPDATE") == "1" {
			if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs from gcd show output (UPDATE=1 to accept)", p)
		}
	}
}

// The default output parses as YAML with flat values; --sources only adds comments.
func TestShowYAMLParses(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	var plain, withSrc map[string]any
	for args, dst := range map[string]*map[string]any{"": &plain, "-s": &withSrc} {
		a := []string{"payments/my-app"}
		if args != "" {
			a = append(a, args)
		}
		out, _, err := runShow(t, root, a...)
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(out), dst); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	if !reflect.DeepEqual(plain, withSrc) {
		t.Error("--sources changed the data")
	}
	app := plain["my-app"].(map[string]any)
	dev := app["stages"].(map[string]any)["dev"].(map[string]any)["dc1-dev-k8s"].(map[string]any)
	if dev["replicas"] != 3 || dev["image"] == nil {
		t.Errorf("dev/dc1-dev-k8s: %v", dev)
	}
	if env := dev["env"].(map[string]any); env["STAGE_ENV"] != "false" {
		t.Errorf("STAGE_ENV must stay a string: %#v", env["STAGE_ENV"])
	}
	// the source of a target-layer value is the target file, relative to the capability
	out, _, _ := runShow(t, root, "payments/my-app", "--stage", "dev", "--target", "dc1-dev-k8s", "-s")
	if !strings.Contains(out, "# targets/dc1-dev-k8s/target.yml:3") || !strings.Contains(out, "# global default") {
		t.Errorf("sources:\n%s", out)
	}
}

func TestShowLintErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixtureRoot)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "capabilities/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "replicas: 2", "replicas: x", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runShow(t, root, "payments")
	if err == nil || out != "" || !strings.Contains(errOut, "app.yml:") {
		t.Errorf("err=%v stdout=%q stderr=%q", err, out, errOut)
	}
}

func TestShowUnknown(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"payments/nope"}, "unknown app nope; allowed: flagged, infra, my-app, payments, web, worker"},
		{[]string{"payments", "--stage", "nope"}, "unknown stage nope; allowed: dev, perf, prod, staging"},
		{[]string{"payments", "--target", "nope"}, "unknown target nope; allowed: dc1-dev-k8s, dc1-npr-k8s, dc1-prd-k8s, us-east4-dev-k8s, us-east4-dev-tf, us-east4-npr-cr, us-east4-npr-k8s, us-east4-npr-k8s-perf, us-east4-prd-k8s, us-east4-prd-tf"},
	} {
		out, _, err := runShow(t, root, c.args...)
		if err == nil || out != "" || err.Error() != c.want {
			t.Errorf("%v: err=%v out=%q", c.args, err, out)
		}
	}
}

func TestShowFlagConflicts(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	for _, args := range [][]string{
		{"payments", "--rendered", "--json"},
		{"payments", "--diff", "--sources"},
		{"payments", "--rendered", "--diff"},
		{"payments", "--include-platform"},
		{"payments", "--stat"},
		{"payments", "--base", "main"},
		{"--diff", "--stage", "dev"},
		{},
	} {
		if out, _, err := runShow(t, root, args...); err == nil || out != "" {
			t.Errorf("%v: want error, got %v %q", args, err, out)
		}
	}
}

// vcsRepo copies the fixture into a fresh repo of kind ("git" or "jj"), commits it, and returns its root.
func vcsRepo(t *testing.T, kind string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixtureRoot)); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = root
		c.Env = append(os.Environ(), "JJ_CONFIG=")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	id := []string{"user.name=t", "user.email=t@example.com"}
	switch kind {
	case "git":
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not installed")
		}
		run("git", "init", "-q", "-b", "main", ".")
		run("git", "add", ".")
		run("git", "-c", id[0], "-c", id[1], "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	case "jj":
		if _, err := exec.LookPath("jj"); err != nil {
			t.Skip("jj not installed")
		}
		run("jj", "--config", id[0], "--config", id[1], "git", "init", ".")
		run("jj", "--config", id[0], "--config", id[1], "commit", "-m", "base")
		// no remote here: name trunk() so the default base is the commit the branch started from
		run("jj", "bookmark", "create", "main", "-r", "@-")
		run("jj", "config", "set", "--repo", `revset-aliases."trunk()"`, "main")
	}
	p := filepath.Join(root, "capabilities/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "    replicas: 1\n", "    replicas: 5\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestShowDiff(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		t.Run(kind, func(t *testing.T) {
			root := vcsRepo(t, kind)
			// the edit is the dev stage's replicas: us-east4-dev-k8s inherits it, dc1-dev-k8s overrides it
			out, errOut, err := runShow(t, root, "payments/my-app", "--stage", "dev", "--diff")
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			if !strings.Contains(out, "+++ b/rendered/apps/payments/my-app/dev/us-east4-dev-k8s/deployment.yaml") || !strings.Contains(out, "+  replicas: 5") {
				t.Errorf("want a deployment diff hunk:\n%s", out)
			}
			// unchanged files print nothing
			if strings.Contains(out, "dc1-dev-k8s") || strings.Contains(out, "# rendered/") {
				t.Errorf("unchanged files present:\n%s", out)
			}
			if strings.Contains(out, "search") || strings.Contains(out, "my-app/prod") {
				t.Errorf("unselected deploys present:\n%s", out)
			}
			t.Log("\n" + out)
		})
	}
}

func TestShowRequiresCapability(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	if _, _, err := runShow(t, root); err == nil || !strings.Contains(err.Error(), "a capability is required") {
		t.Errorf("got %v", err)
	}
}

func TestShowUnknownCapability(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	if _, _, err := runShow(t, root, "nope", "--diff"); err == nil || !strings.HasPrefix(err.Error(), "unknown capability nope; allowed: ") {
		t.Errorf("got %v", err)
	}
}

// No capability: every changed one, from the default base (the branch fork point), in git and jj.
func TestShowDiffAllChanged(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		t.Run(kind, func(t *testing.T) {
			root := vcsRepo(t, kind)
			out, errOut, err := runShow(t, root, "--diff")
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			if !strings.Contains(out, "+++ b/rendered/apps/payments/my-app/dev/us-east4-dev-k8s/deployment.yaml") || !strings.Contains(out, "+  replicas: 5") {
				t.Errorf("want my-app diff:\n%s", out)
			}
			if strings.Contains(out, "rendered/apps/search") {
				t.Errorf("unchanged capability present:\n%s", out)
			}
			stat, _, err := runShow(t, root, "--diff", "--stat")
			if err != nil || !strings.Contains(stat, "my-app") || !strings.Contains(stat, "files changed") || strings.Contains(stat, "@@") {
				t.Errorf("stat: %v\n%s", err, stat)
			}
			// a capability deleted in the working tree shows as removed files
			if err := os.RemoveAll(filepath.Join(root, "capabilities/search")); err != nil {
				t.Fatal(err)
			}
			out, _, err = runShow(t, root, "--diff", "--stat")
			if err != nil || !strings.Contains(out, "apps/search/") {
				t.Errorf("deleted capability: %v\n%s", err, out)
			}
		})
	}
}

// With a capability, --stat counts only the selected deploys' files.
func TestShowDiffStatCapability(t *testing.T) {
	root := vcsRepo(t, "git")
	out, _, err := runShow(t, root, "payments/my-app", "--stage", "dev", "--diff", "--stat")
	if err != nil || !strings.Contains(out, "us-east4-dev-k8s/deployment.yaml | +1 -1") || strings.Contains(out, "@@") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestShowDiffNoChange(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		root := vcsRepo(t, kind)
		runShow(t, root, "--diff") // warm: jj snapshots the edit
		// revert the edit: nothing differs from the base
		p := filepath.Join(root, "capabilities/payments/apps/my-app/app.yml")
		b, _ := os.ReadFile(p)
		if err := os.WriteFile(p, []byte(strings.Replace(string(b), "    replicas: 5\n", "    replicas: 1\n", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, _, err := runShow(t, root, "--diff"); err != nil || out != "" {
			t.Errorf("%s: err=%v out=%q", kind, err, out)
		}
	}
}

func TestShowDiffBadBase(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		root := vcsRepo(t, kind)
		bad := "nope"
		if kind == "jj" {
			bad = "all()"
		}
		if _, _, err := runShow(t, root, "--diff", "--base", bad); err == nil {
			t.Errorf("%s: want error for base %q", kind, bad)
		}
	}
}

// An explicit base is used as given: jj "all()" resolves to several commits and is refused with a count.
func TestShowDiffBaseCount(t *testing.T) {
	root := vcsRepo(t, "jj")
	if _, _, err := runShow(t, root, "--diff", "--base", "all()"); err == nil || !strings.Contains(err.Error(), "want exactly 1") {
		t.Errorf("got %v", err)
	}
}
