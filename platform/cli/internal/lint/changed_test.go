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
		"systems/a/owners.yml":     "codeOwners: [t1]\n",
		"systems/b/owners.yaml":    "codeOwners: [t2]\napps: {x: {codeOwners: [gone]}}\n",
		"systems/c/owners.yml":     "codeOwners: [t2]\n",
		"systems/d/owners.yml":     "codeOwners: [\n", // unreadable: linted when any team changes
		"systems/e/stages/dev.yml": "dependsOn: []\n",
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
		{"system files", []string{"systems/e/stages/dev.yml", "systems/a/owners.yml"}, Scope{Systems: []string{"a", "e"}}, false},
		{"deleted system", []string{"systems/zz/owners.yml"}, Scope{}, false},
		{"team: its systems, incl. app overrides and unreadable owners", []string{"teams/gone.yml"}, Scope{Teams: true, Systems: []string{"b", "d"}}, false},
		{"team rename: both names", []string{"teams/t1.yml", "teams/t1x.yaml"}, Scope{Teams: true, Systems: []string{"a", "d"}}, false},
		{"non-yaml under teams", []string{"teams/README.md"}, Scope{}, false},
		{"renderer change: everything", []string{"systems/a/owners.yml", "platform/cli/internal/system/model.go"}, Scope{Teams: true, Systems: all}, true},
	} {
		got, gotAll := Changed(r, c.files)
		if !reflect.DeepEqual(got, c.want) || gotAll != c.wantAll {
			t.Errorf("%s: got %+v all=%v, want %+v all=%v", c.name, got, gotAll, c.want, c.wantAll)
		}
	}
}
