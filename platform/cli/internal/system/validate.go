package system

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// validate checks every system input file on its own and against the others (team files: Teams.Lint). It reads the YAML as written
// (no defaults, nothing derived), so each rule runs whatever else is wrong.
func validate(cfg Config, in *inputs) []Diag {
	var diags []Diag
	rep := func(f *file) reporter { return reporter{f, &diags} }
	v := &validator{cfg: cfg, in: in,
		stages: in.names("stage"), targets: in.names("target"), apps: in.names("app"), teams: in.names("team")}

	for _, f := range in.of("stage") {
		v.stage(rep(f), f)
	}
	for _, f := range in.of("legacyTarget") {
		rep(f).at(&yaml.Node{Line: 1, Column: 1}, nil, "move to targets/"+f.name()+"/target.yml")
	}
	for _, f := range in.of("target") {
		v.target(rep(f), f)
	}
	noTarget := map[string]bool{}
	for _, f := range in.of("targetEnv") {
		r := rep(f)
		// bundles without a target: reported once, on the target's first bundle
		if in.find("target", f.names[0]) == nil {
			if !noTarget[f.names[0]] {
				noTarget[f.names[0]] = true
				r.at(nil, nil, "environment-variables without targets/"+f.names[0]+"/target.yml")
			}
			continue
		}
		if !nameRx.MatchString(f.names[1]) {
			r.at(nil, nil, badName)
		}
		r.check(envVarsS, nil, f.doc, nil)
	}
	for _, f := range in.of("app") {
		v.app(rep(f), f)
	}
	for _, f := range in.of("legacyAppOwners") {
		rep(f).at(&yaml.Node{Line: 1, Column: 1}, nil, "move into systems/"+in.sys+"/owners.yml apps."+f.name())
	}
	if o := in.of("owners"); len(o) > 0 {
		v.owners(rep(o[0]), o[0])
	} else {
		diags = append(diags, Diag{File: "systems/" + in.sys + "/owners.yml", Line: 1, Col: 1, Msg: "system needs owners.yml"})
	}
	return diags
}

type validator struct {
	cfg                          Config
	in                           *inputs
	stages, targets, apps, teams []string
}

func allowed(what string, names []string) string {
	return "unknown " + what + "; allowed: " + strings.Join(names, ", ")
}

// stages/<name>.yml
func (v *validator) stage(r reporter, f *file) {
	if !nameRx.MatchString(f.name()) {
		r.at(nil, nil, badName)
	}
	if f.name() == "owners" {
		r.at(nil, nil, `stage name "owners" is reserved (it is a key of gcd show output next to the stages)`)
	}
	r.check(stageSchema(v.stages), nil, f.doc, nil)
}

// targets/<name>/target.yml: where a stage deploys. Reusable across apps.
func (v *validator) target(r reporter, f *file) {
	tn := f.name()
	if !nameRx.MatchString(tn) {
		r.at(nil, nil, badName)
	}
	// Kargo Stage names (one project per app, ADR 0029) are <target> and, for control flow (ADR 0012),
	// <stage> and <stage>-done: a target can't be named like either.
	if has(v.stages, tn) {
		r.at(nil, nil, "target name "+tn+" is also a stage name; Kargo Stage names would collide")
	}
	if strings.HasSuffix(tn, "-done") && has(v.stages, strings.TrimSuffix(tn, "-done")) {
		r.at(nil, nil, "target name "+tn+" is <stage>-done; Kargo control-flow Stage name "+tn+" would collide")
	}
	if !isMap(f.doc) {
		r.at(nil, nil, "expected a map")
		return
	}
	for _, p := range pairs(f.doc) {
		if p.k != "config" {
			r.at(p.key, []string{p.k}, "field not allowed")
		}
	}
	ck, c := getKV(f.doc, "config")
	if !isMap(c) {
		r.at(ck, []string{"config"}, "expected a map")
		return
	}
	cp := []string{"config"}
	gcp := v.cfg.GcpRegions
	cfgS := targetSchema(v.cfg).field("config").s
	// message when the platform requires the field and the file lacks it
	missing := func(f, pn string) string {
		switch f {
		case "region":
			return "region required on a " + pn + " target; allowed: " + strings.Join(gcp, ", ")
		case "gcpProject":
			return "expected a GCP project id"
		case "backend":
			return "backend required on a " + pn + " target (bucket, optional prefix)"
		}
		return "required"
	}
	pn := "K8S"
	if n := get(c, "platform"); n != nil {
		pn = str(n)
	}
	p := platformByName(pn) // nil: unknown platform, reported on the field
	for _, kv := range pairs(c) {
		path := join(cp, kv.k)
		f := cfgS.field(kv.k)
		switch {
		case f != nil && f.platforms == nil:
			r.check(f.s, kv.key, kv.v, path)
		case kv.k == "terraform":
			r.at(kv.key, path, "moved: use config.gcpProject / config.backend")
		case f == nil:
			r.at(kv.key, path, "field not allowed")
		case p == nil:
		case has(p.fields, kv.k):
			r.check(f.s, kv.key, kv.v, path)
		default:
			// a field the platform doesn't take carries just this line, whatever its value
			note := ""
			if kv.k == "region" {
				note = " (it's the cluster's)"
			}
			r.at(kv.key, path, kv.k+" not allowed on a "+pn+" target"+note)
		}
	}
	for _, f := range cfgS.fields {
		if f.required && get(c, f.name) == nil {
			m := f.missing
			if m == "" {
				m = "required"
			}
			r.at(ck, join(cp, f.name), m)
		}
	}
	if p == nil {
		return
	}
	for _, n := range p.fields {
		if get(c, n) == nil {
			r.at(ck, join(cp, n), missing(n, pn))
		}
	}
	et := str(get(c, "environmentType"))
	cl, clOK := v.cfg.cluster(str(get(c, "cluster")))
	if has(p.fields, "cluster") && clOK && has(EnvironmentTypes, et) && !has(cl.EnvironmentTypes, et) {
		k, _ := getKV(c, "environmentType")
		r.at(k, join(cp, "environmentType"), "cluster "+cl.Name+" accepts environment types: "+strings.Join(cl.EnvironmentTypes, ", "))
	}
	// name must be <region>-<env>-<platform>[-<qualifier>] (ADR 0025); skipped while the region can't
	// be resolved (unknown cluster / missing region already error)
	region := ""
	switch {
	case has(p.fields, "region") && has(gcp, str(get(c, "region"))):
		region = str(get(c, "region"))
	case has(p.fields, "cluster") && clOK:
		region = cl.Region
	}
	if region != "" && has(EnvironmentTypes, et) {
		want := region + "-" + et + "-" + p.suffix
		if !regexp.MustCompile("^" + regexp.QuoteMeta(want) + "(-[a-z0-9]+)?$").MatchString(tn) {
			r.at(nil, nil, "name must be <region>-<env>-<platform>[-<qualifier>]: expected "+want+"[-<qualifier>]")
		}
	}
}

