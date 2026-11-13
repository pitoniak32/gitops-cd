package capability

import (
	"fmt"
	"strings"
)

// pipeline is an app's neutral promotion graph (pipelines/<cap>.json): input for Kargo, or anything
// that can run `levels` in order and the stages within a level in parallel.
type pipeline struct {
	app    *appDef
	stages []*pstage
	levels [][]string
}

type pstage struct {
	name        string
	dependsOn   []string // app-level, sorted: a skipped stage passes its own deps through
	autoPromote bool
	level       int
	targets     []string
}

func (p *pipeline) stage(n string) *pstage {
	for _, s := range p.stages {
		if s.name == n {
			return s
		}
	}
	return nil
}

func (m *model) pipeline(a *appDef) *pipeline {
	// app-level deps of every capability stage: a stage the app skips passes its own deps through
	eff := map[string]map[string]bool{}
	var effOf func(s string) map[string]bool
	effOf = func(s string) map[string]bool {
		if e, ok := eff[s]; ok {
			return e
		}
		e := map[string]bool{}
		eff[s] = e
		for _, d := range m.stage(s).dependsOn {
			if a.stage(d) != nil {
				e[d] = true
				continue
			}
			for x := range effOf(d) {
				e[x] = true
			}
		}
		return e
	}
	// transitive upstream stages used by this app
	anc := map[string]map[string]bool{}
	var ancOf func(s string) map[string]bool
	ancOf = func(s string) map[string]bool {
		if e, ok := anc[s]; ok {
			return e
		}
		e := map[string]bool{}
		anc[s] = e
		for d := range effOf(s) {
			e[d] = true
			for x := range ancOf(d) {
				e[x] = true
			}
		}
		return e
	}
	p := &pipeline{app: a}
	maxLevel := 0
	for _, s := range a.stages {
		auto := m.stage(s.name).autoPromote
		if s.autoPromote != nil {
			auto = *s.autoPromote
		}
		ps := &pstage{name: s.name, autoPromote: auto, level: m.stage(s.name).depth, dependsOn: []string{}}
		// drop deps already implied by another dep (dev when staging also depends on dev)
		var deps []string
		for d := range effOf(s.name) {
			implied := false
			for e := range effOf(s.name) {
				implied = implied || ancOf(e)[d]
			}
			if !implied {
				deps = append(deps, d)
			}
		}
		ps.dependsOn = uniqSorted(deps)
		for _, t := range s.targets {
			ps.targets = append(ps.targets, t.name)
		}
		if ps.level > maxLevel {
			maxLevel = ps.level
		}
		p.stages = append(p.stages, ps)
	}
	for l := 0; l <= maxLevel; l++ {
		var names []string
		for _, s := range p.stages {
			if s.level == l {
				names = append(names, s.name)
			}
		}
		if len(names) > 0 {
			p.levels = append(p.levels, sorted(names))
		}
	}
	return p
}

// graphNode is one Kargo Stage of an app (ADR 0012, names ADR 0029): a target Stage <target>, or a
// control-flow Stage where it adds value. Each app has its own Kargo project, so names carry no app prefix.
type graphNode struct {
	name    string
	control bool // control-flow Stage: always auto-promotes, only means "upstream verified"
	stage   string
	target  string
	// Freight comes from the Warehouse directly, or from these upstream Stages (all: every one of
	// them must have verified it)
	direct  bool
	sources []string
	all     bool
	// capability stages the node belongs to (promote RBAC): its own, or for a shared control-flow
	// Stage every stage consuming it
	stages []string
}

const warehouse = "@warehouse"

