package tokenminter

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The Deployment must run the image built from the current sources: edit main.go and this fails until
// minter.yaml carries the new tag (UPDATE=1 writes it).
func TestManifestTag(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	ref, err := Ref(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, Dir, "manifests", "minter.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`image: ` + regexp.QuoteMeta(Repo) + `:\S+`)
	got := re.Find(b)
	if got == nil {
		t.Fatalf("%s: no image %s", path, Repo)
	}
	if want := "image: " + ref; string(got) != want {
		if os.Getenv("UPDATE") != "" {
			if err := os.WriteFile(path, re.ReplaceAll(b, []byte(want)), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Errorf("%s: %s, want %s (sources changed; UPDATE=1 to accept)", path, got, want)
	}
}
