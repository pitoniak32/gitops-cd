package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UniqueApps fails if two capabilities render the same workload Application name in one region
// (<cap>-<app>-<target> can collide; one duplicate breaks the region's whole workloads AppSet) or the same
// Kargo project (kargo-<cap>-<app>).
func UniqueApps(out string) error {
	regions, _ := filepath.Glob(filepath.Join(out, "argocd", "deploys", "*"))
	sort.Strings(regions)
	var msgs []string
	for _, r := range regions {
		if st, err := os.Stat(r); err != nil || !st.IsDir() {
			continue
		}
		seen := map[string]int{}
		err := filepath.WalkDir(r, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			var a struct {
				AppName string `json:"appName"`
			}
			if err := json.Unmarshal(b, &a); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			seen[a.AppName]++
			return nil
		})
		if err != nil {
			return err
		}
		var names []string
		for n, c := range seen {
			if c > 1 {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			msgs = append(msgs, fmt.Sprintf("duplicate workload Application name %s in region %s: rename a capability or app", n, filepath.Base(r)))
		}
	}
	pm, err := duplicateProjects(out)
	if err != nil {
		return err
	}
	msgs = append(msgs, pm...)
	if len(msgs) > 0 {
		return fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
	return nil
}

// duplicateProjects reports Kargo projects (kargo-<cap>-<app>, ADR 0029) rendered by more than one
// capability file; _-prefixed files are platform-level.
func duplicateProjects(out string) ([]string, error) {
	files, _ := filepath.Glob(filepath.Join(out, "kargo", "*.yaml"))
	sort.Strings(files)
	where := map[string][]string{}
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f), "_") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		for {
			var d struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name string `yaml:"name"`
				} `yaml:"metadata"`
			}
			if err := dec.Decode(&d); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			if d.Kind == "Project" {
				where[d.Metadata.Name] = append(where[d.Metadata.Name], filepath.ToSlash(filepath.Join("kargo", filepath.Base(f))))
			}
		}
	}
	var names []string
	for n, fs := range where {
		if len(fs) > 1 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var msgs []string
	for _, n := range names {
		msgs = append(msgs, fmt.Sprintf("duplicate Kargo project %s in %s: rename a capability or app", n, strings.Join(where[n], ", ")))
	}
	return msgs, nil
}

// CodeOwners assembles <root>/.github/CODEOWNERS from CODEOWNERS.tmpl: "#GENERATED" is replaced by the
// capability fragments in <out>/codeowners/ (sorted) plus one line per teams/<team>.yml (the team's
// githubTeam plus the owners of the template's "/teams/" line). Lines after #GENERATED win.
func CodeOwners(root, out string) error {
	tmplPath := filepath.Join(root, ".github", "CODEOWNERS.tmpl")
	tb, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}
	tmpl := strings.Split(strings.TrimSuffix(string(tb), "\n"), "\n")
	n := 0
	teamsOwners := ""
	haveTeams := false
	for _, l := range tmpl {
		if l == "#GENERATED" {
			n++
		}
		f := strings.Fields(l)
		if !haveTeams && len(f) > 0 && f[0] == "/teams/" {
			teamsOwners = strings.Join(f[1:], " ")
			haveTeams = true
		}
	}
	if n != 1 {
		return fmt.Errorf("%s needs exactly one #GENERATED line", tmplPath)
	}
	var gen strings.Builder
	frags, _ := filepath.Glob(filepath.Join(out, "codeowners", "*"))
	sort.Strings(frags)
	for _, f := range frags {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		gen.Write(b)
	}
	teams, _ := filepath.Glob(filepath.Join(root, "teams", "*.y*ml"))
	sort.Strings(teams)
	for _, f := range teams {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var t struct {
			GithubTeam string `yaml:"githubTeam"`
		}
		if err := yaml.Unmarshal(b, &t); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		line := fmt.Sprintf("/teams/%s @%s", filepath.Base(f), t.GithubTeam)
		if teamsOwners != "" {
			line += " " + teamsOwners
		}
		gen.WriteString(line + "\n")
	}
	var res strings.Builder
	for _, l := range tmpl {
		if l == "#GENERATED" {
			res.WriteString(gen.String())
			continue
		}
		res.WriteString(l + "\n")
	}
	return os.WriteFile(filepath.Join(root, ".github", "CODEOWNERS"), []byte(res.String()), 0o644)
}
