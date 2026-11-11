package cueload

import (
	"path/filepath"
	"testing"
)

const testRoot = "../../../render/testdata/root"

func TestInputsOrder(t *testing.T) {
	t.Parallel()
	root, _ := filepath.Abs(testRoot)
	got := Inputs(root, "payments")
	if len(got) == 0 || got[0][:6] != "teams/" {
		t.Fatalf("teams first: %v", got)
	}
	if got[2] != "capabilities/payments/owners.yml" {
		t.Fatalf("owners after teams: %v", got)
	}
}

func TestInputsMissingCap(t *testing.T) {
	t.Parallel()
	root, _ := filepath.Abs(testRoot)
	got := Inputs(root, "nope")
	for _, p := range got {
		if p[:6] != "teams/" {
			t.Fatalf("unexpected %s", p)
		}
	}
}

func TestPlatformClusters(t *testing.T) {
	t.Parallel()
	root, _ := filepath.Abs(testRoot)
	root = filepath.Join(root, "..", "..", "..", "..")
	v, err := Platform(root, "clusters.cue", "regions.cue", "promotiontasks.cue")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := v.LookupPath(Lookup("regionFiles")).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if len(m) == 0 {
		t.Fatal("no region files")
	}
}
