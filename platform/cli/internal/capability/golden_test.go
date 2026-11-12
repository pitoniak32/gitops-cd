package capability

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const testdata = "../../../render/testdata"

// The fixture capabilities and the platform-level files render to exactly the committed golden tree.
func TestGolden(t *testing.T) {
	t.Parallel()
	root, _ := filepath.Abs(testdata + "/root")
	golden, _ := filepath.Abs(testdata + "/golden")
	got := map[string]string{}
	for _, c := range []string{"payments", "search"} {
		files, err := Render(Default(), root, c)
		if err != nil {
			t.Fatalf("%s:\n%v", c, err)
		}
		for p, s := range files {
			got[p] = s
		}
	}
	want := map[string]string{}
	filepath.Walk(golden, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(golden, p)
			b, _ := os.ReadFile(p)
			want[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	for p, s := range PlatformFiles(Default()) {
		got[p] = s
	}
	var paths []string
	for p := range want {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		g, ok := got[p]
		switch {
		case !ok:
			t.Errorf("missing: %s", p)
		case g != want[p]:
			gl, wl := strings.Split(g, "\n"), strings.Split(want[p], "\n")
			for i := range wl {
				if i >= len(gl) || gl[i] != wl[i] {
					gotLine := "<eof>"
					if i < len(gl) {
						gotLine = gl[i]
					}
					t.Errorf("differs: %s line %d:\n  want %q\n  got  %q", p, i+1, wl[i], gotLine)
					break
				}
			}
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("extra: %s", p)
		}
	}
}
