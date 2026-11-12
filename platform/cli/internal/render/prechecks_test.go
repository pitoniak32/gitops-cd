package render

import (
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
)

// capability.Check (ADR 0028): platform config errors fail the render.
func TestPreDeployCheckSchema(t *testing.T) {
	t.Parallel()
	for _, ok := range []capability.Check{
		{File: "precheck.yaml"},
		{File: "c.yml", Ref: "v1", Timeout: "1m"},
		{File: "c.yml", Timeout: "120m"},
	} {
		if _, err := ok.Normalize(); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range []capability.Check{
		{},
		{File: "c.sh"},
		{File: "c.yml", Timeout: "0m"},
		{File: "c.yml", Timeout: "121m"},
		{File: "c.yml", Timeout: "1h"},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
	if d, err := (capability.Check{File: "x.yml"}).Normalize(); err != nil || d.Ref != "main" || d.Timeout != "30m" {
		t.Errorf("defaults: %v %v", d, err)
	}
	prd := capability.Default().PreDeploy["prd"]
	if len(prd) != 1 || prd[0].File != "precheck.yaml" {
		t.Errorf("PreDeploy prd: %v", prd)
	}
	// a bad check in the platform config fails every render
	cfg := capability.Default()
	cfg.PreDeploy["prd"] = []capability.Check{{File: "c.sh"}}
	if _, err := capability.Render(cfg, abs(t, "testdata/root"), "payments"); err == nil {
		t.Error("render with an invalid pre-deploy check: want error")
	}
}
