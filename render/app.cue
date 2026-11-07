package render

// Plain value, or a Secret Manager ref: secret:<gcp-project>/<secret>[@<version>]
#EnvValue: string & (!~"^secret:" | =~"^secret:[a-z][a-z0-9-]{4,28}[a-z0-9]/[A-Za-z0-9_-]+(@([0-9]+|latest))?$" | error("expected a plain string or secret:<gcp-project>/<secret>[@<version>]"))

// Settings that can be set at app, stage, or target level. Most specific wins;
// env is merged per key.
#Layer: {
	replicas?: int & >=0 & <=100
	env: [=~"^[A-Z_][A-Z0-9_]*$"]: #EnvValue
	resources: {
		cpu?:    =~"^[0-9]+m?$"
		memory?: =~"^[0-9]+(Mi|Gi)$"
	}
}

// apps/<name>/app.yml
#App: {
	#Layer
	image: string & =~"(:[A-Za-z0-9_.-]+|@sha256:[a-f0-9]{64})$" & !~":latest$"
	port?: int & >0 & <65536 // set => Service on :80

	// Subset of the capability's stages. Skipped stages are bypassed in the pipeline
	// (prod depends on [staging, perf]; without perf, prod depends on staging).
	stages: {
		if generic {[#Name]: #StageLayer}
		if !generic {
			for n in _stageNames {(n)?: #StageLayer}
			[_notStage]: error("unknown stage; allowed: \(_stageList)")
		}
	}
}

#StageLayer: {
	#Layer
	targets: {
		if generic {[#Name]: #Layer}
		if !generic {
			for n in _targetNames {(n)?: #Layer}
			[_notTarget]: error("unknown target; allowed: \(_targetList)")
		}
	}
}

_defaults: {replicas: 2, cpu: "100m", memory: "128Mi"}

// One entry per (app, target): the effective, fully merged config.
// Keyed by app/target, so using a target in two stages is a conflict.
deploys: {for an, a in apps for sn, s in a.stages for tn, t in s.targets {
	"\(an)/\(tn)": {
		app:    an
		stage:  sn
		target: tn
		tgt:    targets[tn].config
		image:  a.image
		if a.port != _|_ {port: a.port}

		replicas: [
			if t.replicas != _|_ {t.replicas},
			if s.replicas != _|_ {s.replicas},
			if a.replicas != _|_ {a.replicas},
			_defaults.replicas,
		][0] & (>=policy[tgt.environment].minReplicas | error("\(tgt.environment) requires replicas >= \(policy[tgt.environment].minReplicas)"))

		cpu: [
			if t.resources.cpu != _|_ {t.resources.cpu},
			if s.resources.cpu != _|_ {s.resources.cpu},
			if a.resources.cpu != _|_ {a.resources.cpu},
			_defaults.cpu,
		][0]
		memory: [
			if t.resources.memory != _|_ {t.resources.memory},
			if s.resources.memory != _|_ {s.resources.memory},
			if a.resources.memory != _|_ {a.resources.memory},
			_defaults.memory,
		][0]

		env: {
			for k, v in a.env if s.env[k] == _|_ if t.env[k] == _|_ {(k): v}
			for k, v in s.env if t.env[k] == _|_ {(k): v}
			for k, v in t.env {(k): v}
		}

		// two targets of one app must not share cluster+namespace
		_placement: _placements[an]["\(tgt.cluster)/\(tgt.namespace)"] & tn
	}
}}

_placements: {for an, a in apps {
	(an): {for _, s in a.stages for tn, _ in s.targets {
		"\(targets[tn].config.cluster)/\(targets[tn].config.namespace)": tn
	}}
}}
