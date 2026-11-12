package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestColorAlwaysGolden(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	got, _, err := runShow(t, root, "payments/my-app", "--stage", "dev", "-s", "--color", "always")
	if err != nil {
		t.Fatal(err)
	}
	p := schemaGolden + "show-my-app-dev-sources-color.txt"
	if os.Getenv("UPDATE") == "1" {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs (UPDATE=1 to accept)", p)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Error("no color")
	}
}

func TestColorOffAndJSON(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	plain, _, _ := runShow(t, root, "payments/my-app", "-s")
	t.Setenv("NO_COLOR", "1")
	for _, mode := range []string{"auto", "never"} {
		got, _, err := runShow(t, root, "payments/my-app", "-s", "--color", mode)
		if err != nil || got != plain || strings.Contains(got, "\x1b") {
			t.Errorf("--color %s not plain (err %v)", mode, err)
		}
	}
	js, _, err := runShow(t, root, "payments/my-app", "--json", "--color", "always")
	if err != nil || strings.Contains(js, "\x1b") {
		t.Errorf("json colored (err %v)", err)
	}
}

func TestColorInvalid(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	if _, _, err := runShow(t, root, "payments", "--color", "bogus"); err == nil || !strings.Contains(err.Error(), "invalid --color") {
		t.Errorf("want invalid --color error, got %v", err)
	}
}

func TestSchemaColor(t *testing.T) {
	out, err := runSchema(t, "app", "--color", "always")
	if err != nil || !strings.Contains(out, "\x1b[2m# ") || !strings.Contains(out, "\x1b[33m") {
		t.Errorf("schema not colored (err %v):\n%s", err, out)
	}
}
