package kargo

import (
	"bytes"
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
)

func TestHubKubeconfig(t *testing.T) {
	b := HubKubeconfig("us", "tok123", "Q0E=")
	cfg, err := clientcmd.Load(b)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	cl := cfg.Clusters["hub"]
	if cl.Server != "https://gitops-cd-control-plane:6443" || string(cl.CertificateAuthorityData) != "CA" {
		t.Errorf("cluster %+v", cl)
	}
	if cfg.AuthInfos["kargo-controller-us"].Token != "tok123" {
		t.Errorf("user %+v", cfg.AuthInfos)
	}
	if cfg.CurrentContext != "hub" || cfg.Contexts["hub"].AuthInfo != "kargo-controller-us" {
		t.Errorf("ctx %+v", cfg)
	}
}

func TestHandoff(t *testing.T) {
	hubCS := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kargo-controller-us-token", Namespace: "kargo"},
		Data:       map[string][]byte{"token": []byte("tok"), "ca.crt": []byte("CA")},
	})
	regCS := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "kargo-controller-us", Namespace: "kargo"}})
	hub, reg := &kube.Clients{Ctx: "hub", Core: hubCS}, &kube.Clients{Ctx: "region", Core: regCS}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := handoff(ctx, hub, reg, "us", &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := regCS.CoreV1().Secrets("kargo").Get(ctx, "kargo-hub-kubeconfig", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientcmd.Load(s.Data["kubeconfig.yaml"]); err != nil {
		t.Fatal(err)
	}
	d, _ := regCS.AppsV1().Deployments("kargo").Get(ctx, "kargo-controller-us", metav1.GetOptions{})
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("controller not restarted")
	}
	// second run is a no-op: restart annotation unchanged
	first := d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]
	d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = "marker"
	_, _ = regCS.AppsV1().Deployments("kargo").Update(ctx, d, metav1.UpdateOptions{})
	_ = handoff(ctx, hub, reg, "us", &bytes.Buffer{})
	d, _ = regCS.AppsV1().Deployments("kargo").Get(ctx, "kargo-controller-us", metav1.GetOptions{})
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] != "marker" {
		t.Errorf("restarted again (first=%s)", first)
	}
}