// graph is the app's Kargo Stages: control-flow Stages first, then target Stages in app.yml order.
//
//	Group a stage's targets by the stage's (app-resolved) dependency set. U = upstream targets of that
//	set (or the Warehouse for first stages), C = all targets of the stages sharing that set.
//	- |U| == 1 or |C| == 1: direct (1->1, 1->N, M->1 with availabilityStrategy All).
//	- |U| > 1 and |C| > 1: one shared control-flow Stage waits for All of U; consumers request from it.
//	  Named <D> when one stage D consumes it (fan-in), <S>-done after one stage S (fan-out),
//	  <S1>-<S2>...-done otherwise.
//	- One click per stage: Kargo's "Promote to downstream" promotes to a node's direct downstream
//	  Stages, so every stage with >1 target must be exactly one node's downstream set. If its feeder
//	  also feeds other stages (or is the Warehouse, which can't promote to several Stages at once),
//	  the stage gets an entry control-flow Stage <D>.
func (m *model) graph(a *appDef) []*graphNode {
	p := m.pipelines[a.name]
	type group struct {
		deps, mem, up []string // dependency set, stages sharing it (sorted), upstream targets (sorted)
		consumers     int      // targets of mem
		wh, shared    bool
		name, label   string // shared control-flow Stage and the stage it is shown under
		// the single node feeding this group's targets; "" when each target joins several sources
		feeder string
	}
	key := func(s *pstage) string { return strings.Join(s.dependsOn, ",") }
	groups := map[string]*group{}
	var order []*group
	for _, s := range p.stages {
		g := groups[key(s)]
		if g == nil {
			g = &group{deps: s.dependsOn, wh: len(s.dependsOn) == 0}
			groups[key(s)] = g
			order = append(order, g)
		}
		g.mem = append(g.mem, s.name)
		g.consumers += len(s.targets)
	}
	for _, g := range order {
		g.mem = sorted(g.mem)
		for _, d := range g.deps {
			g.up = append(g.up, p.stage(d).targets...)
		}
		g.up = sorted(g.up)
		g.shared = !g.wh && len(g.up) > 1 && g.consumers > 1
		switch {
		case g.shared && len(g.mem) == 1:
			g.name, g.label = g.mem[0], g.mem[0]
		case g.shared && len(g.deps) == 1:
			g.name, g.label = g.deps[0]+"-done", g.deps[0]
		case g.shared:
			g.name, g.label = strings.Join(g.deps, "-")+"-done", g.deps[0]
		}
		switch {
		case g.shared:
			g.feeder = g.name
		case g.wh:
			g.feeder = warehouse
		case len(g.up) == 1:
			g.feeder = g.up[0]
		}
	}
	// how many Stages "Promote to downstream" reaches from each upstream target
	down := map[string]int{}
	for _, g := range order {
		if g.wh {
			continue
		}
		n := g.consumers
		if g.shared {
			n = 1
		}
		for _, u := range g.up {
			down[u] += n
		}
	}
	entry := func(s *pstage) bool {
		g := groups[key(s)]
		if len(s.targets) <= 1 || g.feeder == "" {
			return false
		}
		switch {
		case g.feeder == warehouse:
			return true
		case g.shared:
			return len(g.mem) > 1
		}
		if n, ok := down[g.feeder]; ok {
			return n != len(s.targets)
		}
		return false
	}

	var nodes []*graphNode
	add := func(n *graphNode) {
		for _, x := range nodes {
			if x.name == n.name {
				return
			}
		}
		nodes = append(nodes, n)
	}
	for _, g := range order {
		if g.shared {
			add(&graphNode{name: g.name, control: true, stage: g.label, sources: g.up, all: true, stages: g.mem})
		}
	}
	for _, s := range p.stages {
		if entry(s) {
			n := &graphNode{name: s.name, control: true, stage: s.name, stages: []string{s.name}}
			if g := groups[key(s)]; g.feeder == warehouse {
				n.direct = true
			} else {
				n.sources = []string{g.feeder}
			}
			add(n)
		}
	}
	for _, s := range p.stages {
		g := groups[key(s)]
		for _, t := range s.targets {
			n := &graphNode{name: t, stage: s.name, target: t, stages: []string{s.name}}
			switch {
			case entry(s):
				n.sources = []string{s.name}
			case g.shared:
				n.sources = []string{g.name}
			case g.wh:
				n.direct = true
			default:
				n.sources, n.all = g.up, len(g.up) > 1
			}
			add(n)
		}
	}
	return nodes
}

const (
	// the Kargo project kargo-<cap>-<app> is also a namespace
	maxProjectName = 63
	// Stage names must fit a label value: Kargo 1.12 labels each PromotionRequest with the unshortened
	// Stage name, so a longer name creates the Stage but fails every promotion.
	maxStageName = 63
)

func (m *model) project(app string) string { return "kargo-" + m.cap + "-" + app }

func (m *model) nameLengths(r reporter, a *appDef) {
	if n := m.project(a.name); len(n) > maxProjectName {
		r.at(nil, nil, fmt.Sprintf("Kargo project name %s is %d characters (max %d): shorten the app name", n, len(n), maxProjectName))
	}
	// target Stages are named after their target (checked on app.yml, validate.go); control-flow
	// Stage names are built from stage names
	for _, n := range m.graphs[a.name] {
		if n.control && len(n.name) > maxStageName {
			k, _ := getKV(a.file.doc, "stages", n.stage)
			r.at(k, []string{"stages", n.stage}, fmt.Sprintf("Kargo Stage name %s is %d characters (max %d): shorten the stage names", n.name, len(n.name), maxStageName))
		}
	}
}
