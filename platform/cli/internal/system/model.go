package system

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type kv struct {
	k, v string
	src  Source
}

// layer is the settings that can be set at app, stage or target level (and the platform's and the
// global defaults below them). A zero field is "not set here".
type layer struct {
	replicas, port          *int
	env                     []kv
	envFrom                 []string
	cpu, memory, deployWith string
	wfRepo, wfFile, wfRef   string
	node                    *yaml.Node
	file, note              string // the file it is read from, or what it is when no file (defaults)
}

func readLayer(n *yaml.Node, path string) layer {
	l := layer{node: n, file: path, envFrom: strList(get(n, "envFrom")),
		cpu: str(get(n, "resources", "cpu")), memory: str(get(n, "resources", "memory")), deployWith: str(get(n, "deployWith")),
		wfRepo: str(get(n, "workflow", "repo")), wfFile: str(get(n, "workflow", "file")), wfRef: str(get(n, "workflow", "ref"))}
	for _, f := range []struct {
		name string
		to   **int
	}{{"replicas", &l.replicas}, {"port", &l.port}} {
		if v := get(n, f.name); isInt(v) {
			i, _ := strconv.Atoi(v.Value)
			*f.to = &i
		}
	}
	for _, p := range pairs(get(n, "env")) {
		l.env = append(l.env, kv{p.k, valueOf(p.v), keySource(path, p.key)})
	}
	return l
}

type stageDef struct {
	name        string
	file        *file
	dependsOn   []string
	autoPromote bool
	color       string
	depth       int // longest path from a root
}

type targetDef struct {
	name                                   string
	file                                   *file
	platform                               *platform
	environment, environmentType           string
	wifEmail, billing                      string
	cluster, namespace, region, gcpProject string
	bucket, prefix                         string
}

type appTarget struct {
	name string
	key  *yaml.Node
	layer
}

type appStage struct {
	name string
	layer
	autoPromote *bool // app-level override of the stage's autoPromote
	targets     []appTarget
}

type appDef struct {
	name string
	file *file
	layer
	image, imageConstraint, module, moduleConstraint, sourceRepo string
	stages                                                       []appStage
}

func (a *appDef) stage(n string) *appStage {
	for i := range a.stages {
		if a.stages[i].name == n {
			return &a.stages[i]
		}
	}
	return nil
}

// what the Freight's image subscription tracks: the image or module repository
func (a *appDef) artifactRepo() string {
	if a.module != "" {
		return a.module
	}
	return a.image
}

// deploy is one (app, target): the effective, fully merged settings.
type deploy struct {
	app      *appDef
	stage    string
	target   string
	tgt      *targetDef
	artifact string // image | module

	port                    *int
	replicas                int
	cpu, memory, deployWith string
	wfRepo, wfFile, wfRef   string
	envFrom                 []string
	env                     []kv
	autoPromote             bool              // effective for this app's stage: app override, else stage, else false
	src                     map[string]Source // leaf path -> where its value comes from
	prefix                  string            // terraform state prefix
}

func (d *deploy) key() string { return d.app.name + "/" + d.target }

// model is a validated system and everything derived from it.
type model struct {
	cfg     Config
	sys     string
	in      *inputs
	stages  []*stageDef
	targets []*targetDef
	apps    []*appDef
	teams   map[string]string // name -> githubTeam
	// target -> bundle -> KEY: value, in file order
	targetEnvs map[string]map[string][]kv
	deploys    []*deploy
	pipelines  map[string]*pipeline
	graphs     map[string][]*graphNode
	owners     owners
}

func (m *model) stage(n string) *stageDef {
	for _, s := range m.stages {
		if s.name == n {
			return s
		}
	}
	return nil
}

func (m *model) target(n string) *targetDef {
	for _, t := range m.targets {
		if t.name == n {
			return t
		}
	}
	return nil
}

func (m *model) app(n string) *appDef {
	for _, a := range m.apps {
		if a.name == n {
			return a
		}
	}
	return nil
}

func (m *model) deploy(app, target string) *deploy {
	for _, d := range m.deploys {
		if d.app.name == app && d.target == target {
			return d
		}
	}
	return nil
}

