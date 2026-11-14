package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var fixed = time.Date(2026, 11, 14, 17, 0, 0, 0, time.UTC)

func serve(t *testing.T, method, path, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var out bytes.Buffer
	h := newHandler(&out, func() time.Time { return fixed })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w, out.String()
}

const valid = `{"promotion":"us-east4-dev-k8s.01abc.74a416b","project":"kargo-payments-worker","system":"payments",
"app":"worker","stage":"dev","target":"us-east4-dev-k8s","platform":"k8s","deployWith":"gha",
"freight":"74a416bbfa","freightAlias":"torrid-dog","warehouse":"worker",
"artifact":"ghcr.io/pitoniak32/nginx:1.1.0@sha256:c8fa","commit":"0a455a6","actor":"email:admin@example.com",
"automatic":false,"rollback":false,"result":"Succeeded","ghaRunURL":"https://github.com/x/runs/1",
"uiURL":"https://kargo.localhost/project/kargo-payments-worker/stage/us-east4-dev-k8s","extra":"ignored"}`

func TestValidRecordLoggedAsOneLine(t *testing.T) {
	w, out := serve(t, http.MethodPost, "/promotions", valid)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("want one line, got %q", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"msg": "promotion", "receivedAt": "2026-11-14T17:00:00Z",
		"promotion": "us-east4-dev-k8s.01abc.74a416b", "project": "kargo-payments-worker", "system": "payments",
		"app": "worker", "stage": "dev", "target": "us-east4-dev-k8s", "platform": "k8s", "deployWith": "gha",
		"freight": "74a416bbfa", "freightAlias": "torrid-dog", "warehouse": "worker",
		"artifact": "ghcr.io/pitoniak32/nginx:1.1.0@sha256:c8fa", "commit": "0a455a6", "actor": "email:admin@example.com",
		"automatic": false, "rollback": false, "result": "Succeeded", "ghaRunURL": "https://github.com/x/runs/1",
		"uiURL": "https://kargo.localhost/project/kargo-payments-worker/stage/us-east4-dev-k8s",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if _, ok := got["extra"]; ok {
		t.Error("unknown field logged")
	}
	if len(got) != len(want) {
		t.Errorf("fields %d, want %d: %v", len(got), len(want), got)
	}
}

func TestInvalidRecordRejected(t *testing.T) {
	cases := map[string]string{
		"not json":      `{`,
		"no target":     strings.Replace(valid, `"target":"us-east4-dev-k8s",`, ``, 1),
		"no promotion":  strings.Replace(valid, `"promotion":"us-east4-dev-k8s.01abc.74a416b",`, ``, 1),
		"no project":    strings.Replace(valid, `"project":"kargo-payments-worker",`, ``, 1),
		"bad result":    strings.Replace(valid, `"result":"Succeeded"`, `"result":"Maybe"`, 1),
		"trailing data": valid + `{}`,
		"wrong type":    strings.Replace(valid, `"automatic":false`, `"automatic":"no"`, 1),
		"too big":       `{"promotion":"` + strings.Repeat("x", maxBody) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w, out := serve(t, http.MethodPost, "/promotions", body)
			if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("status %d, want 400/413", w.Code)
			}
			if out != "" {
				t.Errorf("logged %q", out)
			}
		})
	}
}

func TestFailedResultAccepted(t *testing.T) {
	w, out := serve(t, http.MethodPost, "/promotions", strings.Replace(valid, `"result":"Succeeded"`, `"result":"Failed"`, 1))
	if w.Code != http.StatusNoContent || !strings.Contains(out, `"result":"Failed"`) {
		t.Fatalf("status %d, out %q", w.Code, out)
	}
}

func TestRoutes(t *testing.T) {
	if w, _ := serve(t, http.MethodGet, "/promotions", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /promotions: %d", w.Code)
	}
	if w, _ := serve(t, http.MethodGet, "/healthz", ""); w.Code != http.StatusOK {
		t.Errorf("GET /healthz: %d", w.Code)
	}
	if w, _ := serve(t, http.MethodPost, "/other", valid); w.Code != http.StatusNotFound {
		t.Errorf("POST /other: %d", w.Code)
	}
}
