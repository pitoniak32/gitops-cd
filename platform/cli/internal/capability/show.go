package capability

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ShowFilter narrows Show; empty = everything.
type ShowFilter struct{ App, Stage, Target string }

// Value is one effective setting and where it comes from.
type Value struct {
	Path   string
	Value  any
	Source Source
}

func (v Value) MarshalJSON() ([]byte, error) {
	o := struct {
		Path  string `json:"path"`
		Value any    `json:"value"`
		File  string `json:"file,omitempty"`
		Line  int    `json:"line,omitempty"`
		Note  string `json:"note,omitempty"`
	}{Path: v.Path, Value: v.Value}
	if !v.Source.none() {
		o.File, o.Line, o.Note = v.Source.File, v.Source.Line, v.Source.Note
	}
	return json.Marshal(o)
}

// none: the source says nothing (a key that is not in any file).
func (s Source) none() bool { return s.Line == 0 && s.Note == "" }

// ShowDeploy is the merged config of one app on one target.
type ShowDeploy struct {
	App      string  `json:"app"`
	Stage    string  `json:"stage"`
	Target   string  `json:"target"`
	Platform string  `json:"platform"`
	Values   []Value `json:"values"`
	artifact string  // "image" | "module": the key its artifact is shown under
}

// ShowResult is the effective config of a capability.
type ShowResult struct {
	Deploys []ShowDeploy       `json:"deploys"`
	Owners  map[string][]Value `json:"owners"` // app -> owners values
}

// Show loads and validates a capability and returns its effective config per deploy, each value with
// the file:line that set it. Validation problems come back as an Error.
func Show(cfg Config, root, capName string, teams *Teams, f ShowFilter) (ShowResult, error) {
	m, diags, err := loadModel(cfg, root, capName, teams)
	if err != nil {
		return ShowResult{}, err
	}
	if len(diags) > 0 {
		return ShowResult{}, Error(diags)
	}
	var apps, stages, targets []string
	for _, a := range m.apps {
		apps = append(apps, a.name)
	}
	for _, s := range m.stages {
		stages = append(stages, s.name)
	}
	for _, t := range m.targets {
		targets = append(targets, t.name)
	}
	for _, c := range []struct {
		what, x string
		all     []string
	}{{"app", f.App, apps}, {"stage", f.Stage, stages}, {"target", f.Target, targets}} {
		if c.x != "" && !has(c.all, c.x) {
			return ShowResult{}, fmt.Errorf("unknown %s %s; allowed: %s", c.what, c.x, strings.Join(sorted(c.all), ", "))
		}
	}
	res := ShowResult{Owners: map[string][]Value{}}
	for _, a := range m.apps {
		if f.App != "" && a.name != f.App {
			continue
		}
		var ds []*deploy
		for _, d := range m.deploys {
			if d.app == a && (f.Stage == "" || d.stage == f.Stage) && (f.Target == "" || d.target == f.Target) {
				ds = append(ds, d)
			}
		}
		rank := map[string]int{}
		if p := m.pipelines[a.name]; p != nil {
			for i, s := range p.stages {
				rank[s.name] = i
			}
		}
		sort.SliceStable(ds, func(i, j int) bool {
			if ri, rj := rank[ds[i].stage], rank[ds[j].stage]; ri != rj {
				return ri < rj
			}
			return ds[i].target < ds[j].target
		})
		for _, d := range ds {
			res.Deploys = append(res.Deploys, ShowDeploy{App: a.name, Stage: d.stage, Target: d.target, Platform: d.tgt.platform.name, Values: deployValues(d), artifact: d.artifact})
		}
		if len(ds) > 0 {
			res.Owners[a.name] = m.ownerValues(a, f.Stage)
		}
	}
	if len(res.Deploys) == 0 && (f.Stage != "" || f.Target != "") {
		var flt []string
		if f.Stage != "" {
			flt = append(flt, "--stage "+f.Stage)
		}
		if f.Target != "" {
			flt = append(flt, "--target "+f.Target)
		}
		who, scope := "", "capability"
		if f.App != "" {
			who, scope = " of "+f.App, f.App
		}
		var have []string
		for _, d := range m.deploys {
			if f.App == "" || d.app.name == f.App {
				have = append(have, d.stage+"/"+d.target)
			}
		}
		return ShowResult{}, fmt.Errorf("no deploys%s match %s; %s deploys to: %s", who, strings.Join(flt, " "), scope, strings.Join(sorted(have), ", "))
	}
	return res, nil
}

func deployValues(d *deploy) []Value {
	var out []Value
	add := func(path string, v any, ok bool) {
		if ok {
			out = append(out, Value{Path: path, Value: v, Source: d.src[path]})
		}
	}
	art := d.app.image
	if d.artifact == "module" {
		art = d.app.module
	}
	add("artifact", art, true)
	// target config, from the target.yml keys that are set
	if t := d.tgt; t.file != nil {
		for _, k := range [][]string{{"environment"}, {"environmentType"}, {"platform"}, {"cluster"}, {"namespace"}, {"region"}, {"gcpProject"},
			{"billing"}, {"auth", "wifEmail"}, {"backend", "bucket"}, {"backend", "prefix"}} {
			key, val := getKV(t.file.doc, append([]string{"config"}, k...)...)
			if key == nil || val == nil {
				if k[0] == "platform" {
					out = append(out, Value{Path: "config.platform", Value: t.platform.name, Source: Source{Note: "global default"}})
				}
				continue
			}
			out = append(out, Value{Path: "config." + strings.Join(k, "."), Value: valueOf(val), Source: keySource(t.file.path, key)})
		}
	}
	if d.port != nil {
		add("port", *d.port, true)
	}
	if d.artifact == "image" {
		add("replicas", d.replicas, true)
		add("resources.cpu", d.cpu, d.cpu != "")
		add("resources.memory", d.memory, d.memory != "")
	}
	add("deployWith", d.deployWith, d.deployWith != "")
	add("autoPromote", d.autoPromote, true)
	if d.deployWith == "gha" {
		add("workflow.repo", d.wfRepo, d.wfRepo != "")
		add("workflow.file", d.wfFile, d.wfFile != "")
		add("workflow.ref", d.wfRef, d.wfRef != "")
	}
	for _, n := range d.envFrom {
		add("envFrom."+n, true, true)
	}
	for _, e := range d.env {
		add("env."+e.k, e.v, true)
	}
	return out
}

func (m *model) ownerValues(a *appDef, stage string) []Value {
	o, an := &m.owners, a.name
	var out []Value
	add := func(path string, v []string) {
		if len(v) > 0 {
			out = append(out, Value{Path: path, Value: v, Source: o.src[an][path]})
		}
	}
	add("codeOwners", o.codeOwners[an])
	for _, s := range a.stages {
		if stage == "" || s.name == stage {
			add("promoters."+s.name, o.promoters[an][s.name])
		}
	}
	add("viewers", o.viewers[an])
	add("breakGlass", o.breakGlass)
	return out
}
