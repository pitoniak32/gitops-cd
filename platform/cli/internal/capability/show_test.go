package capability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowFilterAndOwners(t *testing.T) {
	root, _ := filepath.Abs(testdata + "/root")
	r, err := Show(Default(), root, "payments", nil, ShowFilter{App: "my-app", Stage: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Deploys) != 2 || r.Deploys[0].Stage != "dev" {
		t.Fatalf("deploys: %+v", r.Deploys)
	}
	var paths []string
	for _, v := range r.Owners["my-app"] {
		paths = append(paths, v.Path)
	}
	if got := strings.Join(paths, ","); got != "codeOwners,promoters.dev,viewers,breakGlass" {
		t.Errorf("owners: %s", got)
	}
	if _, err := Show(Default(), root, "payments", nil, ShowFilter{Target: "x"}); err == nil || !strings.HasPrefix(err.Error(), "unknown target x; allowed: ") {
		t.Errorf("got %v", err)
	}
}

// A target.yml without platform is K8S by the global default.
func TestShowAbsentPlatform(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(testdata+"/root")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "capabilities/payments/targets/dc1-dev-k8s/target.yml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "  platform: K8S\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Show(Default(), root, "payments", nil, ShowFilter{App: "my-app", Target: "dc1-dev-k8s"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range r.Deploys[0].Values {
		if v.Path == "config.platform" {
			if v.Value != "K8S" || v.Source.Note != "global default" {
				t.Errorf("got %+v", v)
			}
			return
		}
	}
	t.Error("no config.platform value")
}

// --stage/--target are checked against the selected app's deploys, not the whole capability.
func TestShowNoMatchingDeploys(t *testing.T) {
	root, _ := filepath.Abs(testdata + "/root")
	_, err := Show(Default(), root, "payments", nil, ShowFilter{App: "flagged", Target: "dc1-dev-k8s"})
	if err == nil || !strings.HasPrefix(err.Error(), "no deploys of flagged match --target dc1-dev-k8s; flagged deploys to: ") {
		t.Errorf("got %v", err)
	}
	_, err = Show(Default(), root, "payments", nil, ShowFilter{Stage: "dev", Target: "nope-none"})
	if err == nil {
		t.Error("expected error")
	}
}
