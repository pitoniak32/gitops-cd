// Package cuefmt only holds a test: every renderer .cue file must be canonically formatted
// (what `cue fmt --check ./platform/render/...` did in CI).
package cuefmt

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
)

func TestCueFormatted(t *testing.T) {
	root, err := filepath.Abs("../../../render")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "cue.mod" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".cue") {
			return nil
		}
		n++
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		got, err := format.Source(b)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
		} else if !bytes.Equal(got, b) {
			t.Errorf("%s is not cue-fmt formatted (run `cue fmt ./platform/render/...`)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no .cue files found under platform/render")
	}
}
