package cluster

import (
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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

func kindKubeconfig(t *testing.T, dir, name, server string) string {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	n := "kind-" + name
	cfg.Clusters[n] = &clientcmdapi.Cluster{Server: server}
	cfg.AuthInfos[n] = &clientcmdapi.AuthInfo{Token: "t-" + server}
	cfg.Contexts[n] = &clientcmdapi.Context{Cluster: n, AuthInfo: n}
	cfg.CurrentContext = n
	p := filepath.Join(dir, name+".kubeconfig")
	if err := clientcmd.WriteToFile(*cfg, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergeKubeconfig(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "kubeconfig")
	// stale entry for region-us + an unrelated context that must survive
	old := kindKubeconfig(t, dir, "region-us", "https://old")
	other := kindKubeconfig(t, dir, "other", "https://other")
	for _, src := range []string{old, other} {
		if err := mergeKubeconfig(dst, src); err != nil {
			t.Fatal(err)
		}
	}
	for _, src := range []string{kindKubeconfig(t, dir, "kargo-demo", "https://hub"), kindKubeconfig(t, dir, "region-us", "https://new")} {
		if err := mergeKubeconfig(dst, src); err != nil {
			t.Fatal(err)
		}
	}
	if err := useContext(dst, "kind-kargo-demo"); err != nil {
		t.Fatal(err)
	}
	got, err := clientcmd.LoadFromFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contexts) != 3 || len(got.Clusters) != 3 || len(got.AuthInfos) != 3 {
		t.Fatalf("entries: %d contexts %d clusters %d users", len(got.Contexts), len(got.Clusters), len(got.AuthInfos))
	}
	if s := got.Clusters["kind-region-us"].Server; s != "https://new" {
		t.Fatalf("region-us server = %s", s)
	}
	if got.AuthInfos["kind-region-us"].Token != "t-https://new" {
		t.Fatal("region-us user not replaced")
	}
	if got.Clusters["kind-other"] == nil || got.Clusters["kind-kargo-demo"].Server != "https://hub" {
		t.Fatal("other/hub missing")
	}
	if got.CurrentContext != "kind-kargo-demo" {
		t.Fatalf("current-context = %s", got.CurrentContext)
	}
}

func TestClustersToDelete(t *testing.T) {
	got := clustersToDelete([]string{"region-us", "region-onprem", "other", "region-us-east4", Hub}, []string{"us-east4", "dc1"})
	want := []string{Hub, "region-us-east4", "region-dc1", "region-us", "region-onprem"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := clustersToDelete(nil, nil); len(got) != 1 || got[0] != Hub {
		t.Fatalf("empty: %v", got)
	}
}
