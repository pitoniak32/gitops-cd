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
	k := kargoPayments(t)
	const inf = "infra-us-east4-dev-tf"
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
	if st(t, k, "my-app-us-east4-dev-k8s", `metadata.labels."example.com/platform"`) != "k8s" {
		t.Error("k8s platform label")
	}
	if strings.Join(stepUses(t, k, inf), ",") != "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" {
		t.Error("infra-us-east4-dev-tf steps")
	}
	yu := stepUsing(k, inf, "yaml-update")
	if str(get(yu, "config.path"))+" "+str(get(yu, "config.updates.0.key"))+" "+str(get(yu, "config.updates.0.value")) != `./out/terraform.yaml module ${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}` {
		t.Error("pin module in terraform.yaml")
	}
	if varNames(k, inf) != "repo,src,branch,image,workflowRepo,workflowFile,workflowRef" {
		t.Error("terraform vars (kargo-restore reads image by name)")
	}
	if varValuesFrom4(k, inf) != "pitoniak32/gitops-cd terraform.yaml v1" {
		t.Error("terraform workflow defaults/override")
	}
	body := str(get(stepAs(k, inf, "dispatch"), "config.body"))
	if !strings.Contains(body, "'module': vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest") {
		t.Error("dispatch must send module")
	}
	if strings.Contains(body, "'image':") {
		t.Error("terraform dispatch must not send image")
	}
	if !strings.Contains(str(get(stepUsing(k, inf, "git-commit"), "config.message")), ", module ${{ imageFrom(vars.image).Tag }}") {
		t.Error("commit message names module tag (kargo-restore parses it)")
	}
	if str(get(stepAs(k, inf, "wait"), "retry.timeout"))+" "+str(get(stepAs(k, "worker-us-east4-dev-k8s", "wait"), "retry.timeout")) != "60m 30m" {
		t.Error("wait timeout terraform 60m, k8s 30m")
	}
}
