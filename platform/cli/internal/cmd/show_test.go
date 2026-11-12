package cmd

import (
	"bytes"
	"os"
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
		"show-payments.yaml":       {"payments"},
		"show-my-app-prod.yaml":    {"payments/my-app", "--stage", "prod"},
		"show-my-app-sources.yaml": {"payments/my-app", "--sources"},
		"show-target.yaml":         {"payments", "--target", "dc1-dev-k8s"},
		"show-my-app.json":         {"payments/my-app", "--json"},
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
