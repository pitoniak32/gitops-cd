package schemas

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
)

func j(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func check(t *testing.T, f func(any) any, in, want string) {
	t.Helper()
	got, w := f(j(t, in)), j(t, want)
	if !reflect.DeepEqual(got, w) {
		gb, _ := json.Marshal(got)
		t.Fatalf("got %s\nwant %s", gb, want)
	}
}

func TestFileName(t *testing.T) {
	if got := FileName("EnvironmentVariables"); got != "environment-variables.json" {
		t.Fatal(got)
	}
	if got := FileName("App"); got != "app.json" {
		t.Fatal(got)
	}
}

func TestDropFalseAnyOf(t *testing.T) {
	check(t, dropFalseAnyOf, `{"a":{"anyOf":[false,{"type":"string"},false]}}`, `{"a":{"anyOf":[{"type":"string"}]}}`)
}

func TestInlineRefProps(t *testing.T) {
	check(t, inlineRefProps,
		`{"$defs":{"X":{"properties":{"a":1,"b":1}}},"properties":{"b":2,"c":2},"$ref":"#/$defs/X"}`,
		`{"$defs":{"X":{"properties":{"a":1,"b":1}}},"properties":{"a":1,"b":2,"c":2}}`)
	// no properties: ref untouched
	check(t, inlineRefProps, `{"$defs":{},"x":{"$ref":"#/$defs/X"}}`, `{"$defs":{},"x":{"$ref":"#/$defs/X"}}`)
}

func TestPropertyNamesMessages(t *testing.T) {
	check(t, propertyNamesMessages,
		`{"additionalProperties":false,"properties":{"b":{},"a":{}}}`,
		`{"properties":{"b":{},"a":{}},"propertyNames":{"enum":["a","b"],"errorMessage":"Unknown key. Allowed: a, b"}}`)
	check(t, propertyNamesMessages,
		`{"additionalProperties":false,"patternProperties":{"^x$":{},"^a$":{}}}`,
		`{"patternProperties":{"^x$":{},"^a$":{}},"propertyNames":{"pattern":"^a$ or ^x$","errorMessage":"Invalid name, must match ^a$ or ^x$"}}`)
}

func TestFoldNotPattern(t *testing.T) {
	check(t, foldNotPattern, `{"not":{"pattern":"bad"}}`, `{"pattern":"^(?![\\s\\S]*?(?:bad))"}`)
	check(t, foldNotPattern, `{"pattern":"ok","not":{"pattern":"bad"}}`, `{"pattern":"^(?![\\s\\S]*?(?:bad))[\\s\\S]*?(?:ok)"}`)
	check(t, foldNotPattern, `{"not":{"pattern":"bad","x":1}}`, `{"not":{"pattern":"bad","x":1}}`)
}

func TestPatternMessages(t *testing.T) {
	check(t, patternMessages, `{"pattern":"^a(/[a-z0-9._-]+)+$"}`,
		`{"pattern":"^a(/[a-z0-9._-]+)+$","errorMessage":"Image repository only, no tag or digest (Kargo discovers SemVer tags)"}`)
	check(t, patternMessages, `{"pattern":"^[a-z0-9._%+-]+@example\\.com$"}`,
		`{"pattern":"^[a-z0-9._%+-]+@example\\.com$","errorMessage":"Lowercase email in the allowed domain: example.com"}`)
	check(t, patternMessages, `{"anyOf":[],"description":"Plain value, or a Secret ref"}`,
		`{"anyOf":[],"description":"Plain value, or a Secret ref","errorMessage":"Plain string, or secret:<gcp-project>/<secret>[@<version>]"}`)
	check(t, patternMessages, `{"pattern":"x"}`, `{"pattern":"x"}`)
}

func TestPatternErrorMessage(t *testing.T) {
	check(t, patternErrorMessage, `{"errorMessage":"m","pattern":"p"}`, `{"errorMessage":"m","pattern":"p","patternErrorMessage":"m"}`)
	check(t, patternErrorMessage, `{"errorMessage":"m","anyOf":[{"type":"string"}]}`,
		`{"errorMessage":"m","patternErrorMessage":"m","anyOf":[{"type":"string","patternErrorMessage":"m"}]}`)
	check(t, patternErrorMessage, `{"errorMessage":"m"}`, `{"errorMessage":"m"}`)
}

func TestWalkIsBottomUp(t *testing.T) {
	// inner anyOf cleaned before the outer sees it
	check(t, dropFalseAnyOf, `{"anyOf":[{"anyOf":[false]}]}`, `{"anyOf":[{"anyOf":[]}]}`)
}

func TestGenerateMatchesCommitted(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Skip(err)
	}
	out := t.TempDir()
	var w bytes.Buffer
	if err := Generate(root, out, &w); err != nil {
		t.Fatal(err)
	}
	for _, def := range Defs {
		name := FileName(def)
		got, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(root, "platform", "render", "schemas", name))
		if err != nil {
			t.Fatal(err)
		}
		var g, e any
		if err := json.Unmarshal(got, &g); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want, &e); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g, e) {
			t.Errorf("%s differs from committed", name)
		}
	}
}
