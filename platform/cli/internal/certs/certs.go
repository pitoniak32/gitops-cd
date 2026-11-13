// Package certs issues browser-trusted *.localhost certificates for traefik from a local CA, the way
// mkcert does (and reusing an mkcert CA when one exists, so an already-trusted CA keeps working).
package certs

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	caFile    = "rootCA.pem"
	caKeyFile = "rootCA-key.pem"
	// Apple rejects TLS server certificates valid for more than 825 days
	leafValidity = 825 * 24 * time.Hour
)

// Truster checks and adds trust for a CA certificate in the OS trust store.
type Truster interface {
	Trusted(ca *x509.Certificate, file string) bool
	Add(ctx context.Context, file string) error
}

// CA is where the local CA lives (Dir) and how it gets trusted.
type CA struct {
	Dir   string
	Trust Truster
}

// DefaultCA is the CA in CAROOT, else an existing mkcert CA, else a new one in the user config dir;
// trusted through the macOS login keychain.
func DefaultCA() CA { return CA{Dir: caDir(), Trust: keychainTrust{}} }

// caDir: $CAROOT, else mkcert's CAROOT if it holds a CA ($XDG_DATA_HOME/mkcert, ~/.local/share/mkcert,
// ~/Library/Application Support/mkcert), else <user config dir>/gitops-cd/ca.
func caDir() string {
	if d := os.Getenv("CAROOT"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	var candidates []string
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		candidates = append(candidates, filepath.Join(x, "mkcert"))
	}
	candidates = append(candidates, filepath.Join(home, ".local/share/mkcert"), filepath.Join(home, "Library/Application Support/mkcert"))
	for _, d := range candidates {
		if _, err := os.Stat(filepath.Join(d, caFile)); err == nil {
			return d
		}
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "gitops-cd", "ca")
}

// Run makes sure the CA exists and is trusted, issues a cert for the UI hosts and stores it as
// traefik/local-tls on the hub.
func Run(ctx context.Context, root string, hub *kube.Clients, ca CA, log io.Writer) error {
	rs, err := regions.List(root)
	if err != nil {
		return err
	}
	// explicit names: browsers reject *.localhost wildcards
	hosts := []string{"kargo.localhost", "argocd.localhost", "keycloak.localhost", "localhost"}
	for _, r := range rs {
		hosts = append(hosts, "argocd-"+r+".localhost")
	}
	caCert, caKey, err := loadOrCreate(ca.Dir)
	if err != nil {
		return err
	}
	file := filepath.Join(ca.Dir, caFile)
	if !ca.Trust.Trusted(caCert, file) {
		fmt.Fprintf(log, "trusting local CA %s (macOS asks for your password once)\n", file)
		if err := ca.Trust.Add(ctx, file); err != nil {
			return err
		}
	}
	crtB, keyB, err := issue(caCert, caKey, hosts)
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

// loadOrCreate reads the CA in dir (mkcert's layout: rootCA.pem, PKCS#8 rootCA-key.pem), or creates one.
func loadOrCreate(dir string) (*x509.Certificate, crypto.Signer, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, caFile))
	if errors.Is(err, fs.ErrNotExist) {
		return create(dir)
	}
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		return nil, nil, fmt.Errorf("CA key: %w", err)
	}
	cb, kb := pemBlock(certPEM), pemBlock(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, fmt.Errorf("CA in %s: not PEM", dir)
	}
	cert, err := x509.ParseCertificate(cb)
	if err != nil {
		return nil, nil, fmt.Errorf("CA cert: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(kb)
	if err != nil {
		return nil, nil, fmt.Errorf("CA key: %w", err)
	}
	signer, ok := k.(crypto.Signer)
	if !ok {
		return nil, nil, fmt.Errorf("CA key: unsupported type %T", k)
	}
	return cert, signer, nil
}

func pemBlock(b []byte) []byte {
	if blk, _ := pem.Decode(b); blk != nil {
		return blk.Bytes
	}
	return nil
}

// create makes a 10-year RSA-3072 CA like mkcert's.
func create(dir string) (*x509.Certificate, crypto.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, nil, err
	}
	who := "gitops-cd"
	if u, err := user.Current(); err == nil {
		h, _ := os.Hostname()
		who = u.Username + "@" + h
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{Organization: []string{"gitops-cd development CA"}, OrganizationalUnit: []string{who}, CommonName: "gitops-cd " + who},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, caKeyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0o400); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, caFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return cert, key, err
}

// issue signs an ECDSA P-256 server cert for hosts: PEM cert, PEM PKCS#8 key.
func issue(ca *x509.Certificate, caKey crypto.Signer, hosts []string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{Organization: []string{"gitops-cd development certificate"}},
		DNSNames:     hosts,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), nil
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	return n
}

// keychainTrust uses the OS trust store: on macOS the login keychain via /usr/bin/security (Chrome,
// Safari, Brave; Firefox keeps its own store).
type keychainTrust struct{}

// Trusted: the system verifier accepts the CA as a root.
func (keychainTrust) Trusted(ca *x509.Certificate, _ string) bool {
	_, err := ca.Verify(x509.VerifyOptions{})
	return err == nil
}

func (keychainTrust) Add(ctx context.Context, file string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("trust %s in your OS/browser trust store by hand (automatic only on macOS)", file)
	}
	home, _ := os.UserHomeDir()
	c := exec.CommandContext(ctx, "/usr/bin/security", "add-trusted-cert", "-r", "trustRoot",
		"-k", filepath.Join(home, "Library/Keychains/login.keychain-db"), file)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("security add-trusted-cert: %w", err)
	}
	return nil
}
