package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const schemaGolden = "../../../render/testdata/golden-cli/"

func runSchema(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root, _ := filepath.Abs("../../../..")
	c := NewRoot()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append([]string{"schema", "--root", root}, args...))
	err := c.Execute()
	return out.String(), err
}

func TestSchemaGolden(t *testing.T) {
	for name, args := range map[string][]string{"schema.yaml": nil, "schema-app-sources.yaml": {"app", "-s"}, "schema.json": {"--json"}} {
		got, err := runSchema(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		p := schemaGolden + name
		if os.Getenv("UPDATE") == "1" {
			if err := os.MkdirAll(schemaGolden, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs from gcd schema output (UPDATE=1 to accept)", p)
		}
	}
	got, err := runSchema(t, "app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "# app: capabilities/{cap}/apps/*/app.yml") {
		t.Errorf("gcd schema app starts: %.80q", got)
	}
	// every kind's skeleton is valid YAML, documents separated by ---
	all, err := runSchema(t, "-s")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	dec := yaml.NewDecoder(strings.NewReader(all))
	for {
		var v any
		if err := dec.Decode(&v); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("%v\n%s", err, all)
		}
		n++
	}
	if n != 5 {
		t.Errorf("documents: %d, want 5", n)
	}
}

func TestSchemaUnknownKind(t *testing.T) {
	_, err := runSchema(t, "nope")
	if err == nil || !strings.Contains(err.Error(), "owners, stage, app, target, env-vars") {
		t.Errorf("got %v", err)
	}
}

// Without the fixture, the schema still prints, just without examples.
func TestSchemaNoFixture(t *testing.T) {
	c := NewRoot()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetArgs([]string{"schema", "--root", t.TempDir()})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 || strings.Contains(out.String(), "e.g.") {
		t.Errorf("want schema without examples, got %.200q", out.String())
	}
}
