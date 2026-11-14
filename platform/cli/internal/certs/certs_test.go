package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// fakeTrust records trust calls; trusted reports what the "system" trusts.
type fakeTrust struct {
	trusted map[string]bool // CA cert file -> trusted
	added   []string
}

func (f *fakeTrust) Trusted(ca *x509.Certificate, file string) bool { return f.trusted[file] }
func (f *fakeTrust) Add(_ context.Context, file string) error {
	f.added = append(f.added, file)
	f.trusted[file] = true
	return nil
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "platform/cluster"), 0o755)
	os.WriteFile(filepath.Join(root, "platform/cluster/regions"), []byte("# c\neu\nus\n"), 0o644)
	caroot := filepath.Join(t.TempDir(), "ca")
	tr := &fakeTrust{trusted: map[string]bool{}}
	cs := fake.NewSimpleClientset()
	ctx := context.Background()
	if err := Run(ctx, root, &kube.Clients{Core: cs}, CA{Dir: caroot, Trust: tr}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(caroot, "rootCA.pem")
	if len(tr.added) != 1 || tr.added[0] != caFile {
		t.Fatalf("new CA must be trusted once: %v", tr.added)
	}
	s, err := cs.CoreV1().Secrets("traefik").Get(ctx, "local-tls", metav1.GetOptions{})
	if err != nil || s.Type != "kubernetes.io/tls" {
		t.Fatalf("secret = %+v err=%v", s, err)
	}
	// a valid key pair, the leaf verifies against the CA for every UI host (no wildcards: browsers reject them)
	pair, err := tls.X509KeyPair(s.Data["tls.crt"], s.Data["tls.key"])
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	pool := x509.NewCertPool()
	caPEM, _ := os.ReadFile(caFile)
	pool.AppendCertsFromPEM(caPEM)
	for _, h := range []string{"kargo.localhost", "argocd.localhost", "keycloak.localhost", "grafana.localhost", "localhost", "argocd-eu.localhost", "argocd-us.localhost"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: h}); err != nil {
			t.Errorf("%s: %v", h, err)
		}
	}
	if len(leaf.DNSNames) != 7 {
		t.Errorf("names %v", leaf.DNSNames)
	}

	// re-run: the same CA (already trusted) signs a new leaf; no second trust prompt
	before := caPEM
	if err := Run(ctx, root, &kube.Clients{Core: cs}, CA{Dir: caroot, Trust: tr}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(caFile)
	if string(before) != string(after) || len(tr.added) != 1 {
		t.Fatalf("CA must be reused and not re-trusted (added %v)", tr.added)
	}
}

// An existing mkcert CA (rootCA.pem + PKCS#8 rootCA-key.pem) is reused, so a CA the browser already
// trusts keeps working.
func TestReusesExistingCA(t *testing.T) {
	dir := t.TempDir()
	tr := &fakeTrust{trusted: map[string]bool{}}
	ca, key, err := loadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = key
	ca2, _, err := loadOrCreate(dir)
	if err != nil || ca.SerialNumber.Cmp(ca2.SerialNumber) != 0 {
		t.Fatalf("not reused: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "rootCA-key.pem"))
	if blk, _ := pem.Decode(b); blk == nil || blk.Type != "PRIVATE KEY" {
		t.Fatal("key must be PKCS#8 PEM like mkcert's")
	}
	if st, _ := os.Stat(filepath.Join(dir, "rootCA-key.pem")); st.Mode().Perm() != 0o400 {
		t.Fatalf("key mode %v", st.Mode().Perm())
	}
	_ = tr
}
