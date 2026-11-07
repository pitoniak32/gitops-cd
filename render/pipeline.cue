package render

import "list"

// Neutral per-app promotion graph: input for Kargo (kargo.cue) or a Temporal
// workflow, which can run `levels` in order and the stages within a level in parallel.
pipelines: {for an, a in apps {
	(an): {
		capability: _cap
		app:        an

		// app-level deps: a skipped stage passes its own deps through
		_eff: {for s, v in _capStages {
			(s): {for d in v.dependsOn {
				if a.stages[d] != _|_ {(d): true}
				if a.stages[d] == _|_ {_eff[d]}
			}}
		}}

		// transitive upstream stages used by this app
		_anc: {for s, _ in _capStages {
			(s): {for d, _ in _eff[s] {(d): true, _anc[d]}}
		}}

		stages: {for sn, s in a.stages {
			(sn): {
				// drop deps already implied by another dep (dev when staging also depends on dev)
				dependsOn: list.SortStrings([for d, _ in _eff[sn]
					if len([for e, _ in _eff[sn] if _anc[e][d] != _|_ {e}]) == 0 {d}])
				autoPromote: _capStages[sn].autoPromote
				level:       stageDepth[sn]
				targets: {for tn, _ in s.targets {
					(tn): {
						cluster:     _capTargets[tn].config.cluster
						namespace:   _capTargets[tn].config.namespace
						environment: _capTargets[tn].config.environment
						path:        "apps/\(_cap)/\(an)/\(sn)/\(tn)"
					}
				}}
			}
		}}

		_maxLevel: list.Max([for _, s in stages {s.level}])
		levels: [for l in list.Range(0, _maxLevel+1, 1)
			let names = list.SortStrings([for sn, s in stages if s.level == l {sn}])
			if len(names) > 0 {names}]
	}
}}
