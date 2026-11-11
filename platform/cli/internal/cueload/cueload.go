// Package cueload builds CUE values from the render package using the CUE Go library (ADR 0026).
package cueload

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/yaml"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
)

// RenderDir is <root>/platform/render, or, for fixture roots that hold only YAML inputs
// (testdata/root), the repo's own renderer package.
func RenderDir(root string) string {
	d := filepath.Join(root, "platform", "render")
	if st, err := os.Stat(filepath.Join(d, "cue.mod")); err == nil && st.IsDir() {
		return d
	}
	if st, err := os.Stat(filepath.Join(root, "platform", "cue.mod")); err == nil && st.IsDir() {
		return d
	}
	if r, err := repo.Root(); err == nil {
		return filepath.Join(r, "platform", "render")
	}
	return d
}

func build(ctx *cue.Context, root string, files []string, tags []string) (cue.Value, error) {
	insts := load.Instances(files, &load.Config{Dir: RenderDir(root), Tags: tags})
	if len(insts) != 1 {
		return cue.Value{}, fmt.Errorf("cueload: expected 1 instance, got %d", len(insts))
	}
	if err := insts[0].Err; err != nil {
		return cue.Value{}, err
	}
	return ctx.BuildInstance(insts[0]), nil
}

// Platform loads the given files (e.g. clusters.cue, regions.cue) in platform/render.
func Platform(root string, files ...string) (cue.Value, error) {
	return build(cuecontext.New(), root, files, nil)
}

// Generic loads the package with tag generic=true (editor schemas).
func Generic(root string) (cue.Value, error) {
	return build(cuecontext.New(), root, []string{"."}, []string{"generic=true"})
}

// Inputs lists a capability's input YAML paths, relative to root, in routing order.
func Inputs(root, capName string) []string {
	c := filepath.Join("capabilities", capName)
	globs := []string{
		"teams/*.y*ml",
		c + "/owners.y*ml",
		c + "/stages/*.y*ml",
		c + "/targets/*.y*ml",
		c + "/targets/*/target.y*ml",
		c + "/targets/*/environment-variables/*.y*ml",
		c + "/apps/*/app.y*ml",
		c + "/apps/*/owners.y*ml",
	}
	var out []string
	for _, g := range globs {
		m, _ := filepath.Glob(filepath.Join(root, g))
		for _, p := range m {
			rel, err := filepath.Rel(root, p)
			if err == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	return out
}

// Capability loads the render package with one capability's YAML inputs filled under `in`.
func Capability(root, capName string) (cue.Value, error) {
	tags := []string{"capability=" + capName}
	if r := os.Getenv("GITOPS_REPO"); r != "" {
		tags = append(tags, "repo="+r)
	}
	ctx := cuecontext.New()
	v, err := build(ctx, root, []string{"."}, tags)
	if err != nil {
		return v, err
	}
	for _, rel := range Inputs(root, capName) {
		abs := filepath.Join(root, rel)
		b, err := os.ReadFile(abs)
		if err != nil {
			return v, err
		}
		f, err := yaml.Extract(abs, b)
		if err != nil {
			return v, err
		}
		dv := ctx.BuildFile(f)
		if err := dv.Err(); err != nil {
			return v, err
		}
		v = v.FillPath(cue.MakePath(cue.Str("in"), cue.Str(rel)), dv)
	}
	return v, nil
}

// Lookup is cue.ParsePath, exported for callers.
func Lookup(p string) cue.Path { return cue.ParsePath(p) }
