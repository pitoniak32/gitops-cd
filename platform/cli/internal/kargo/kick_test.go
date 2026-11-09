package kargo

import (
	"bytes"
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
)

func pod(name string, phase corev1.PodPhase, vols ...corev1.Volume) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kargo"},
		Spec: corev1.PodSpec{Volumes: vols}, Status: corev1.PodStatus{Phase: phase}}
}

func secretVol(n string) corev1.Volume {
	return corev1.Volume{Name: "v", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: n}}}
}

func TestStuckCertPods(t *testing.T) {
	proj := corev1.Volume{Name: "p", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
		Sources: []corev1.VolumeProjection{{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "x-cert"}}}}}}}
	cm := corev1.Volume{Name: "c", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
		LocalObjectReference: corev1.LocalObjectReference{Name: "foo-cert"}}}}
	pods := []corev1.Pod{
		pod("plain", corev1.PodPending, secretVol("api-cert")),
		pod("projected", corev1.PodPending, proj),
		pod("configmap", corev1.PodPending, cm),
		pod("othersecret", corev1.PodPending, secretVol("other")),
		pod("running", corev1.PodRunning, secretVol("api-cert")),
	}
	got := StuckCertPods(pods)
	if len(got) != 2 || got[0] != "plain" || got[1] != "projected" {
		t.Fatalf("%v", got)
	}
}

func cert(n string, ready bool) *unstructured.Unstructured {
	st := "False"
	if ready {
		st = "True"
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"name": n, "namespace": "kargo"},
		"status":   map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": st}}},
	}}
}

func TestKickCertPods(t *testing.T) {
	old := PollInterval
	PollInterval = time.Millisecond
	defer func() { PollInterval = old }()
	dyn := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{certGVR: "CertificateList"}, cert("a", true), cert("b", true), cert("c", true))
	p1, p2 := pod("stuck", corev1.PodPending, secretVol("api-cert")), pod("ok", corev1.PodRunning, secretVol("api-cert"))
	core := k8sfake.NewSimpleClientset(&p1, &p2)
	hub := &kube.Clients{Ctx: "hub", Dyn: dyn, Core: core}
	if err := KickCertPods(context.Background(), hub, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	l, _ := core.CoreV1().Pods("kargo").List(context.Background(), metav1.ListOptions{})
	if len(l.Items) != 1 || l.Items[0].Name != "ok" {
		t.Fatalf("%v", l.Items)
	}
}
