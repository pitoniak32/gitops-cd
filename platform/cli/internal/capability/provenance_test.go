package capability

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

func srcModel(t *testing.T) *model {
	t.Helper()
	root, _ := filepath.Abs(testdata + "/root")
	m, diags, err := loadModel(Default(), root, "payments", nil)
	if err != nil || len(diags) > 0 {
		t.Fatalf("fixture: %v %v", err, diags)
	}
	return m
}

// lineOf is the 1-based line of the first line containing lit after the first line containing
// anchor in the fixture file rel (anchor "" = from the top).
func lineOf(t *testing.T, rel, anchor, lit string) int {
	t.Helper()
	root, _ := filepath.Abs(testdata + "/root")
	return lineIn(t, root, rel, anchor, lit)
}

func lineIn(t *testing.T, root, rel, anchor, lit string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	i := 0
	if anchor != "" {
		for ; i < len(lines) && !strings.Contains(lines[i], anchor); i++ {
		}
	}
	for ; i < len(lines); i++ {
		if strings.Contains(lines[i], lit) {
			return i + 1
		}
	}
	t.Fatalf("%s: %q after %q not found", rel, lit, anchor)
	return 0
}

func wantSrc(t *testing.T, d *deploy, key string, file string, line int) {
	t.Helper()
	got := d.src[key]
	if got.File != file || got.Line != line {
		t.Errorf("%s src[%s] = %s, want %s:%d", d.key(), key, got, file, line)
	}
}

const myApp = "capabilities/payments/apps/my-app/app.yml"

func TestSrcTargetOverride(t *testing.T) {
	t.Parallel()
	d := srcModel(t).deploy("my-app", "dc1-dev-k8s")
	wantSrc(t, d, "replicas", myApp, lineOf(t, myApp, "dc1-dev-k8s:", "replicas: 3"))
	wantSrc(t, d, "env.APP_ENV", myApp, lineOf(t, myApp, "dc1-dev-k8s:", "APP_ENV: 'false'"))
	wantSrc(t, d, "resources.cpu", myApp, lineOf(t, myApp, "dc1-dev-k8s:", "cpu: 250m"))
	wantSrc(t, d, "artifact", myApp, lineOf(t, myApp, "", "image:"))
	wantSrc(t, d, "port", myApp, lineOf(t, myApp, "", "port: 80"))
}

func TestSrcStageLayer(t *testing.T) {
	t.Parallel()
	d := srcModel(t).deploy("my-app", "us-east4-dev-k8s")
	wantSrc(t, d, "replicas", myApp, lineOf(t, myApp, "  dev:", "replicas: 1"))
	wantSrc(t, d, "env.STAGE_ENV", myApp, lineOf(t, myApp, "  dev:", "STAGE_ENV"))
	wantSrc(t, d, "envFrom.launchdarkly", myApp, lineOf(t, myApp, "  dev:", "envFrom:"))
}

func TestSrcDefaults(t *testing.T) {
	t.Parallel()
	m := srcModel(t)
	if got := m.deploy("my-app", "us-east4-dev-k8s").src["resources.cpu"].String(); got != "global default" {
		t.Errorf("k8s cpu = %q", got)
	}
	// every recorded source is a file:line or a note
	for _, d := range m.deploys {
		for k, s := range d.src {
			if s.File == "" && s.Note == "" || s.File != "" && s.Line == 0 {
				t.Errorf("%s src[%s] = %+v", d.key(), k, s)
			}
		}
		for _, k := range []string{"artifact", "deployWith"} {
			if _, ok := d.src[k]; !ok {
				t.Errorf("%s lacks src[%s]", d.key(), k)
			}
		}
	}
	var cr int
	for _, d := range m.deploys {
		if d.tgt.platform.name == "CLOUD_RUN" {
			cr++
			if s := d.src["replicas"].String(); !strings.HasPrefix(s, "platform default (CLOUD_RUN)") && d.src["replicas"].File == "" {
				t.Errorf("%s replicas src %q", d.key(), s)
			}
		}
	}
	if cr == 0 {
		t.Fatal("no CLOUD_RUN deploy in fixture")
	}
}

func TestSrcForcedDeployWith(t *testing.T) {
	t.Parallel()
	m := srcModel(t)
	var cr int
	for _, d := range m.deploys {
		if d.tgt.platform.name == "CLOUD_RUN" {
			cr++
			if got := d.src["deployWith"].String(); got != "forced by platform" {
				t.Errorf("%s deployWith src %q", d.key(), got)
			}
		}
	}
	if cr == 0 {
		t.Fatal("no CLOUD_RUN deploy in fixture")
	}
	for _, d := range m.deploys {
		if d.tgt.platform.name == "TERRAFORM" {
			if got := d.src["deployWith"].String(); got != "forced by artifact" {
				t.Errorf("%s deployWith src %q", d.key(), got)
			}
		}
	}
}

func TestSrcWorkflow(t *testing.T) {
	t.Parallel()
	const flagged = "capabilities/payments/apps/flagged/app.yml"
	d := srcModel(t).deploy("flagged", "us-east4-dev-k8s")
	wantSrc(t, d, "workflow.repo", flagged, lineOf(t, flagged, "  dev:", "repo:"))
	wantSrc(t, d, "workflow.ref", flagged, lineOf(t, flagged, "  dev:", "ref: v2"))
	wantSrc(t, d, "deployWith", flagged, lineOf(t, flagged, "", "deployWith: gha"))
}