// apps/<name>/app.yml
func (v *validator) app(r reporter, f *file) {
	if !nameRx.MatchString(f.name()) {
		r.at(nil, nil, badName)
	}
	r.check(appS, nil, f.doc, nil)
	if !isMap(f.doc) {
		return
	}
	image, module := get(f.doc, "image"), get(f.doc, "module")
	if (image != nil) == (module != nil) {
		k, _ := getKV(f.doc, "module")
		r.at(k, []string{"module"}, "set exactly one of image (K8s app) or module (terraform app)")
	}
	no := func(n *yaml.Node, path []string, field, msg string) {
		if k, _ := getKV(n, field); k != nil {
			r.at(k, join(path, field), msg)
		}
	}
	// a terraform app takes no K8s-only settings at any layer
	tfLayer := func(n *yaml.Node, path []string) {
		no(n, path, "replicas", "replicas not allowed on a terraform app")
		no(n, path, "deployWith", "deployWith not allowed on a terraform app (always gha)")
		res := get(n, "resources")
		no(res, join(path, "resources"), "cpu", "resources not allowed on a terraform app")
		no(res, join(path, "resources"), "memory", "resources not allowed on a terraform app")
	}
	if module != nil {
		tfLayer(f.doc, nil)
		no(f.doc, nil, "port", "port not allowed on a terraform app")
		no(f.doc, nil, "imageConstraint", "imageConstraint not allowed on a terraform app (use moduleConstraint)")
	}
	if image != nil {
		no(f.doc, nil, "moduleConstraint", "moduleConstraint not allowed on a K8s app (use imageConstraint)")
	}
	// stages: a subset of the system's; each with at least one of its targets
	sk, stages := getKV(f.doc, "stages")
	if len(pairs(stages)) == 0 && (stages == nil || isMap(stages)) {
		r.at(sk, []string{"stages"}, "needs at least one stage")
	}
	for _, s := range pairs(stages) {
		sp := []string{"stages", s.k}
		if !has(v.stages, s.k) {
			r.at(s.key, sp, allowed("stage", v.stages))
			continue
		}
		if module != nil {
			tfLayer(s.v, sp)
		}
		tk, targets := getKV(s.v, "targets")
		if len(pairs(targets)) == 0 && (targets == nil || isMap(targets)) && isMap(s.v) {
			r.at(tk, join(sp, "targets"), "needs at least one target")
		}
		for _, t := range pairs(targets) {
			tp := join(sp, "targets", t.k)
			if !has(v.targets, t.k) {
				r.at(t.key, tp, allowed("target", v.targets))
				continue
			}
			if module != nil {
				tfLayer(t.v, tp)
			}
			// the target's Kargo Stage is named after it
			if len(t.k) > maxStageName {
				r.at(t.key, tp, fmt.Sprintf("Kargo Stage name %s is %d characters (max %d): shorten the target name", t.k, len(t.k), maxStageName))
			}
		}
	}
}

