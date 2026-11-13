package system

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every schema field is used somewhere in the fixture, so `gcd schema` can point at a real example.
func TestFixtureCoversSchema(t *testing.T) {
	t.Parallel()
	root, _ := filepath.Abs(testdata + "/root")
	ex, err := Examples(root)
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, d := range Schemas(Default()) {
		for _, p := range FieldPaths(d) {
			if _, ok := ex[d.Kind+"/"+p]; !ok {
				missing = append(missing, d.Kind+"/"+p)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("fixture %s lacks:\n  %s", FixtureRoot, strings.Join(missing, "\n  "))
	}
}

func TestExamplesMissingDir(t *testing.T) {
	t.Parallel()
	ex, err := Examples(t.TempDir())
	if err != nil || len(ex) != 0 {
		t.Errorf("got %v, %v", ex, err)
	}
}
