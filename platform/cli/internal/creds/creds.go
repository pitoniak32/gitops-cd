// Package creds installs Kargo's shared credentials from the keychain (internal/secrets): git (GitHub App) and the GitHub
// Actions token (PAT) that deployWith: gha Stages use to dispatch workflows (ADR 0020).
package creds

import (
	"context"
	"fmt"
	"io"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/secrets"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const repoURL = "https://github.com/pitoniak32/gitops-cd"

// Getter returns the named secret value.
type Getter func(name string) (string, error)

// KeychainGetter reads values from the macOS keychain (`gcd secret set` stores them).
func KeychainGetter(ctx context.Context) Getter {
	return func(name string) (string, error) { return secrets.Default().Get(ctx, name) }
}

// Run writes kargo-shared-resources/github-gitops-cd (App auth only) and github-actions (PAT, read
// by sharedSecret('github-actions').token) and removes the legacy Argo CD repo secret. Secret values
// are never logged.
func Run(ctx context.Context, hub *kube.Clients, get Getter, log io.Writer) error {
	vals := map[string]string{}
	for _, n := range []string{"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY", "GITHUB_PAT"} {
		v, err := get(n)
		if err != nil {
			return err
		}
		if v == "" {
			return fmt.Errorf("%s is empty in the keychain (gcd secret set %s)", n, n)
		}
		vals[n] = v
	}
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "github-gitops-cd", Namespace: "kargo-shared-resources",
			Labels: map[string]string{"kargo.akuity.io/cred-type": "git"},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"repoURL":                 []byte(repoURL),
			"githubAppID":             []byte(vals["GITHUB_APP_ID"]),
			"githubAppInstallationID": []byte(vals["GITHUB_APP_INSTALLATION_ID"]),
			"githubAppPrivateKey":     []byte(vals["GITHUB_APP_PRIVATE_KEY"]),
		},
	}
	// UpsertSecret replaces data wholesale: old PAT keys (username/password) are dropped.
	if err := hub.UpsertSecret(ctx, s); err != nil {
		return err
	}
	fmt.Fprintf(log, "secret %s/%s applied in %s\n", s.Namespace, s.Name, hub.Ctx)
	gha := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "github-actions", Namespace: "kargo-shared-resources",
			Labels: map[string]string{"kargo.akuity.io/cred-type": "generic"},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"token": []byte(vals["GITHUB_PAT"])},
	}
	if err := hub.UpsertSecret(ctx, gha); err != nil {
		return err
	}
	fmt.Fprintf(log, "secret %s/%s applied in %s\n", gha.Namespace, gha.Name, hub.Ctx)
	// earlier versions created an Argo CD repo secret; the public repo needs none
	err := hub.Core.CoreV1().Secrets("argocd").Delete(ctx, "repo-gitops-cd", metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete argocd/repo-gitops-cd: %w", err)
	}
	return nil
}
