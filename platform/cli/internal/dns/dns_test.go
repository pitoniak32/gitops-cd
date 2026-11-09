package dns

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const corefile = ".:53 {\n    errors\n    health\n    ready\n    kubernetes cluster.local\n}\n"

func TestRewriteCorefile(t *testing.T) {
	out, changed := RewriteCorefile(corefile, "keycloak.localhost", "t.svc")
	if !changed {
		t.Fatal("want changed")
	}
	want := "    ready\n    rewrite name keycloak.localhost t.svc\n    kubernetes"
	if !strings.Contains(out, want) {
		t.Fatalf("got %q", out)
	}
	out2, changed2 := RewriteCorefile(out, "keycloak.localhost", "t.svc")
	if changed2 || out2 != out {
		t.Fatal("second call must be a no-op")
	}
}

func TestEnsureRewriteRestartsOnlyWhenChanged(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}, Data: map[string]string{"Corefile": corefile}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}},
	)
	waits := 0
	old := waitRollout
	waitRollout = func(context.Context, *kube.Clients) error { waits++; return nil }
	defer func() { waitRollout = old }()
	c := &kube.Clients{Ctx: "x", Core: cs}
	for i := 0; i < 2; i++ {
		if err := ensureRewrite(context.Background(), c, "keycloak.localhost", "t.svc", &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	if waits != 1 {
		t.Fatalf("waits = %d, want 1", waits)
	}
	cm, _ := cs.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "coredns", metav1.GetOptions{})
	if !strings.Contains(cm.Data["Corefile"], "rewrite name keycloak.localhost t.svc") {
		t.Fatal("corefile not updated")
	}
	d, _ := cs.AppsV1().Deployments("kube-system").Get(context.Background(), "coredns", metav1.GetOptions{})
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatal("restartedAt annotation missing")
	}
}