func TestSrcEnvFromBundle(t *testing.T) {
	t.Parallel()
	const bundle = "capabilities/payments/targets/us-east4-dev-k8s/environment-variables/launchdarkly.yml"
	d := srcModel(t).deploy("my-app", "us-east4-dev-k8s")
	wantSrc(t, d, "env.LD_SDK_KEY", bundle, lineOf(t, bundle, "", "LD_SDK_KEY"))
}

// the same key in the bundle and in the app's env: env wins, so the source is app.yml
func TestSrcEnvBeatsBundle(t *testing.T) {
	t.Parallel()
	d := srcModel(t).deploy("my-app", "us-east4-dev-k8s")
	wantSrc(t, d, "env.APP_ENV", myApp, lineOf(t, myApp, "env:", "APP_ENV: 'true'"))
}

func TestSrcWorkflowDefaults(t *testing.T) {
	t.Parallel()
	d := srcModel(t).deploy("my-app", "us-east4-npr-k8s")
	for k, want := range map[string]string{"workflow.repo": "global default", "workflow.ref": "global default", "workflow.file": "platform default (K8S)"} {
		if got := d.src[k].String(); got != want {
			t.Errorf("src[%s] = %q, want %q", k, got, want)
		}
	}
}

func copyFixture(t *testing.T) string {
	t.Helper()
	src, _ := filepath.Abs(testdata + "/root")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if e.IsDir() {
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
	return dst
}

func loadAt(t *testing.T, root string) *model {
	t.Helper()
	m, diags, err := loadModel(Default(), root, "payments", nil)
	if err != nil || len(diags) > 0 {
		t.Fatalf("temp fixture: %v %v", err, diags)
	}
	return m
}

// envFrom.<name> points at the most specific layer listing that name
func TestSrcEnvFromPerName(t *testing.T) {
	t.Parallel()
	root := copyFixture(t)
	dirs, _ := filepath.Glob(filepath.Join(root, "capabilities/payments/targets/*"))
	for _, d := range dirs { // app-level envFrom applies to every target of the app
		if err := os.MkdirAll(filepath.Join(d, "environment-variables"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "environment-variables/extra.yml"), []byte("EXTRA: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := filepath.Join(root, myApp)
	if err := yamledit.Set(app, []string{"envFrom"}, []string{"extra"}); err != nil {
		t.Fatal(err)
	}
	if err := yamledit.Set(app, []string{"stages", "dev", "targets", "us-east4-dev-k8s", "envFrom"}, []string{"launchdarkly"}); err != nil {
		t.Fatal(err)
	}
	d := loadAt(t, root).deploy("my-app", "us-east4-dev-k8s")
	wantSrc(t, d, "envFrom.extra", myApp, lineIn(t, root, myApp, "dc1-prd-k8s", "envFrom:"))
	wantSrc(t, d, "envFrom.launchdarkly", myApp, lineIn(t, root, myApp, "us-east4-dev-k8s:", "envFrom:"))
}

const ownersYml = "capabilities/payments/owners.yml"

func wantOwn(t *testing.T, o owners, app, key string, line int) {
	t.Helper()
	got := o.src[app][key]
	if got.File != ownersYml || got.Line != line {
		t.Errorf("owners.src[%s][%s] = %s, want %s:%d", app, key, got, ownersYml, line)
	}
}

func TestOwnersSrc(t *testing.T) {
	t.Parallel()
	root := copyFixture(t)
	f := filepath.Join(root, ownersYml)
	if err := yamledit.Set(f, []string{"apps", "my-app", "promoters", "prod"}, []string{"x@example.com"}); err != nil {
		t.Fatal(err)
	}
	o := loadAt(t, root).owners
	at := func(anchor, lit string) int { return lineIn(t, root, ownersYml, anchor, lit) }
	// override
	wantOwn(t, o, "my-app", "promoters.prod", at("my-app:", "prod:"))
	// capability-wide
	wantOwn(t, o, "my-app", "promoters.dev", at("", "dev:"))
	wantOwn(t, o, "worker", "promoters.prod", at("worker:", "prod:"))
	wantOwn(t, o, "my-app", "breakGlass", at("", "breakGlass:"))
	wantOwn(t, o, "my-app", "codeOwners", at("", "codeOwners:"))
	wantOwn(t, o, "my-app", "viewers", at("", "viewers:"))
	// worker overrides
	wantOwn(t, o, "worker", "codeOwners", at("worker:", "codeOwners:"))
	wantOwn(t, o, "worker", "viewers", at("worker:", "viewers:"))
	wantOwn(t, o, "worker", "promoters.dev", at("worker:", "dev:"))
	// capability-wide promoters for a stage the app does not override
	wantOwn(t, o, "worker", "promoters.staging", at("", "staging:"))
	if got := o.viewers["worker"]; len(got) != 2 {
		t.Errorf("worker viewers = %v", got)
	}
	if got := o.viewers["my-app"]; len(got) != 1 || got[0] != "bob@example.com" {
		t.Errorf("my-app viewers = %v", got)
	}
}
