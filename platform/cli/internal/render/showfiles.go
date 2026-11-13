package render

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Pick is one deploy whose rendered files are wanted.
type Pick struct{ App, Stage, Target string }

// SelectFiles picks from a system's rendered files those of the deploys: workload files
// apps/<sys>/<app>/<stage>/<target>/*; with platform also the Argo CD params
// argocd/deploys/<region>/<sys>/<app>-<target>.json and the deploy's Kargo Stage doc, keyed
// "kargo/<sys>.yaml#Stage/<namespace>/<name>".
func SelectFiles(rendered map[string]string, sysName string, picks []Pick, platform bool) map[string]string {
	out := map[string]string{}
	var stageDocs map[string]string
	for _, p := range picks {
		dir := fmt.Sprintf("apps/%s/%s/%s/%s/", sysName, p.App, p.Stage, p.Target)
		suffix := fmt.Sprintf("/%s/%s-%s.json", sysName, p.App, p.Target)
		for path, c := range rendered {
			if strings.HasPrefix(path, dir) || (platform && strings.HasPrefix(path, "argocd/deploys/") && strings.HasSuffix(path, suffix)) {
				out[path] = c
			}
		}
		if platform {
			if stageDocs == nil {
				stageDocs = kargoStages("kargo/"+sysName+".yaml", rendered["kargo/"+sysName+".yaml"])
			}
			// the target Stage: named after the target, in the app's Kargo project (ADR 0029)
			key := fmt.Sprintf("kargo/%s.yaml#Stage/kargo-%s-%s/%s", sysName, sysName, p.App, p.Target)
			if d, ok := stageDocs[key]; ok {
				out[key] = d
			}
		}
	}
	return out
}

// kargoStages splits a multi-doc YAML stream and returns its Stage docs by virtual path.
func kargoStages(file, stream string) map[string]string {
	out := map[string]string{}
	var cur []string
	flush := func() {
		doc := strings.Join(cur, "\n")
		cur = nil
		var h struct {
			Kind     string `yaml:"kind"`
			Metadata struct{ Name, Namespace string }
		}
		if strings.TrimSpace(doc) == "" || yaml.Unmarshal([]byte(doc), &h) != nil || h.Kind != "Stage" {
			return
		}
		out[fmt.Sprintf("%s#Stage/%s/%s", file, h.Metadata.Namespace, h.Metadata.Name)] = strings.Trim(doc, "\n") + "\n"
	}
	for _, l := range strings.Split(stream, "\n") {
		if l == "---" {
			flush()
			continue
		}
		cur = append(cur, l)
	}
	flush()
	return out
}

func isJSON(path string) bool { return strings.HasSuffix(path, ".json") }

// Header is the line printed before a file's content.
func Header(path string) string { return "# rendered/" + path + "\n" }

// FormatFiles prints files sorted by path, each under its header; YAML files after the first are
// preceded by a --- line, JSON files print as is.
func FormatFiles(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var sb strings.Builder
	for i, p := range paths {
		if i > 0 {
			if isJSON(p) {
				sb.WriteString("\n")
			} else {
				sb.WriteString("---\n")
			}
		}
		sb.WriteString(Header(p))
		sb.WriteString(files[p])
		if !strings.HasSuffix(files[p], "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// SelectedDiffs lists, sorted by path, the files that differ between old and now (Old "" = added, New "" = removed).
func SelectedDiffs(old, now map[string]string) []FileDiff {
	var out []FileDiff
	for p, n := range now {
		if o := old[p]; o != n {
			out = append(out, FileDiff{p, o, n})
		}
	}
	for p, o := range old {
		if _, ok := now[p]; !ok {
			out = append(out, FileDiff{p, o, ""})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
