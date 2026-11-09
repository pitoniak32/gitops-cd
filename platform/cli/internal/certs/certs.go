// Package certs issues browser-trusted *.localhost certificates with mkcert for traefik.
package certs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Runner executes an external command: args[0] is the program.
type Runner func(ctx context.Context, args ...string) error

// ExecRunner runs the command with stdio attached (mkcert -install may prompt for a password).
func ExecRunner(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	return nil
}

// Run trusts the mkcert CA, issues a cert for the UI hosts and stores it as traefik/local-tls on the hub.
func Run(ctx context.Context, root string, hub *kube.Clients, run Runner, log io.Writer) error {
	rs, err := regions.List(root)
	if err != nil {
		return err
	}
	if err := run(ctx, "mkcert", "-install"); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "gcd-certs")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	crt, key := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	// explicit names: browsers reject *.localhost wildcards
	args := []string{"mkcert", "-cert-file", crt, "-key-file", key,
		"kargo.localhost", "argocd.localhost", "keycloak.localhost", "localhost"}
	for _, r := range rs {
		args = append(args, "argocd-"+r+".localhost")
	}
	if err := run(ctx, args...); err != nil {
		return err
	}
	crtB, err := os.ReadFile(crt)
	if err != nil {
		return err
	}
	keyB, err := os.ReadFile(key)
	if err != nil {
		return err
	}
	// traefik's TLSStore default points at this secret
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "traefik"}}
	if _, err := hub.Core.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace traefik: %w", err)
	}
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "local-tls", Namespace: "traefik"},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: crtB, corev1.TLSPrivateKeyKey: keyB},
	}
	if err := hub.UpsertSecret(ctx, s); err != nil {
		return err
	}
	fmt.Fprintf(log, "secret traefik/local-tls applied in %s\n", hub.Ctx)
	return nil
}
