// Package argocd bootstraps Argo CD (helm install, root app) and waits for Applications.
package argocd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AppGVR is the Argo CD Application resource.
var AppGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

// Namespace holds Argo CD and its Applications.
const Namespace = "argocd"

// RolloutTimeout bounds each controller rollout wait in ApplyRoot (var for tests).
var RolloutTimeout = 300 * time.Second

// ApplyRoot waits for Argo CD's controllers, applies platform/bootstrap/root.yaml and waits for the
// root app. A root created before the application-controller runs idles until the next periodic reconcile.
func ApplyRoot(ctx context.Context, c *kube.Clients, root string) error {
	if err := waitRollout(ctx, c, "statefulset", "argocd-application-controller"); err != nil {
		return err
	}
	for _, d := range []string{"argocd-repo-server", "argocd-applicationset-controller"} {
		if err := waitRollout(ctx, c, "deployment", d); err != nil {
			return err
		}
	}
	y, err := os.ReadFile(filepath.Join(root, "platform", "bootstrap", "root.yaml"))
	if err != nil {
		return fmt.Errorf("read root.yaml: %w", err)
	}
	if err := c.ApplyYAML(ctx, y, kube.FieldManager); err != nil {
		return fmt.Errorf("apply root app: %w", err)
	}
	return WaitApp(ctx, c, "root", os.Stderr)
}

func waitRollout(ctx context.Context, c *kube.Clients, kind, name string) error {
	return poll(ctx, PollInterval, RolloutTimeout, fmt.Sprintf("rollout of %s/%s in %s", kind, name, c.Ctx), func() (bool, error) {
		switch kind {
		case "statefulset":
			s, err := c.Core.AppsV1().StatefulSets(Namespace).Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			want := int32(1)
			if s.Spec.Replicas != nil {
				want = *s.Spec.Replicas
			}
			return s.Status.ObservedGeneration >= s.Generation && s.Status.UpdatedReplicas == want && s.Status.ReadyReplicas == want, nil
		default:
			d, err := c.Core.AppsV1().Deployments(Namespace).Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			return d.Status.ObservedGeneration >= d.Generation && d.Status.UpdatedReplicas == want &&
				d.Status.ReadyReplicas == want && d.Status.Replicas == want, nil
		}
	})
}