func uniqSorted(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range lists {
		for _, x := range l {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sorted(l []string) []string {
	out := append([]string{}, l...)
	sort.Strings(out)
	return out
}

// build derives the model from inputs that passed validate. The diagnostics are what only shows in
// derived values: merged settings, the stage graph, generated names.
func build(cfg Config, in *inputs) (*model, []Diag) {
	var diags []Diag
	rep := func(f *file) reporter { return reporter{f, &diags} }
	m := &model{cfg: cfg, sys: in.sys, in: in, teams: map[string]string{}, targetEnvs: map[string]map[string][]kv{},
		pipelines: map[string]*pipeline{}, graphs: map[string][]*graphNode{}}

	for _, f := range in.of("team") {
		m.teams[f.name()] = str(get(f.doc, "githubTeam"))
	}
	for _, f := range in.of("stage") {
		if m.stage(f.name()) != nil {
			continue
		}
		s := &stageDef{name: f.name(), file: f, dependsOn: strList(get(f.doc, "dependsOn")), color: str(get(f.doc, "color"))}
		if n := get(f.doc, "autoPromote"); isBool(n) {
			s.autoPromote = n.Value == "true"
		}
		m.stages = append(m.stages, s)
	}
	// default color: spread sorted stage names across the palette
	for i, n := range sorted(in.names("stage")) {
		if s := m.stage(n); s.color == "" {
			s.color = defaultColors[i%len(defaultColors)]
		}
	}
	m.stageDepths(rep)

	for _, f := range in.of("target") {
		if m.target(f.name()) != nil {
			continue
		}
		c := get(f.doc, "config")
		g := func(p ...string) string { return str(get(c, p...)) }
		pn := "K8S"
		if g("platform") != "" {
			pn = g("platform")
		}
		m.targets = append(m.targets, &targetDef{name: f.name(), file: f, platform: platformByName(pn),
			environment: g("environment"), environmentType: g("environmentType"), wifEmail: g("auth", "wifEmail"), billing: g("billing"),
			cluster: g("cluster"), namespace: g("namespace"), region: g("region"), gcpProject: g("gcpProject"),
			bucket: g("backend", "bucket"), prefix: g("backend", "prefix")})
	}
	for _, f := range in.of("targetEnv") {
		t, b := f.names[0], f.names[1]
		if m.targetEnvs[t] == nil {
			m.targetEnvs[t] = map[string][]kv{}
		}
		for _, p := range pairs(f.doc) {
			m.targetEnvs[t][b] = append(m.targetEnvs[t][b], kv{p.k, valueOf(p.v), keySource(f.path, p.key)})
		}
		if m.targetEnvs[t][b] == nil {
			m.targetEnvs[t][b] = []kv{}
		}
	}
	for _, f := range in.of("app") {
		if m.app(f.name()) != nil {
			continue
		}
		g := func(k string) string { return str(get(f.doc, k)) }
		a := &appDef{name: f.name(), file: f, layer: readLayer(f.doc, f.path), image: g("image"), imageConstraint: g("imageConstraint"),
			module: g("module"), moduleConstraint: g("moduleConstraint"), sourceRepo: g("sourceRepo")}
		for _, s := range pairs(get(f.doc, "stages")) {
			as := appStage{name: s.k, layer: readLayer(s.v, f.path)}
			if n := get(s.v, "autoPromote"); isBool(n) {
				b := n.Value == "true"
				as.autoPromote = &b
			}
			for _, t := range pairs(get(s.v, "targets")) {
				as.targets = append(as.targets, appTarget{name: t.k, key: t.key, layer: readLayer(t.v, f.path)})
			}
			a.stages = append(a.stages, as)
		}
		m.apps = append(m.apps, a)
	}
	m.readOwners()

	for _, a := range m.apps {
		r := rep(a.file)
		placed := map[string]string{} // cluster/namespace -> target
		for i := range a.stages {
			s := &a.stages[i]
			for j := range s.targets {
				t := &s.targets[j]
				if prev := m.deploy(a.name, t.name); prev != nil {
					r.at(t.key, []string{"targets", t.name, "stage"}, fmt.Sprintf("target %s is already used by stage %s: an app deploys a target from one stage", t.name, prev.stage))
					continue
				}
				if d := m.merge(r, a, s, t, placed); d != nil {
					m.deploys = append(m.deploys, d)
				}
			}
		}
	}
	if len(diags) > 0 {
		return m, diags
	}

	// autoPromote: app.yml stages.<s> override, else stages/<s>.yml, else manual
	for _, d := range m.deploys {
		as, st := d.app.stage(d.stage), m.stage(d.stage)
		switch {
		case as.autoPromote != nil:
			d.autoPromote = *as.autoPromote
			k, _ := getKV(as.node, "autoPromote")
			d.src["autoPromote"] = keySource(d.app.file.path, k)
		case st.file != nil && isBool(get(st.file.doc, "autoPromote")):
			d.autoPromote = st.autoPromote
			k, _ := getKV(st.file.doc, "autoPromote")
			d.src["autoPromote"] = keySource(st.file.path, k)
		default:
			d.src["autoPromote"] = Source{Note: "default: manual"}
		}
	}
	// Prod is only ever promoted by a person: no autoPromote on a stage that deploys to a prod target,
	// in the stage file or an app's override.
	prod := map[string]bool{}
	for _, d := range m.deploys {
		if d.tgt.environmentType == "prd" {
			prod[d.stage] = true
		}
	}
	notAllowed := func(s string) string { return "stage " + s + " deploys to prod targets; autoPromote not allowed" }
	for _, s := range m.stages {
		if prod[s.name] && s.autoPromote {
			k, _ := getKV(s.file.doc, "autoPromote")
			rep(s.file).at(k, []string{"autoPromote"}, notAllowed(s.name))
		}
	}
	for _, a := range m.apps {
		for _, s := range a.stages {
			if prod[s.name] && s.autoPromote != nil && *s.autoPromote {
				k, _ := getKV(s.node, "autoPromote")
				rep(a.file).at(k, []string{"stages", s.name, "autoPromote"}, notAllowed(s.name))
			}
		}
	}

	for _, a := range m.apps {
		m.pipelines[a.name] = m.pipeline(a)
		m.graphs[a.name] = m.graph(a)
		m.nameLengths(rep(a.file), a)
	}
	return m, diags
}

// stageDepths sets each stage's longest path from a root; a dependency cycle is reported on every
// stage in it.
func (m *model) stageDepths(rep func(*file) reporter) {
	const visiting = -1
	depth := map[string]int{}
	cyclic := map[string]bool{}
	var walk func(s *stageDef, trail []string) int
	walk = func(s *stageDef, trail []string) int {
		if d, ok := depth[s.name]; ok {
			if d == visiting {
				for i := len(trail) - 1; i >= 0; i-- {
					cyclic[trail[i]] = true
					if trail[i] == s.name {
						break
					}
				}
				return 0
			}
			return d
		}
		depth[s.name] = visiting
		d := 0
		for _, dep := range s.dependsOn {
			if ds := m.stage(dep); ds != nil {
				if x := walk(ds, append(trail, s.name)) + 1; x > d {
					d = x
				}
			}
		}
		depth[s.name] = d
		return d
	}
	for _, s := range m.stages {
		s.depth = walk(s, nil)
	}
	for _, s := range m.stages {
		if cyclic[s.name] {
			k, _ := getKV(s.file.doc, "dependsOn")
			rep(s.file).at(k, []string{"dependsOn"}, "stage "+s.name+" is in a dependency cycle")
		}
	}
}

// merge builds the deploy of one target of an app: each setting is the first layer's that has it,
// target > stage > app > platform default > default; env is merged per key.
func (m *model) merge(r reporter, a *appDef, s *appStage, t *appTarget, placed map[string]string) *deploy {
	tn := t.name
	at := func(field, msg string) { r.at(t.key, []string{"targets", tn, field}, msg) }
	tgt := m.target(tn)
	P := tgt.platform
	d := &deploy{app: a, stage: s.name, target: tn, tgt: tgt, artifact: "image", src: map[string]Source{}}
	artKey := "image"
	if a.module != "" {
		d.artifact, artKey = "module", "module"
	}
	d.src["artifact"] = a.layer.src(artKey)
	if P.artifact != d.artifact {
		at("platform", fmt.Sprintf("target %s is %s; %s apps need %s targets", tn, P.name, artifacts[d.artifact].app, strings.Join(platformNames(d.artifact), " or ")))
		return nil
	}
	pd := P.defaults // note derived from the platform name, not per entry
	pd.note = "platform default (" + P.name + ")"
	L := []*layer{&t.layer, &s.layer, &a.layer, &pd, &defaults}
	// first is the value of the first layer that has one; sets src[key] from that layer's path
	first := func(key string, path []string, f func(*layer) string) string {
		for _, l := range L {
			if v := f(l); v != "" {
				d.src[key] = l.src(path...)
				return v
			}
		}
		return ""
	}

	if d.artifact == "image" {
		// app level only. K8S: Service on :80 when set. CLOUD_RUN: the container port.
		for _, l := range []*layer{&a.layer, &pd} {
			if l.port != nil {
				d.port = l.port
				d.src["port"] = l.src("port")
				break
			}
		}
		// CLOUD_RUN: replicas is minScale
		for _, l := range L {
			if l.replicas != nil {
				d.replicas = *l.replicas
				d.src["replicas"] = l.src("replicas")
				if min := m.cfg.MinReplicas[tgt.environmentType]; P.minReplicas && d.replicas < min {
					where := t.key
					if k, _ := getKV(l.node, "replicas"); k != nil {
						where = k
					}
					r.at(where, []string{"targets", tn, "replicas"}, fmt.Sprintf("%s requires replicas >= %d", tgt.environmentType, min))
				}
				break
			}
		}
		d.cpu = first("resources.cpu", []string{"resources", "cpu"}, func(l *layer) string { return l.cpu })
		d.memory = first("resources.memory", []string{"resources", "memory"}, func(l *layer) string { return l.memory })
		if P.name == "K8S" {
			// two targets of one app must not share cluster+namespace
			at := tgt.cluster + "/" + tgt.namespace
			if other, ok := placed[at]; ok {
				r.at(t.key, []string{"targets", tn}, fmt.Sprintf("targets %s and %s both deploy to %s: one target per cluster and namespace", other, tn, at))
			}
			placed[at] = tn
		}
	}
	if d.artifact == "module" {
		d.prefix = tgt.prefix
		if d.prefix == "" {
			d.prefix = m.sys + "/" + a.name + "/" + tn
		}
	}

	// forced by the artifact or the platform (a layer's deployWith is then ignored), else layered
	switch {
	case artifacts[d.artifact].deployWith != "":
		d.deployWith = artifacts[d.artifact].deployWith
		d.src["deployWith"] = Source{Note: "forced by artifact"}
	case P.deployWith != "":
		d.deployWith = P.deployWith
		d.src["deployWith"] = Source{Note: "forced by platform"}
	default:
		d.deployWith = first("deployWith", []string{"deployWith"}, func(l *layer) string { return l.deployWith })
	}
	// GitHub Actions workflow for gha deploys; each field defaults separately
	wfG := layer{wfRepo: m.cfg.ghRepo(), wfRef: "main", note: "global default"}
	wfP := layer{wfFile: P.workflowFile, note: pd.note}
	L = []*layer{&t.layer, &s.layer, &a.layer, &wfP, &wfG}
	d.wfRepo = first("workflow.repo", []string{"workflow", "repo"}, func(l *layer) string { return l.wfRepo })
	d.wfFile = first("workflow.file", []string{"workflow", "file"}, func(l *layer) string { return l.wfFile })
	d.wfRef = first("workflow.ref", []string{"workflow", "ref"}, func(l *layer) string { return l.wfRef })

	// envFrom: targets/<t>/environment-variables/<name>.yml, unioned over the layers
	d.envFrom = uniqSorted(a.envFrom, s.envFrom, t.envFrom)
	for _, n := range d.envFrom {
		for _, l := range []*layer{&t.layer, &s.layer, &a.layer} { // the most specific layer listing it
			if has(l.envFrom, n) {
				d.src["envFrom."+n] = l.src("envFrom")
				break
			}
		}
		if m.targetEnvs[tn][n] == nil {
			at("envFrom", fmt.Sprintf("target %s has no environment-variables/%s.yml", tn, n))
		}
	}
	// Per layer: a KEY from the files listed there. Equal values are fine, different values at one
	// layer are ambiguous. A KEY from a more specific layer's envFrom wins.
	layers := []struct {
		name string
		l    *layer
	}{{"app", &a.layer}, {"stage", &s.layer}, {"target", &t.layer}}
	var layerEnv [3][]kv
	for i, ly := range layers {
		var keys []string
		values := map[string][]string{} // KEY -> distinct values
		files := map[string][]string{}  // KEY -> files setting it
		srcs := map[string]Source{}     // KEY -> its first line
		for _, n := range ly.l.envFrom {
			for _, e := range m.targetEnvs[tn][n] {
				if _, ok := values[e.k]; !ok {
					keys = append(keys, e.k)
					srcs[e.k] = e.src
				}
				if !has(values[e.k], e.v) {
					values[e.k] = append(values[e.k], e.v)
				}
				if !has(files[e.k], n) {
					files[e.k] = append(files[e.k], n)
				}
			}
		}
		for _, k := range keys {
			if len(values[k]) > 1 {
				at("envFrom", fmt.Sprintf("target %s: %s differs between environment-variables/{%s}.yml (both in %s-level envFrom)", tn, k, strings.Join(sorted(files[k]), ","), ly.name))
				continue
			}
			layerEnv[i] = append(layerEnv[i], kv{k, values[k][0], srcs[k]})
		}
	}
	// later lists win; a key keeps the place of the list its value comes from
	override := func(lists ...[]kv) []kv {
		var out []kv
		for i, l := range lists {
			for _, e := range l {
				shadowed := false
				for _, later := range lists[i+1:] {
					for _, x := range later {
						shadowed = shadowed || x.k == e.k
					}
				}
				if !shadowed {
					out = append(out, e)
				}
			}
		}
		return out
	}
	// any env wins over every envFrom
	d.env = override(override(layerEnv[0], layerEnv[1], layerEnv[2]), a.env, s.env, t.env)
	for _, e := range d.env {
		d.src["env."+e.k] = e.src
	}
	return d
}

// owners is owners.yml after the per-app merge. Emails are matched on the OIDC email claim.
type owners struct {
	// per app, sorted: the app's teams if set, else the system's
	codeOwners map[string][]string
	// per app, per stage the app uses: the app's promoters if set, else the system's
	promoters map[string]map[string][]string
	// system-wide
	breakGlass []string
	// stages/ + targets/ approvers: union of the apps' codeOwners (the system's without apps)
	allOwners []string
	// per app: everyone who can see it in Kargo / Argo CD: its promoters, viewers and break-glass
	appEmails map[string][]string
	// everyone who can list the system's projects in Kargo
	allEmails []string
	// per app: its effective viewers (the app's if set, else the system's)
	viewers map[string][]string
	// per app: codeOwners / viewers / breakGlass / promoters.<stage> -> the key line of the list used
	src map[string]map[string]Source
}

func (m *model) readOwners() {
	o := &m.owners
	o.codeOwners, o.promoters, o.appEmails = map[string][]string{}, map[string]map[string][]string{}, map[string][]string{}
	o.viewers, o.src = map[string][]string{}, map[string]map[string]Source{}
	var doc *yaml.Node
	var path string
	if f := m.in.of("owners"); len(f) > 0 {
		doc, path = f[0].doc, f[0].path
	}
	list := func(p ...string) []string { return strList(get(doc, p...)) }
	// where: key line of the list at path in owners.yml
	where := func(p ...string) Source {
		k, _ := getKV(doc, p...)
		return keySource(path, k)
	}
	o.breakGlass = uniqSorted(list("breakGlass"))
	var all, allTeams []string
	for _, p := range pairs(get(doc, "promoters")) {
		all = append(all, strList(p.v)...)
	}
	for _, a := range pairs(get(doc, "apps")) {
		for _, p := range pairs(get(a.v, "promoters")) {
			all = append(all, strList(p.v)...)
		}
		all = append(all, strList(get(a.v, "viewers"))...)
	}
	o.allEmails = uniqSorted(all, list("viewers"), o.breakGlass)
	for _, a := range m.apps {
		an := a.name
		src := map[string]Source{"breakGlass": where("breakGlass")}
		o.src[an] = src
		teams, tp := list("codeOwners"), []string{"codeOwners"}
		if get(doc, "apps", an, "codeOwners") != nil {
			teams, tp = list("apps", an, "codeOwners"), []string{"apps", an, "codeOwners"}
		}
		src["codeOwners"] = where(tp...)
		o.codeOwners[an] = uniqSorted(teams)
		allTeams = append(allTeams, teams...)
		o.promoters[an] = map[string][]string{}
		var mine []string
		for _, s := range a.stages {
			pp := []string{"apps", an, "promoters", s.name}
			p := get(doc, pp...)
			if p == nil {
				pp = []string{"promoters", s.name}
				p = get(doc, pp...)
			}
			if p != nil {
				src["promoters."+s.name] = where(pp...)
				o.promoters[an][s.name] = uniqSorted(strList(p))
				mine = append(mine, strList(p)...)
			}
		}
		viewers, vp := list("viewers"), []string{"viewers"}
		if get(doc, "apps", an, "viewers") != nil {
			viewers, vp = list("apps", an, "viewers"), []string{"apps", an, "viewers"}
		}
		src["viewers"] = where(vp...)
		o.viewers[an] = uniqSorted(viewers)
		o.appEmails[an] = uniqSorted(mine, viewers, o.breakGlass)
	}
	if len(m.apps) == 0 {
		allTeams = list("codeOwners")
	}
	o.allOwners = uniqSorted(allTeams)
}
