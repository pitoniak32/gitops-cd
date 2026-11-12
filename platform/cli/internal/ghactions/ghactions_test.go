package ghactions

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetect(t *testing.T) {
	if r, err := Detect(env(nil)); r != nil || err != nil {
		t.Fatalf("outside Actions: %v %v", r, err)
	}
	r, err := Detect(env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "push"}))
	if err != nil || r == nil || r.PullRequest() {
		t.Fatalf("push: %+v %v", r, err)
	}
	p := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(p, []byte(`{"pull_request":{"labels":[{"name":"bug"},{"name":"lint-all"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err = Detect(env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request",
		"GITHUB_BASE_REF": "main", "GITHUB_EVENT_PATH": p}))
	if err != nil {
		t.Fatal(err)
	}
	if !r.PullRequest() || !r.Labeled(LintAllLabel) || r.Labeled("nope") || !reflect.DeepEqual(r.Labels, []string{"bug", "lint-all"}) {
		t.Errorf("pull_request: %+v", r)
	}
	if _, err := Detect(env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "pull_request",
		"GITHUB_EVENT_PATH": filepath.Join(t.TempDir(), "missing")})); err == nil {
		t.Error("want error for unreadable payload")
	}
	var nilRun *Run
	if nilRun.PullRequest() || nilRun.Labeled("x") {
		t.Error("nil run")
	}
}

func TestError(t *testing.T) {
	got := Error("a,b:c.yml", 3, 4, "50% bad\nline")
	want := "::error file=a%2Cb%3Ac.yml,line=3,col=4::50%25 bad%0Aline"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
