package certs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "platform/cluster"), 0o755)
	os.WriteFile(filepath.Join(root, "platform/cluster/regions"), []byte("# c\neu\nus\n"), 0o644)
	var calls [][]string
	run := func(_ context.Context, args ...string) error {
		calls = append(calls, args)
		if args[1] == "-cert-file" {
			os.WriteFile(args[2], []byte("CERT"), 0o644)
			os.WriteFile(args[4], []byte("KEY"), 0o644)
		}
		return nil
	}
	cs := fake.NewSimpleClientset()
	if err := Run(context.Background(), root, &kube.Clients{Core: cs}, run, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || strings.Join(calls[0], " ") != "mkcert -install" {
		t.Fatalf("calls = %v", calls)
	}
	hosts := strings.Join(calls[1][5:], " ")
	if hosts != "kargo.localhost argocd.localhost keycloak.localhost localhost argocd-eu.localhost argocd-us.localhost" {
		t.Errorf("hosts = %s", hosts)
	}
	s, err := cs.CoreV1().Secrets("traefik").Get(context.Background(), "local-tls", metav1.GetOptions{})
	if err != nil || string(s.Data["tls.crt"]) != "CERT" || string(s.Data["tls.key"]) != "KEY" || s.Type != "kubernetes.io/tls" {
		t.Fatalf("secret = %+v err=%v", s, err)
	}
}
