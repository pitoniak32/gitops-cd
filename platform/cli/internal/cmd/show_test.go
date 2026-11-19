package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
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
		"show-rendered-platform.yaml": {"payments/my-app", "--stage", "dev", "--target", "dc1-dev-k8s", "--rendered", "--files", "all"},
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
	dev := app["dev"].(map[string]any)["dc1-dev-k8s"].(map[string]any)
	if dev["replicas"] != 3 || dev["image"] == nil {
		t.Errorf("dev/dc1-dev-k8s: %v", dev)
	}
	if env := dev["env"].(map[string]any); env["STAGE_ENV"] != "false" {
		t.Errorf("STAGE_ENV must stay a string: %#v", env["STAGE_ENV"])
	}
	// the source of a target-layer value is the target file, relative to the system
	out, _, _ := runShow(t, root, "payments/my-app", "--stage", "dev", "--target", "dc1-dev-k8s", "-s")
	if !strings.Contains(out, "# targets/dc1-dev-k8s/target.yml:3") {
		t.Errorf("sources:\n%s", out)
	}
	// a value no layer sets: resources on us-east4-dev-k8s
	out, _, _ = runShow(t, root, "payments/my-app", "--stage", "dev", "--target", "us-east4-dev-k8s", "-s")
	if !strings.Contains(out, "# global default") {
		t.Errorf("sources:\n%s", out)
	}
}

func TestShowLintErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixtureRoot)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
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
		{"payments", "--diff", "--files", "all"},
		{"payments", "--rendered", "--files", "nope"},
		{"payments", "--diff", "--json"},
		{"payments", "--files", "all"},
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
func vcsRepo(t *testing.T, kind string) string { return vcsRepoWith(t, kind, nil) }

// vcsRepoWith is vcsRepo; prepare (if set) runs on the copied fixture before the base commit.
func vcsRepoWith(t *testing.T, kind string, prepare func(root string)) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixtureRoot)); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(root)
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
	p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
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
			out, errOut, err := runShow(t, root, "payments/my-app", "--stage", "dev", "--diff", "--rendered")
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

func TestShowRequiresSystem(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	if _, _, err := runShow(t, root); err == nil || !strings.Contains(err.Error(), "a system is required") {
		t.Errorf("got %v", err)
	}
}

func TestShowUnknownSystem(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	if _, _, err := runShow(t, root, "nope", "--diff"); err == nil || !strings.HasPrefix(err.Error(), "unknown system nope; allowed: ") {
		t.Errorf("got %v", err)
	}
}

// No system: every changed one, from the default base (the branch fork point), in git and jj.
func TestShowDiffAllChanged(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		t.Run(kind, func(t *testing.T) {
			root := vcsRepo(t, kind)
			out, errOut, err := runShow(t, root, "--diff", "--rendered")
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			if !strings.Contains(out, "+++ b/rendered/apps/payments/my-app/dev/us-east4-dev-k8s/deployment.yaml") || !strings.Contains(out, "+  replicas: 5") {
				t.Errorf("want my-app diff:\n%s", out)
			}
			if strings.Contains(out, "rendered/apps/search") {
				t.Errorf("unchanged system present:\n%s", out)
			}
			stat, _, err := runShow(t, root, "--diff", "--rendered", "--stat")
			if err != nil || !strings.Contains(stat, "my-app") || !strings.Contains(stat, "files changed") || strings.Contains(stat, "@@") {
				t.Errorf("stat: %v\n%s", err, stat)
			}
			// a system deleted in the working tree shows as removed files
			if err := os.RemoveAll(filepath.Join(root, "systems/search")); err != nil {
				t.Fatal(err)
			}
			out, _, err = runShow(t, root, "--diff", "--rendered", "--stat")
			if err != nil || !strings.Contains(out, "apps/search/") {
				t.Errorf("deleted system: %v\n%s", err, out)
			}
		})
	}
}

