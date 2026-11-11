package render

import (
	"testing"

	"cuelang.org/go/cue"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cueload"
)

// #PreDeployCheck (prechecks.cue, ADR 0028): platform config errors fail the render.
func TestPreDeployCheckSchema(t *testing.T) {
	t.Parallel()
	v, err := cueload.Platform(repoRoot(t), "clusters.cue", "prechecks.cue")
	if err != nil {
		t.Fatal(err)
	}
	def := v.LookupPath(cue.ParsePath("#PreDeployCheck"))
	check := func(src map[string]any) error {
		return def.Unify(v.Context().Encode(src)).Validate(cue.Concrete(true))
	}
	for _, ok := range []map[string]any{
		{"file": "precheck.yaml"},
		{"file": "c.yml", "ref": "v1", "timeout": "1m"},
		{"file": "c.yml", "timeout": "120m"},
	} {
		if err := check(ok); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range []map[string]any{
		{},
		{"file": "c.sh"},
		{"file": "c.yml", "timeout": "0m"},
		{"file": "c.yml", "timeout": "121m"},
		{"file": "c.yml", "timeout": "1h"},
		{"file": "c.yml", "ref": ""},
	} {
		if check(bad) == nil {
			t.Errorf("%v: want error", bad)
		}
	}
	var d map[string]any
	if err := def.Unify(v.Context().Encode(map[string]any{"file": "x.yml"})).Decode(&d); err != nil || d["ref"] != "main" || d["timeout"] != "30m" {
		t.Errorf("defaults: %v %v", d, err)
	}
	var prd []map[string]any
	if err := v.LookupPath(cue.ParsePath("preDeploy.prd")).Decode(&prd); err != nil || len(prd) != 1 || prd[0]["file"] != "precheck.yaml" {
		t.Errorf("preDeploy.prd: %v %v", prd, err)
	}
}
