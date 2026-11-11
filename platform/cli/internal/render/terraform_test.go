package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// test.sh: "terraform render ok" (ADR 0021)
func TestTerraformRender(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	tf := filepath.Join(out, "apps/payments/infra")
	ents, err := os.ReadDir(filepath.Join(tf, "dev/us-east4-dev-tf"))
	if err != nil {
		t.Fatal(err)
	}
	var ns []string
	for _, e := range ents {
		ns = append(ns, e.Name())
	}
	if strings.Join(ns, " ") != "terraform.yaml" {
		t.Errorf("terraform target renders only terraform.yaml: %s", strings.Join(ns, " "))
	}
	want := `{"capability":"payments","app":"infra","stage":"dev","target":"us-east4-dev-tf","environment":"dev","gcpProject":"team1-dev","region":"us-east4","wifEmail":"team1-dev@team1-dev.iam.gserviceaccount.com","backend":{"bucket":"tfstate-team1-dev","prefix":"payments/infra/us-east4-dev-tf"},"module":"ghcr.io/pitoniak32/tf-bucket","env":{"TF_VAR_bucket_name":"payments-dev","TF_VAR_region":"us-central1"}}`
	if got := orderedJSON(t, filepath.Join(tf, "dev/us-east4-dev-tf/terraform.yaml"), ""); got != want {
		t.Errorf("us-east4-dev-tf terraform.yaml\nwant: %s\ngot:  %s", want, got)
	}
	p := docs(t, filepath.Join(tf, "prod/us-east4-prd-tf/terraform.yaml"))[0]
	if str(get(p, "backend.prefix"))+" "+str(get(p, "env.TF_VAR_region")) != "payments/custom us-east1" {
		t.Error("us-east4-prd-tf prefix override / target env")
	}
	if m := globOne(t, filepath.Join(out, "argocd/deploys/*/payments/infra-*")); len(m) > 0 {
		t.Error("terraform targets must not get Argo CD params")
	}
}

// test.sh: "terraform kargo ok"
// Warehouse watches the module as an image (no platform), Stage runs on the hub controller
// (no shard), pins module in terraform.yaml, dispatches with `module`, waits 60m
func TestTerraformKargo(t *testing.T) {
	t.Parallel()
	k := kargoApp(t, "infra")
	const inf = "us-east4-dev-tf"
	wh := find(k, "Warehouse", "infra")
	img := get(wh, "spec.subscriptions.1.image")
	_, hasPlatform := get(wh, "spec.subscriptions.1.image").(map[string]any)["platform"]
	if str(get(img, "repoURL"))+" "+str(get(img, "imageSelectionStrategy"))+" "+str(get(img, "constraint"))+" "+str(get(img, "discoveryLimit"))+" "+map[bool]string{true: "true", false: "false"}[hasPlatform] != "ghcr.io/pitoniak32/tf-bucket SemVer ^1 10 false" {
		t.Error("infra Warehouse module subscription")
	}
	shard := st(t, k, inf, "spec.shard")
	if shard == "" {
		shard = "-"
	}
	if shard+" "+st(t, k, inf, `metadata.labels."example.com/platform"`)+" "+st(t, k, inf, `metadata.labels."example.com/deploy-with"`) != "- terraform gha" {
		t.Error("infra-us-east4-dev-tf: no shard, platform/deploy-with labels")
	}
	if st(t, kargoApp(t, "my-app"), "us-east4-dev-k8s", `metadata.labels."example.com/platform"`) != "k8s" {
		t.Error("k8s platform label")
	}
	if strings.Join(stepUses(t, k, inf), ",") != "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" {
		t.Error("infra-us-east4-dev-tf steps")
	}
	if varValue(k, inf, "pinFile")+" "+varValue(k, inf, "pinKey")+" "+varValue(k, inf, "artifactKey") != "./out/terraform.yaml module module" {
		t.Error("pin module in terraform.yaml")
	}
	yu := stepUsing(t, k, inf, "yaml-update")
	if str(get(yu, "config.path"))+" "+str(get(yu, "config.updates.0.key"))+" "+str(get(yu, "config.updates.0.value")) != `${{ vars.pinFile }} ${{ vars.pinKey }} ${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}` {
		t.Error("yaml-update pins vars.image at vars.pinFile/pinKey")
	}
	if varNames(k, inf) != ghaVars {
		t.Error("terraform vars (kargo-restore reads image by name)")
	}
	if varValue(k, inf, "workflowRepo")+" "+varValue(k, inf, "workflowFile")+" "+varValue(k, inf, "workflowRef") != "pitoniak32/gitops-cd terraform.yaml v1" {
		t.Error("terraform workflow defaults/override")
	}
	// dispatch input key and commit message word come from artifactKey (module here)
	if !strings.Contains(str(get(stepUsing(t, k, inf, "git-commit"), "config.message")), ", ${{ vars.artifactKey }} ${{ imageFrom(vars.image).Tag }}") {
		t.Error("commit message names artifact tag (kargo-restore parses it)")
	}
}
