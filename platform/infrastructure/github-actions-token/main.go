// Command github-actions-token keeps kargo-shared-resources/github-actions holding a fresh GitHub App
// installation token (ADR 0020).
//
// Kargo's gha steps dispatch workflows with `sharedSecret('github-actions').token`; an App token makes the
// runs show as `<app>[bot]` instead of a user. Installation tokens live 1h and Kargo expressions can't mint
// one, so this loop mints a new one every refresh interval from the App key that `gcd creds` stores in
// kargo-shared-resources/github-gitops-cd. Stdlib only; built into a scratch image (Dockerfile) that
// `gcd infra` loads into the hub cluster. Serves /readyz on :8080 once the first token is written.
// Never logs a secret value.
package main

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	namespace = "kargo-shared-resources"
	srcSecret = "github-gitops-cd" // App ID, installation ID, private key (gcd creds)
	dstSecret = "github-actions"   // read by sharedSecret('github-actions').token
	saDir     = "/var/run/secrets/kubernetes.io/serviceaccount"
)

func main() {
	refresh := 30 * time.Minute // tokens expire after 1h
	if s := os.Getenv("REFRESH_SECONDS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			log.Fatalf("REFRESH_SECONDS: %v", err)
		}
		refresh = time.Duration(n) * time.Second
	}
	k, err := inCluster()
	if err != nil {
		log.Fatal(err)
	}
	gh := &GitHub{URL: "https://api.github.com", HTTP: &http.Client{Timeout: 30 * time.Second}}
	var ready atomic.Bool
	go serveReady(":8080", &ready)
	for {
		// missing source secret (before gcd creds), GitHub or API errors: retry in a minute
		wait := time.Minute
		if exp, err := refreshOnce(k, gh, time.Now()); err != nil {
			log.Printf("refresh failed, retry in %s: %v", wait, err)
		} else {
			ready.Store(true)
			log.Printf("%s/%s refreshed, expires %s", namespace, dstSecret, exp)
			wait = refresh
		}
		time.Sleep(wait)
	}
}

// serveReady answers the readinessProbe: 200 once a token has been written.
func serveReady(addr string, ready *atomic.Bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(w, "no token yet", http.StatusServiceUnavailable)
		}
	})
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

// refreshOnce mints a token from the App key in the source secret and writes it to the target secret.
func refreshOnce(k *Kube, gh *GitHub, now time.Time) (string, error) {
	src, err := k.GetSecret(srcSecret)
	if err != nil {
		return "", err
	}
	key, err := ParseKey(src["githubAppPrivateKey"])
	if err != nil {
		return "", err
	}
	tok, exp, err := gh.Mint(src["githubAppID"], src["githubAppInstallationID"], key, now)
	if err != nil {
		return "", err
	}
	return exp, k.ApplyToken(tok, exp)
}

// ParseKey reads a PEM RSA private key, PKCS#1 (what GitHub hands out) or PKCS#8.
func ParseKey(pemText string) (*rsa.PrivateKey, error) {
	b, _ := pem.Decode([]byte(pemText))
	if b == nil {
		return nil, errors.New("app private key: no PEM block")
	}
	if k, err := x509.ParsePKCS1PrivateKey(b.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return nil, fmt.Errorf("app private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("app private key: not RSA")
	}
	return rk, nil
}

// AppJWT is the RS256 JWT GitHub wants for App-level calls: issued 60s in the past (clock drift),
// valid 9 minutes (max 10).
func AppJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	seg := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := seg(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." +
		seg(map[string]any{"iat": now.Unix() - 60, "exp": now.Unix() + 540, "iss": appID})
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// GitHub is the REST API (URL: https://api.github.com; a test server in tests).
type GitHub struct {
	URL  string
	HTTP *http.Client
}

// Mint returns an installation token scoped to Actions read/write (dispatch + read runs) on every repo
// the installation covers, and its expiry (RFC 3339).
func (g *GitHub) Mint(appID, installationID string, key *rsa.PrivateKey, now time.Time) (string, string, error) {
	jwt, err := AppJWT(appID, key, now)
	if err != nil {
		return "", "", err
	}
	var r struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	err = call(g.HTTP, http.MethodPost, g.URL+"/app/installations/"+installationID+"/access_tokens",
		map[string]any{"permissions": map[string]string{"actions": "write"}},
		map[string]string{"Authorization": "Bearer " + jwt, "Accept": "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28", "Content-Type": "application/json", "User-Agent": "gitops-cd-token-minter"},
		&r)
	if err != nil {
		return "", "", err
	}
	if r.Token == "" {
		return "", "", errors.New("GitHub returned no token")
	}
	return r.Token, r.ExpiresAt, nil
}

// Kube is the Kubernetes API with a service account token.
type Kube struct {
	URL       string
	HTTP      *http.Client
	TokenFile string // re-read on every call: the kubelet rotates it
}

func inCluster() (*Kube, error) {
	ca, err := os.ReadFile(saDir + "/ca.crt")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("service account ca.crt: no certificates")
	}
	return &Kube{
		URL:       "https://kubernetes.default.svc",
		HTTP:      &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}},
		TokenFile: saDir + "/token",
	}, nil
}

func (k *Kube) headers(contentType string) (map[string]string, error) {
	t, err := os.ReadFile(k.TokenFile)
	if err != nil {
		return nil, err
	}
	return map[string]string{"Authorization": "Bearer " + strings.TrimSpace(string(t)),
		"Content-Type": contentType, "Accept": "application/json"}, nil
}

// GetSecret returns a secret's data, decoded.
func (k *Kube) GetSecret(name string) (map[string]string, error) {
	h, err := k.headers("application/json")
	if err != nil {
		return nil, err
	}
	var s struct {
		Data map[string][]byte `json:"data"` // base64 in JSON: decoded by encoding/json
	}
	if err := call(k.HTTP, http.MethodGet, k.URL+"/api/v1/namespaces/"+namespace+"/secrets/"+name, nil, h, &s); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key, v := range s.Data {
		out[key] = string(v)
	}
	return out, nil
}

// ApplyToken creates or replaces the target secret in one server-side apply.
func (k *Kube) ApplyToken(token, expiresAt string) error {
	h, err := k.headers("application/apply-patch+yaml") // JSON is YAML
	if err != nil {
		return err
	}
	body := map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
		"metadata": map[string]any{
			"name": dstSecret, "namespace": namespace,
			"labels":      map[string]string{"kargo.akuity.io/cred-type": "generic"},
			"annotations": map[string]string{"example.com/expires-at": expiresAt},
		},
		"stringData": map[string]string{"token": token},
	}
	return call(k.HTTP, http.MethodPatch,
		k.URL+"/api/v1/namespaces/"+namespace+"/secrets/"+dstSecret+"?fieldManager=github-actions-token&force=true",
		body, h, nil)
}

// call sends body as JSON (nil: none) and decodes a 2xx response into out (nil: discard). Errors carry
// the status and the start of the response body, never the request.
func call(c *http.Client, method, url string, body any, headers map[string]string, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %d %s", method, req.URL.Path, resp.StatusCode, bytes.TrimSpace(b[:min(len(b), 300)]))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}