// appStages are the stage names an app's app.yml lists.
func (v *validator) appStages(app string) []string {
	var out []string
	if f := v.in.find("app", app); f != nil {
		for _, s := range pairs(get(f.doc, "stages")) {
			out = append(out, s.k)
		}
	}
	return out
}

// systems/<sys>/owners.yml (required: codeOwners + promoters). One file per system; apps override
// codeOwners / promoters stages / viewers under `apps:` (ADR 0007).
func (v *validator) owners(r reporter, f *file) {
	if !isMap(f.doc) {
		r.at(nil, nil, "expected a map")
		return
	}
	teamS := ownersSchema(v.teams, v.stages).field("codeOwners").s
	// a list of what: at least one unless optional, no repeats
	list := func(n *yaml.Node, key *yaml.Node, path []string, s *schema, what string, optional bool) {
		r.check(s, key, n, path)
		if !isSeq(n) {
			return
		}
		if len(n.Content) == 0 && !optional {
			r.at(key, path, "needs at least one "+what)
		}
		seen := map[string]bool{}
		for _, it := range items(n) {
			if seen[valueOf(it)] {
				r.at(key, path, what+"s must not repeat")
				break
			}
			seen[valueOf(it)] = true
		}
	}
	// stage -> emails that may promote into it; app: the app an override is for
	promoters := func(key, n *yaml.Node, path []string, app string) {
		if !isMap(n) {
			r.at(key, path, "expected a map: <stage>: [email, ..]")
			return
		}
		for _, p := range pairs(n) {
			pp := join(path, p.k)
			if !has(v.stages, p.k) {
				r.at(p.key, pp, allowed("stage", v.stages))
				continue
			}
			list(p.v, p.key, pp, emails, "email", false)
			if app != "" && has(v.apps, app) && !has(v.appStages(app), p.k) {
				r.at(p.key, pp, "app "+app+" doesn't use stage "+p.k)
			}
		}
	}
	for _, p := range pairs(f.doc) {
		path := []string{p.k}
		switch p.k {
		case "codeOwners": // GitHub teams (teams/): approve the app's code changes (CODEOWNERS)
			list(p.v, p.key, path, teamS, "team", false)
		case "promoters": // promote the app into that stage (and approve its Freight); can also view
			promoters(p.key, p.v, path, "")
		case "viewers": // read-only view of the system in Kargo and Argo CD
			list(p.v, p.key, path, emails, "email", true)
		case "breakGlass": // can approve Freight that skipped upstream verification (ADR 0013)
			list(p.v, p.key, path, emails, "email", true)
		case "owners":
			r.at(p.key, path, "renamed to codeOwners")
		case "apps": // per-app overrides: each field set replaces the system's (promoters: per stage)
			if !isMap(p.v) {
				r.at(p.key, path, "expected a map")
				continue
			}
			for _, a := range pairs(p.v) {
				ap := join(path, a.k)
				if !has(v.apps, a.k) {
					r.at(a.key, ap, allowed("app", v.apps))
					continue
				}
				if !isMap(a.v) {
					r.at(a.key, ap, "expected a map")
					continue
				}
				for _, o := range pairs(a.v) {
					op := join(ap, o.k)
					switch o.k {
					case "codeOwners":
						list(o.v, o.key, op, teamS, "team", false)
					case "promoters":
						promoters(o.key, o.v, op, a.k)
					case "viewers":
						list(o.v, o.key, op, emails, "email", true)
					case "breakGlass":
						r.at(o.key, op, "breakGlass is system-wide; set it at the top level")
					default:
						r.at(o.key, op, "field not allowed")
					}
				}
			}
		default:
			r.at(p.key, path, "field not allowed")
		}
	}
	if get(f.doc, "codeOwners") == nil && get(f.doc, "owners") == nil {
		r.at(nil, []string{"codeOwners"}, "system owners.yml needs codeOwners: [team, ..]")
	}
	pk, prom := getKV(f.doc, "promoters")
	if prom == nil {
		r.at(nil, []string{"promoters"}, "system owners.yml needs promoters: {<stage>: [email, ..]}")
	}
	if isMap(prom) {
		// every stage an app uses needs a promoter, after the per-app merge
		for _, an := range v.apps {
			for _, sn := range v.appStages(an) {
				if has(v.stages, sn) && get(prom, sn) == nil && get(f.doc, "apps", an, "promoters", sn) == nil {
					r.at(pk, []string{"promoters"}, fmt.Sprintf("app %s uses stage %s: needs promoters.%s (or apps.%s.promoters.%s)", an, sn, sn, an, sn))
				}
			}
		}
	}
}
