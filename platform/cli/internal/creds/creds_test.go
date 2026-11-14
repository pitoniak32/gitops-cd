package creds

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCredsSecret(t *testing.T) {
	vals := map[string]string{
		"GITHUB_APP_ID": "sekret-id-1", "GITHUB_APP_INSTALLATION_ID": "sekret-inst-2",
		"GITHUB_APP_PRIVATE_KEY": "-----BEGIN-----\nsekret-key-3\n-----END-----",
	}
	cs := fake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "github-gitops-cd", Namespace: "kargo-shared-resources"},
			Data: map[string][]byte{"username": []byte("u"), "password": []byte("p")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "repo-gitops-cd", Namespace: "argocd"}},
	)
	var log bytes.Buffer
	get := func(n string) (string, error) { return vals[n], nil }
	if err := Run(context.Background(), &kube.Clients{Ctx: "h", Core: cs}, get, &log); err != nil {
		t.Fatal(err)
	}
	s, err := cs.CoreV1().Secrets("kargo-shared-resources").Get(context.Background(), "github-gitops-cd", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range s.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "githubAppID,githubAppInstallationID,githubAppPrivateKey,repoURL" {
		t.Errorf("keys = %s", got)
	}
	if s.Labels["kargo.akuity.io/cred-type"] != "git" {
		t.Errorf("labels = %v", s.Labels)
	}
	if string(s.Data["githubAppPrivateKey"]) != vals["GITHUB_APP_PRIVATE_KEY"] {
		t.Error("private key altered")
	}
	if _, err := cs.CoreV1().Secrets("argocd").Get(context.Background(), "repo-gitops-cd", metav1.GetOptions{}); err == nil {
		t.Error("repo-gitops-cd not deleted")
	}
	for _, v := range vals {
		for _, line := range strings.Split(v, "\n") {
			if strings.Contains(log.String(), line) && strings.HasPrefix(line, "sekret") {
				t.Errorf("log leaks %q", line)
			}
		}
	}
}

func TestCredsMissingValue(t *testing.T) {
	get := func(string) (string, error) { return "", nil }
	err := Run(context.Background(), &kube.Clients{Core: fake.NewSimpleClientset()}, get, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want error on empty value")
	}
}

func TestCredsIdempotent(t *testing.T) {
	get := func(n string) (string, error) { return "v-" + n, nil }
	cs := fake.NewSimpleClientset()
	hub := &kube.Clients{Core: cs}
	for i := 0; i < 2; i++ {
		if err := Run(context.Background(), hub, get, &bytes.Buffer{}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if _, err := cs.CoreV1().Secrets("kargo-shared-resources").Get(context.Background(), "github-gitops-cd", metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestCredsMissingKey(t *testing.T) {
	get := func(n string) (string, error) {
		if n == "GITHUB_APP_PRIVATE_KEY" {
			return "", nil
		}
		return "v", nil
	}
	err := Run(context.Background(), &kube.Clients{Core: fake.NewSimpleClientset()}, get, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_APP_PRIVATE_KEY is empty in the keychain") {
		t.Fatalf("err = %v", err)
	}
}
