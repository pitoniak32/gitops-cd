package render

import (
	"list"
	"strings"
)

// Kargo node graph per app (ADR 0019): target Stages <app>-<target> plus control-flow Stages only where
// they add value.
//   Group a stage's targets by the stage's (app-resolved) dependency set. U = upstream targets of that set
//   (or the Warehouse for first stages), C = all targets of the stages sharing that set.
//   - |U| == 1 or |C| == 1: direct (1->1, 1->N, M->1 with availabilityStrategy All).
//   - |U| > 1 and |C| > 1: one shared control-flow Stage waits for All of U; consumers request from it.
//     Named <app>-<D> when one stage D consumes it (fan-in), <app>-<S>-done after one stage S (fan-out),
//     <app>-<S1>-<S2>...-done otherwise.
//   - One click per stage: Kargo's "Promote to downstream" promotes to a node's direct downstream Stages, so
//     every stage with >1 target must be exactly one node's downstream set. If its feeder also feeds other
//     stages (or is the Warehouse, which can't promote to several Stages at once), the stage gets an entry
//     control-flow Stage <app>-<D>.
//   Control-flow Stages always auto-promote; they only mean "upstream verified".
#GraphNode: {
	kind:    "control-flow" | "target"
	stage:   string
	target?: string
	direct:  bool | *false
	sources: [...string] | *[]
	all: bool | *false
}

_whNode: "@warehouse"

_gp: {for an, p in pipelines {(an): {
	key: {for sn, s in p.stages {(sn): "k:" + strings.Join(s.dependsOn, ",")}}
	_groups: {for sn, s in p.stages {(key[sn]): {deps: s.dependsOn, members: (sn): true}}}
	grp: {for k, g in _groups {(k): {
		deps: g.deps
		mem: list.SortStrings([for m, _ in g.members {m}])
		wh: len(deps) == 0
		U: list.SortStrings([for d in deps for t, _ in p.stages[d].targets {"\(an)-\(t)"}])
		nC: len([for m in mem for t, _ in p.stages[m].targets {t}])
		shared: !wh && len(U) > 1 && nC > 1
		// only evaluated for shared groups (deps non-empty)
		let sname = [
			if len(mem) == 1 {"\(an)-\(mem[0])"},
			if len(deps) == 1 {"\(an)-\(deps[0])-done"},
			"\(an)-\(strings.Join(deps, "-"))-done",
		][0]
		if shared {
			name: sname
			label: [if len(mem) == 1 {mem[0]}, deps[0]][0]
		}

		// the single node feeding this group's targets; "" when each target joins several sources (M->1)
		feeder: [
			if shared {sname},
			if wh {_whNode},
			if len(U) == 1 {U[0]},
			"",
		][0]
	}}}
	// base downstream size of each upstream target: every group it feeds (direct, M->1 join, or one shared
	// node), so a stage fed by it alone gets an entry whenever "Promote to downstream" there would reach more
	_down: {for k, g in grp if !g.wh for u in g.U {(u): (k): [if g.shared {1}, g.nC][0]}}
	down: {for t, ks in _down {(t): list.Sum([for _, n in ks {n}])}}
	st: {for sn, s in p.stages {(sn): {
		let g = grp[key[sn]]
		nT: len(s.targets)
		entry: nT > 1 && g.feeder != "" && [
			if g.feeder == _whNode {true},
			if g.shared {len(g.mem) > 1},
			if down[g.feeder] != _|_ {down[g.feeder] != nT},
			false,
		][0]
	}}}
}}}

_graph: [app=string]: [node=string]: #GraphNode
_graph: {for an, p in pipelines {(an): {
	let a = _gp[an]
	for _, g in a.grp if g.shared {
		(g.name): {kind: "control-flow", stage: g.label, sources: g.U, all: true}
	}
	for sn, d in a.st if d.entry {
		let g = a.grp[a.key[sn]]
		("\(an)-\(sn)"): {
			kind:  "control-flow"
			stage: sn
			if g.feeder == _whNode {direct: true}
			if g.feeder != _whNode {sources: [g.feeder]}
		}
	}
	for sn, s in p.stages let d = a.st[sn] let g = a.grp[a.key[sn]] for tn, _ in s.targets {
		("\(an)-\(tn)"): {
			kind:   "target"
			stage:  sn
			target: tn
			if d.entry {sources: ["\(an)-\(sn)"]}
			if !d.entry && g.shared {sources: [g.name]}
			if !d.entry && !g.shared && g.wh {direct: true}
			if !d.entry && !g.shared && !g.wh {sources: g.U, all: len(g.U) > 1}
		}
	}
}}}