// With a system, --stat counts only the selected deploys' files.
func TestShowDiffStatSystem(t *testing.T) {
	root := vcsRepo(t, "git")
	out, _, err := runShow(t, root, "payments/my-app", "--stage", "dev", "--diff", "--rendered", "--stat")
	if err != nil || !strings.Contains(out, "us-east4-dev-k8s/deployment.yaml | +1 -1") || strings.Contains(out, "@@") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestShowDiffNoChange(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		root := vcsRepo(t, kind)
		runShow(t, root, "--diff") // warm: jj snapshots the edit
		// revert the edit: nothing differs from the base
		p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
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

// writeRendered writes everything gcd render would put under root/rendered.
func writeRendered(t *testing.T, root string) {
	t.Helper()
	files, err := render.AllFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for p, c := range files {
		f := filepath.Join(root, "rendered", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// --against: fresh render vs the rendered/ tree committed at a rev.
func TestShowDiffAgainst(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		t.Run(kind, func(t *testing.T) {
			root := vcsRepoWith(t, kind, func(r string) { writeRendered(t, r) })
			rev := "HEAD"
			if kind == "jj" {
				rev = "@-"
			}
			// the working tree edit (replicas 1 -> 5) is the only change
			out, errOut, err := runShow(t, root, "--diff", "--rendered", "--against", rev)
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			if !strings.Contains(out, "+++ b/rendered/apps/payments/my-app/dev/us-east4-dev-k8s/deployment.yaml") || !strings.Contains(out, "+  replicas: 5") {
				t.Errorf("want my-app diff:\n%s", out)
			}
			if strings.Contains(out, "README") || strings.Contains(out, "rendered/apps/search") {
				t.Errorf("unchanged files present:\n%s", out)
			}
			stat, _, err := runShow(t, root, "--diff", "--rendered", "--against", rev, "--stat")
			if err != nil || !strings.Contains(stat, "us-east4-dev-k8s/deployment.yaml | +1 -1") || strings.Contains(stat, "@@") {
				t.Errorf("stat: %v\n%s", err, stat)
			}
			// with a system, filtered to the selected deploys
			out, _, err = runShow(t, root, "payments/my-app", "--stage", "prod", "--diff", "--rendered", "--against", rev)
			if err != nil || out != "" {
				t.Errorf("unselected stage: %v %q", err, out)
			}
			out, _, err = runShow(t, root, "payments/my-app", "--stage", "dev", "--diff", "--rendered", "--against", rev, "--stat")
			if err != nil || !strings.Contains(out, "us-east4-dev-k8s/deployment.yaml | +1 -1") {
				t.Errorf("sys stat: %v %q", err, out)
			}
			// revert the edit: empty
			p := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, []byte(strings.Replace(string(b), "    replicas: 5\n", "    replicas: 1\n", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, _, err := runShow(t, root, "--diff", "--rendered", "--against", rev); err != nil || out != "" {
				t.Errorf("unchanged: err=%v out=%q", err, out)
			}
			// a file only at the rev is a deletion
			if err := os.RemoveAll(filepath.Join(root, "systems/search")); err != nil {
				t.Fatal(err)
			}
			out, _, err = runShow(t, root, "--diff", "--rendered", "--against", rev, "--stat")
			if err != nil || !strings.Contains(out, "apps/search/") {
				t.Errorf("deleted system: %v\n%s", err, out)
			}
		})
	}
}

// A rev without rendered/ is empty: everything is added.
func TestShowDiffAgainstNoRendered(t *testing.T) {
	root := vcsRepo(t, "git")
	out, _, err := runShow(t, root, "--diff", "--rendered", "--against", "HEAD", "--stat", "--files", "all")
	if err != nil || !strings.Contains(out, "README.md | +") || !strings.Contains(out, "apps/payments/my-app/") {
		t.Errorf("%v\n%s", err, out)
	}
	if strings.Contains(out, " -0\n") == false {
		t.Errorf("want additions only:\n%s", out)
	}
}

func TestShowDiffAgainstFlags(t *testing.T) {
	root := vcsRepo(t, "git")
	for _, args := range [][]string{
		{"--diff", "--rendered", "--against", "HEAD", "--base", "HEAD"},
		{"payments", "--against", "HEAD"},
		{"--diff", "--against", "HEAD"},
		{"--diff", "--rendered", "--against", "nope"},
	} {
		if out, _, err := runShow(t, root, args...); err == nil || out != "" {
			t.Errorf("%v: want error, got %v %q", args, err, out)
		}
	}
}

// --diff alone diffs the merged config (gcd show output): per system, or the selected deploys.
func TestShowConfigDiff(t *testing.T) {
	for _, kind := range []string{"git", "jj"} {
		t.Run(kind, func(t *testing.T) {
			root := vcsRepo(t, kind)
			out, errOut, err := runShow(t, root, "--diff")
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			if !strings.Contains(out, "+++ b/payments\n") || !strings.Contains(out, "+      replicas: 5") || !strings.Contains(out, "-      replicas: 1") {
				t.Errorf("want payments config diff:\n%s", out)
			}
			if strings.Contains(out, "rendered/") || strings.Contains(out, "search") {
				t.Errorf("rendered files or unchanged system present:\n%s", out)
			}
			stat, _, err := runShow(t, root, "--diff", "--stat")
			if err != nil || !strings.Contains(stat, "payments | +") || strings.Contains(stat, "@@") {
				t.Errorf("stat: %v\n%s", err, stat)
			}
			// with a system: only the selected deploys
			out, _, err = runShow(t, root, "payments/my-app", "--stage", "prod", "--diff")
			if err != nil || out != "" {
				t.Errorf("unselected stage: %v %q", err, out)
			}
			out, _, err = runShow(t, root, "payments/my-app", "--stage", "dev", "--diff")
			if err != nil || !strings.Contains(out, "+++ b/payments/my-app\n") || !strings.Contains(out, "+      replicas: 5") {
				t.Errorf("sys: %v\n%s", err, out)
			}
			// a system deleted in the working tree: all removed
			if err := os.RemoveAll(filepath.Join(root, "systems/search")); err != nil {
				t.Fatal(err)
			}
			out, _, err = runShow(t, root, "--diff")
			if err != nil || !strings.Contains(out, "--- a/search\n+++ /dev/null") {
				t.Errorf("deleted system: %v\n%s", err, out)
			}
		})
	}
}

// An app absent at the base diffs from empty.
func TestShowConfigDiffNewApp(t *testing.T) {
	root := vcsRepo(t, "git")
	src := filepath.Join(root, "systems/payments/apps/my-app/app.yml")
	b, _ := os.ReadFile(src)
	dir := filepath.Join(root, "systems/payments/apps/my-new-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.yml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runShow(t, root, "payments/my-new-app", "--diff")
	if err != nil || !strings.Contains(out, "--- /dev/null\n+++ b/payments/my-new-app") {
		t.Errorf("%v %s\n%s", err, errOut, out)
	}
}

// --files splits the rendered diff: app = apps/** only (default), platform = everything else, all = both.
func TestShowDiffFiles(t *testing.T) {
	root := vcsRepo(t, "git")
	stat := func(args ...string) string {
		t.Helper()
		out, errOut, err := runShow(t, root, append([]string{"--diff", "--rendered", "--against", "HEAD", "--stat"}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut)
		}
		return out
	}
	app, platform, all := stat(), stat("--files", "platform"), stat("--files", "all")
	if !strings.Contains(app, "apps/payments/") || strings.Contains(app, "README.md") || strings.Contains(app, "kargo/") {
		t.Errorf("app:\n%s", app)
	}
	if strings.Contains(platform, "apps/") || !strings.Contains(platform, "README.md") || !strings.Contains(platform, "kargo/payments.yaml") {
		t.Errorf("platform:\n%s", platform)
	}
	if !strings.Contains(all, "apps/payments/") || !strings.Contains(all, "README.md") {
		t.Errorf("all:\n%s", all)
	}
	// with a system: platform = the deploy's Argo CD params and Kargo Stage only
	out, _, err := runShow(t, root, "payments/my-app", "--stage", "dev", "--target", "dc1-dev-k8s", "--rendered", "--files", "platform")
	if err != nil || strings.Contains(out, "# rendered/apps/") || !strings.Contains(out, "# rendered/argocd/deploys/") || !strings.Contains(out, "# rendered/kargo/payments.yaml#Stage/") {
		t.Errorf("sys platform: %v\n%s", err, out)
	}
}
