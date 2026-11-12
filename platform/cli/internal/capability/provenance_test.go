package capability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	wantSrc(t, d, "envFrom", myApp, lineOf(t, myApp, "  dev:", "envFrom:"))
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
		t.Skip("no CLOUD_RUN deploy in fixture")
	}
}

func TestSrcForcedDeployWith(t *testing.T) {
	t.Parallel()
	m := srcModel(t)
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
