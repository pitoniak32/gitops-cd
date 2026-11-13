package lint

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"gopkg.in/yaml.v3"
)

// Changed is the Scope for changed files (repo-relative, slash-separated, old and new paths of renames):
//   - systems/<sys>/**: that system, unless it was deleted.
//   - teams/<t>.y*ml: the teams/ files, and every system whose owners.yml names <t> in codeOwners
//     (a removed or renamed team breaks them).
//   - platform/cli/**: the rules themselves changed, so everything (all is true).
//
// Anything else needs no lint.
func Changed(root string, files []string) (s Scope, all bool) {
	systems := map[string]bool{}
	teams := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		switch {
		case parts[0] == "platform" && len(parts) > 2 && parts[1] == "cli":
			return Scope{Teams: true, Systems: system.Names(root)}, true
		case parts[0] == "systems" && len(parts) > 2:
			systems[parts[1]] = true
		case parts[0] == "teams" && len(parts) == 2:
			if ext := filepath.Ext(f); ext == ".yml" || ext == ".yaml" {
				teams[strings.TrimSuffix(parts[1], ext)] = true
			}
		}
	}
	if len(teams) > 0 {
		s.Teams = true
		for _, c := range system.Names(root) {
			if !systems[c] && namesTeam(root, c, teams) {
				systems[c] = true
			}
		}
	}
	for c := range systems {
		if st, err := os.Stat(filepath.Join(root, "systems", c)); err == nil && st.IsDir() {
			s.Systems = append(s.Systems, c)
		}
	}
	sort.Strings(s.Systems)
	return s, false
}

// namesTeam: the system's owners.yml lists one of teams in codeOwners (top level or under apps).
// An unreadable owners.yml counts as naming it, so lint reports what is wrong with it.
func namesTeam(root, sysName string, teams map[string]bool) bool {
	for _, ext := range []string{".yml", ".yaml"} {
		b, err := os.ReadFile(filepath.Join(root, "systems", sysName, "owners"+ext))
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
