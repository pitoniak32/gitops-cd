package localimage

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Each Deployment must run the image built from its current sources: edit a main.go and this fails until
// the manifest carries the new tag (UPDATE=1 writes it).
func TestManifestTags(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	if len(Images) != 2 {
		t.Fatalf("images: %v", Images)
	}
	for _, img := range Images {
		t.Run(img.Repo, func(t *testing.T) {
			ref, err := img.Ref(root)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, img.Dir, img.Manifest)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(`image: ` + regexp.QuoteMeta(img.Repo) + `:\S+`)
			got := re.Find(b)
			if got == nil {
				t.Fatalf("%s: no image %s", path, img.Repo)
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
		})
	}
}
