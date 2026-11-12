package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChanged(t *testing.T) {
	t.Parallel()
	r := t.TempDir()
	for rel, body := range map[string]string{
		"capabilities/a/owners.yml":     "codeOwners: [t1]\n",
		"capabilities/b/owners.yaml":    "codeOwners: [t2]\napps: {x: {codeOwners: [gone]}}\n",
		"capabilities/c/owners.yml":     "codeOwners: [t2]\n",
		"capabilities/d/owners.yml":     "codeOwners: [\n", // unreadable: linted when any team changes
		"capabilities/e/stages/dev.yml": "dependsOn: []\n",
	} {
		p := filepath.Join(r, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	all := []string{"a", "b", "c", "d", "e"}
	for _, c := range []struct {
		name    string
		files   []string
		want    Scope
		wantAll bool
	}{
		{"nothing relevant", []string{"README.md", "docs/adr/0002-x.md", "platform/appsets/kargo.yaml"}, Scope{}, false},
		{"capability files", []string{"capabilities/e/stages/dev.yml", "capabilities/a/owners.yml"}, Scope{Caps: []string{"a", "e"}}, false},
		{"deleted capability", []string{"capabilities/zz/owners.yml"}, Scope{}, false},
		{"team: its capabilities, incl. app overrides and unreadable owners", []string{"teams/gone.yml"}, Scope{Teams: true, Caps: []string{"b", "d"}}, false},
		{"team rename: both names", []string{"teams/t1.yml", "teams/t1x.yaml"}, Scope{Teams: true, Caps: []string{"a", "d"}}, false},
		{"non-yaml under teams", []string{"teams/README.md"}, Scope{}, false},
		{"renderer change: everything", []string{"capabilities/a/owners.yml", "platform/cli/internal/capability/model.go"}, Scope{Teams: true, Caps: all}, true},
	} {
		got, gotAll := Changed(r, c.files)
		if !reflect.DeepEqual(got, c.want) || gotAll != c.wantAll {
			t.Errorf("%s: got %+v all=%v, want %+v all=%v", c.name, got, gotAll, c.want, c.wantAll)
		}
	}
}
