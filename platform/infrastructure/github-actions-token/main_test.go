package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestParseKey(t *testing.T) {
	k := testKey(t)
	p8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]*pem.Block{
		"pkcs1": {Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)},
		"pkcs8": {Type: "PRIVATE KEY", Bytes: p8},
	} {
		got, err := ParseKey(string(pem.EncodeToMemory(p)))
		if err != nil || !got.Equal(k) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseKey("not a key"); err == nil {
		t.Error("want error")
	}
}

// verifyJWT checks the signature and returns the claims.
func verifyJWT(t *testing.T, jwt string, pub *rsa.PublicKey) map[string]any {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts = %d", len(parts))
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature: %v", err)
	}
	var claims map[string]any
	b, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(b, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

// One refresh against fake Kubernetes + GitHub APIs: reads the App key, mints with an Actions-scoped
// request signed by that key, applies the token secret.
func TestRefreshOnce(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1_800_000_000, 0)
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/77/access_tokens" {
			t.Errorf("github: %s %s", r.Method, r.URL.Path)
		}
		claims := verifyJWT(t, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &key.PublicKey)
		if claims["iss"] != "12" || claims["iat"] != float64(now.Unix()-60) || claims["exp"] != float64(now.Unix()+540) {
			t.Errorf("claims = %v", claims)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"permissions":{"actions":"write"}}` {
			t.Errorf("mint body = %s", b)
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"token":"ghs_sekret","expires_at":"2027-01-15T09:00:00Z"}`)
	}))
	defer gh.Close()

	var applied map[string]any
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sa-token" {
			t.Errorf("kube auth = %q", r.Header.Get("Authorization"))
		}
		base := "/api/v1/namespaces/kargo-shared-resources/secrets/"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base+"github-gitops-cd":
			enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{
				"githubAppID": enc("12"), "githubAppInstallationID": enc("77"), "githubAppPrivateKey": enc(keyPEM)}})
		case r.Method == http.MethodPatch && r.URL.Path == base+"github-actions":
			if r.Header.Get("Content-Type") != "application/apply-patch+yaml" ||
				r.URL.Query().Get("fieldManager") != "github-actions-token" || r.URL.Query().Get("force") != "true" {
				t.Errorf("apply: %s %s", r.Header.Get("Content-Type"), r.URL.RawQuery)
			}
			json.NewDecoder(r.Body).Decode(&applied)
			io.WriteString(w, `{}`)
		default:
			t.Errorf("kube: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer kube.Close()

	tokFile := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tokFile, []byte("sa-token\n"), 0o600)
	k := &Kube{URL: kube.URL, HTTP: kube.Client(), TokenFile: tokFile}
	exp, err := refreshOnce(k, &GitHub{URL: gh.URL, HTTP: gh.Client()}, now)
	if err != nil {
		t.Fatal(err)
	}
	if exp != "2027-01-15T09:00:00Z" {
		t.Errorf("expires = %s", exp)
	}
	b, _ := json.Marshal(applied)
	want := `{"apiVersion":"v1","kind":"Secret","metadata":{"annotations":{"example.com/expires-at":"2027-01-15T09:00:00Z"},` +
		`"labels":{"kargo.akuity.io/cred-type":"generic"},"name":"github-actions","namespace":"kargo-shared-resources"},` +
		`"stringData":{"token":"ghs_sekret"},"type":"Opaque"}`
	if string(b) != want {
		t.Errorf("applied:\n%s\nwant:\n%s", b, want)
	}
}

// GitHub's refusal (e.g. the App lacks Actions permission) comes back with status and message.
func TestMintError(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"message":"The permissions requested are not granted to this installation."}`)
	}))
	defer gh.Close()
	_, _, err := (&GitHub{URL: gh.URL, HTTP: gh.Client()}).Mint("1", "2", testKey(t), time.Now())
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "not granted") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "Bearer") {
		t.Error("error leaks the JWT")
	}
}
