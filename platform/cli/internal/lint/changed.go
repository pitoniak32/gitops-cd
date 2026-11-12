package lint

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"gopkg.in/yaml.v3"
)

// Changed is the Scope for changed files (repo-relative, slash-separated, old and new paths of renames):
//   - capabilities/<cap>/**: that capability, unless it was deleted.
//   - teams/<t>.y*ml: the teams/ files, and every capability whose owners.yml names <t> in codeOwners
//     (a removed or renamed team breaks them).
//   - platform/cli/**: the rules themselves changed, so everything (all is true).
//
// Anything else needs no lint.
func Changed(root string, files []string) (s Scope, all bool) {
	caps := map[string]bool{}
	teams := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		switch {
		case parts[0] == "platform" && len(parts) > 2 && parts[1] == "cli":
			return Scope{Teams: true, Caps: capability.Names(root)}, true
		case parts[0] == "capabilities" && len(parts) > 2:
			caps[parts[1]] = true
		case parts[0] == "teams" && len(parts) == 2:
			if ext := filepath.Ext(f); ext == ".yml" || ext == ".yaml" {
				teams[strings.TrimSuffix(parts[1], ext)] = true
			}
		}
	}
	if len(teams) > 0 {
		s.Teams = true
		for _, c := range capability.Names(root) {
			if !caps[c] && namesTeam(root, c, teams) {
				caps[c] = true
			}
		}
	}
	for c := range caps {
		if st, err := os.Stat(filepath.Join(root, "capabilities", c)); err == nil && st.IsDir() {
			s.Caps = append(s.Caps, c)
		}
	}
	sort.Strings(s.Caps)
	return s, false
}

// namesTeam: the capability's owners.yml lists one of teams in codeOwners (top level or under apps).
// An unreadable owners.yml counts as naming it, so lint reports what is wrong with it.
func namesTeam(root, capName string, teams map[string]bool) bool {
	for _, ext := range []string{".yml", ".yaml"} {
		b, err := os.ReadFile(filepath.Join(root, "capabilities", capName, "owners"+ext))
		if err != nil {
			continue
		}
		var o struct {
			CodeOwners []string `yaml:"codeOwners"`
			Apps       map[string]struct {
				CodeOwners []string `yaml:"codeOwners"`
			} `yaml:"apps"`
		}
		if yaml.Unmarshal(b, &o) != nil {
			return true
		}
		lists := [][]string{o.CodeOwners}
		for _, a := range o.Apps {
			lists = append(lists, a.CodeOwners)
		}
		for _, l := range lists {
			for _, t := range l {
				if teams[t] {
					return true
				}
			}
		}
	}
	return false
}
