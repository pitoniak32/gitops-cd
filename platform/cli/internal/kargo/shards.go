// Package kargo wires the distributed Kargo shards, unsticks cert-gated pods and restores Kargo state.
package kargo

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/argocd"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const (
	hubServer = "https://gitops-cd-control-plane:6443" // hub apiserver on docker network gitops-cd
	ns        = "kargo"
	restarted = "kubectl.kubernetes.io/restartedAt"
)

// Timing knobs (vars so tests can shorten them).
var (
	PollInterval = 2 * time.Second
	TokenTimeout = 60 * time.Second
)

// HubKubeconfig is the kubeconfig a regional Kargo controller uses to phone home to the hub.
// caB64 is the base64 of the hub CA (as stored in the token Secret's ca.crt, base64-encoded).
func HubKubeconfig(region, token, caB64 string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: hub
    cluster: {server: %s, certificate-authority-data: %s}
users:
  - name: kargo-controller-%s
    user: {token: %s}
contexts:
  - name: hub
    context: {cluster: hub, user: kargo-controller-%s}
current-context: hub
`, hubServer, caB64, region, token, region))
}

// Shards sets up the Kargo controller shard per region: hub identity, token handoff, regional
// controller. Must run after the hub kargo app is Healthy and each region's Argo CD exists.
func Shards(ctx context.Context, root string, hub *kube.Clients, regs map[string]*kube.Clients, log io.Writer) error {
	rs, err := regions.List(root)
	if err != nil {
		return err
	}
	// 1. hub: shard identities (SA, RBAC, token Secret), from the kargo-shard-hub ApplicationSet
	for _, r := range rs {
		if err := argocd.WaitApp(ctx, hub, "kargo-ctrl-"+r, log); err != nil {
			return err
		}
	}
	// 2. token handoff hub -> region (host-side), then 3. regional controller
	for _, r := range rs {
		reg := regs[r]
		if reg == nil {
			return fmt.Errorf("no client for region %s", r)
		}
		if err := handoff(ctx, hub, reg, r, log); err != nil {
			return err
		}
		if err := argocd.WaitApp(ctx, reg, "kargo-ctrl", log); err != nil { // from the kargo-shard ApplicationSet
			return err
		}
	}
	return nil
}

func handoff(ctx context.Context, hub, reg *kube.Clients, r string, log io.Writer) error {
	name := "kargo-controller-" + r + "-token"
	var sec *corev1.Secret
	tctx, cancel := context.WithTimeout(ctx, TokenTimeout)
	defer cancel()
	for {
		s, err := hub.Core.CoreV1().Secrets(ns).Get(tctx, name, metav1.GetOptions{})
		if err == nil && len(s.Data["token"]) > 0 {
			sec = s
			break
		}
		select {
		case <-tctx.Done():
			return fmt.Errorf("no token in kargo/%s on hub", name)
		case <-time.After(PollInterval):
		}
	}
	kcfg := HubKubeconfig(r, string(sec.Data["token"]), base64.StdEncoding.EncodeToString(sec.Data["ca.crt"]))

	_, err := reg.Core.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{FieldManager: kube.FieldManager})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s in %s: %w", ns, reg.Ctx, err)
	}
	old, err := reg.Core.CoreV1().Secrets(ns).Get(ctx, "kargo-hub-kubeconfig", metav1.GetOptions{})
	if err == nil && string(bytes.TrimRight(old.Data["kubeconfig.yaml"], "\n")) == strings.TrimRight(string(kcfg), "\n") {
		return nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("get kargo-hub-kubeconfig in %s: %w", reg.Ctx, err)
	}
	err = reg.UpsertSecret(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kargo-hub-kubeconfig", Namespace: ns},
		Data:       map[string][]byte{"kubeconfig.yaml": kcfg},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "kargo-hub-kubeconfig updated in %s\n", reg.Ctx)
	// token changed: running controller still has the old one
	deps := reg.Core.AppsV1().Deployments(ns)
	ctrl := "kargo-controller-" + r
	found := true
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		d, err := deps.Get(ctx, ctrl, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		if d.Spec.Template.Annotations == nil {
			d.Spec.Template.Annotations = map[string]string{}
		}
		d.Spec.Template.Annotations[restarted] = time.Now().Format(time.RFC3339)
		_, err = deps.Update(ctx, d, metav1.UpdateOptions{FieldManager: kube.FieldManager})
		return err
	})
	if err != nil {
		return fmt.Errorf("restart %s in %s: %w", ctrl, reg.Ctx, err)
	}
	if !found {
		return nil
	}
	fmt.Fprintf(log, "restarted kargo-controller-%s in %s\n", r, reg.Ctx)
	return nil
}
