package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamlpos"
	"gopkg.in/yaml.v3"
)

type locator struct{ root, cap string }

// location: the YAML file (relative to root) a CUE path belongs to, its label, and how to find
// the position in that file.
type location struct {
	file, label string
	find        func(abs string) (line, col int, ok bool)
}

var segRe = regexp.MustCompile(`"[^"]*"|[^.]+`)

var indexRe = regexp.MustCompile(`^[0-9]+$`)

func isIndex(s string) bool { return indexRe.MatchString(s) }

// yamlfile: <path>/<stem> under root -> existing .yml/.yaml (relative to root), default .yml.
func (l locator) yamlfile(stem string) string {
	for _, ext := range []string{"yml", "yaml"} {
		if st, err := os.Stat(filepath.Join(l.root, stem+"."+ext)); err == nil && !st.IsDir() {
			return stem + "." + ext
		}
	}
	return stem + ".yml"
}

// keyOrItem: the key's position for a map key, the item's for a list index, the document's for
// an empty path.
func keyOrItem(path []string) func(string) (int, int, bool) {
	return func(abs string) (int, int, bool) { return yamlpos.Find(abs, path) }
}

func pos(n *yaml.Node) (int, int, bool) {
	if n == nil || n.Line == 0 {
		return 0, 0, false
	}
	return n.Line, n.Column, true
}

// locate maps a CUE path (apps."my-app".stages.qa) to its file, label and position finder.
// Top-level fields not listed (e.g. in, files) are not reported.
func (l locator) locate(cuePath string) (location, bool) {
	var segs []string
	for _, s := range segRe.FindAllString(cuePath, -1) {
		segs = append(segs, strings.ReplaceAll(s, `"`, ""))
	}
	seg := func(i int) string {
		if i < len(segs) {
			return segs[i]
		}
		return ""
	}
	c := "capabilities/" + l.cap
	var loc location
	n := 0
	switch seg(0) {
	case "apps":
		loc.file, n = l.yamlfile(c+"/apps/"+seg(1)+"/app"), 2
	case "stages":
		loc.file, n = l.yamlfile(c+"/stages/"+seg(1)), 2
	case "targets":
		loc.file, n = l.yamlfile(c+"/targets/"+seg(1)+"/target"), 2
	case "legacyTargets":
		loc.file, n = l.yamlfile(c+"/targets/"+seg(1)), len(segs)
	case "targetEnvs": // no file for the target-level error: point at its first bundle
		if len(segs) > 2 {
			loc.file, n = l.yamlfile(c+"/targets/"+seg(1)+"/environment-variables/"+seg(2)), 3
		} else {
			m, _ := filepath.Glob(filepath.Join(l.root, c, "targets", seg(1), "environment-variables", "*.y*ml"))
			if len(m) > 0 {
				loc.file, _ = filepath.Rel(l.root, m[0])
				loc.file = filepath.ToSlash(loc.file)
			}
			n = 2
		}
	case "capOwners":
		loc.file, n = l.yamlfile(c+"/owners"), 2 // skip list index (0 or 1 file)
	case "legacyAppOwners":
		loc.file, n = l.yamlfile(c+"/apps/"+seg(1)+"/owners"), len(segs)
	case "teams":
		loc.file, n = l.yamlfile("teams/"+seg(1)), 2
	case "projectNameErrors": // <app>: the app's app.yml
		loc.file, n = l.yamlfile(c+"/apps/"+seg(1)+"/app"), len(segs)
	case "stageNameErrors": // <app>.<stage>[.<target>]: the stage (control flow) or target entry in app.yml
		loc.file = l.yamlfile(c + "/apps/" + seg(1) + "/app")
		p := []string{"stages", seg(2)}
		loc.label = "stages." + seg(2)
		if seg(3) != "" {
			p = append(p, "targets", seg(3))
			loc.label += ".targets." + seg(3)
		}
		loc.find = keyOrItem(p)
		return loc, true
	case "stageDepth": // the dependsOn value
		loc.file, loc.label = l.yamlfile(c+"/stages/"+seg(1)), "dependsOn"
		loc.find = func(abs string) (int, int, bool) {
			doc, err := yamlpos.Load(abs)
			if err != nil {
				return 0, 0, false
			}
			_, v := yamlpos.Lookup(doc, []string{"dependsOn"})
			return pos(v)
		}
		return loc, true
	case "deploys": // "app/target": the target entry under the first stage that lists it
		app, t, _ := strings.Cut(seg(1), "/")
		if !strings.Contains(seg(1), "/") {
			t = seg(1)
		}
		loc.file, loc.label = l.yamlfile(c+"/apps/"+app+"/app"), "targets."+t
		rest := []string{}
		if seg(2) == "envFrom" {
			loc.label += ".envFrom"
		} else if len(segs) > 2 {
			rest = segs[2:]
			loc.label += "." + strings.Join(rest, ".")
		}
		loc.find = func(abs string) (int, int, bool) {
			doc, err := yamlpos.Load(abs)
			if err != nil {
				return 0, 0, false
			}
			_, stages := yamlpos.Lookup(doc, []string{"stages"})
			if stages == nil || stages.Kind != yaml.MappingNode {
				return 0, 0, false
			}
			for i := 1; i < len(stages.Content); i += 2 {
				_, targets := yamlpos.Lookup(stages.Content[i], []string{"targets"})
				if k, _ := yamlpos.Lookup(targets, []string{t}); k == nil {
					continue
				}
				k, v := yamlpos.Lookup(targets, append([]string{t}, rest...))
				if len(rest) > 0 && isIndex(rest[len(rest)-1]) {
					return pos(v)
				}
				return pos(k)
			}
			return 0, 0, false
		}
		return loc, true
	default:
		return loc, false
	}
	rest := segs[n:]
	loc.label = strings.Join(rest, ".")
	loc.find = keyOrItem(rest)
	return loc, true
}
