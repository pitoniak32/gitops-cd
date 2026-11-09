package cluster

import (
	"strings"
	"testing"
)

func TestRegionConfig(t *testing.T) {
	in := []byte("# comment\nkind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: region\nkubeadmConfigPatches:\n  - |\n    kind: KubeletConfiguration\n    serializeImagePulls: false\nnodes:\n  - role: control-plane\n")
	out, err := regionConfig(in, "region-us")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "name: region-us\n") || strings.Contains(s, "name: region\n") {
		t.Fatalf("name not replaced:\n%s", s)
	}
	if !strings.Contains(s, "serializeImagePulls: false") || !strings.Contains(s, "role: control-plane") {
		t.Fatalf("lost fields:\n%s", s)
	}
}

func TestRegionConfigNoName(t *testing.T) {
	if _, err := regionConfig([]byte("kind: Cluster\n"), "region-us"); err == nil {
		t.Fatal("want error for missing name")
	}
}
