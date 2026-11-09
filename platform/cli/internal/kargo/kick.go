package kargo

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var certGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}

// Cert wait knobs (vars so tests can shorten them).
var (
	CertExistTimeout = 600 * time.Second // 300 polls x 2s in the bash original
	CertReadyTimeout = 600 * time.Second
)

const minCerts = 3

// StuckCertPods names Pending pods mounting a Secret (plain or projected) whose name ends in -cert.
func StuckCertPods(pods []corev1.Pod) []string {
	var out []string
	for _, p := range pods {
		if p.Status.Phase != corev1.PodPending {
			continue
		}
		if mountsCert(p) {
			out = append(out, p.Name)
		}
	}
	return out
}

func mountsCert(p corev1.Pod) bool {
	for _, v := range p.Spec.Volumes {
		if v.Secret != nil && strings.HasSuffix(v.Secret.SecretName, "-cert") {
			return true
		}
		if v.Projected != nil {
			for _, s := range v.Projected.Sources {
				if s.Secret != nil && strings.HasSuffix(s.Secret.Name, "-cert") {
					return true
				}
			}
		}
	}
	return false
}

func certsReady(items []unstructured.Unstructured) bool {
	for _, c := range items {
		conds, _, _ := unstructured.NestedSlice(c.Object, "status", "conditions")
		ok := false
		for _, x := range conds {
			m, _ := x.(map[string]any)
			if m["type"] == "Ready" && m["status"] == "True" {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func waitCerts(ctx context.Context, hub *kube.Clients, timeout time.Duration, what string, done func([]unstructured.Unstructured) bool) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		l, err := hub.Dyn.Resource(certGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err == nil && done(l.Items) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s in %s", what, hub.Ctx)
		case <-time.After(PollInterval):
		}
	}
}

// KickCertPods: kargo pods start before cert-manager has issued their TLS Secrets; kubelet then backs
// off retrying the mount (~2 min after the Secret exists). Once the Certificates are Ready, recreate
// pods still stuck.
func KickCertPods(ctx context.Context, hub *kube.Clients, log io.Writer) error {
	if err := waitCerts(ctx, hub, CertExistTimeout, fmt.Sprintf(">=%d Certificates in %s", minCerts, ns),
		func(i []unstructured.Unstructured) bool { return len(i) >= minCerts }); err != nil {
		return err
	}
	if err := waitCerts(ctx, hub, CertReadyTimeout, "Certificates Ready", certsReady); err != nil {
		return err
	}
	pods, err := hub.Core.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pods in %s: %w", hub.Ctx, err)
	}
	for _, name := range StuckCertPods(pods.Items) {
		if err := hub.Core.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("delete pod %s: %w", name, err)
		}
		fmt.Fprintf(log, "deleted stuck pod %s/%s\n", ns, name)
	}
	return nil
}
