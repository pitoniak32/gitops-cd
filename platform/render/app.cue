package render

import (
	"regexp"
	"list"
	"strings"
)

// Plain value, or a Secret Manager ref: secret:<gcp-project>/<secret>[@<version>]
#EnvValue: string & (!~"^secret:" | =~"^secret:[a-z][a-z0-9-]{4,28}[a-z0-9]/[A-Za-z0-9_-]+(@([0-9]+|latest))?$" | error("expected a plain string or secret:<gcp-project>/<secret>[@<version>]"))

// targets/<t>/environment-variables/<name>.yml: flat KEY: value map
#EnvironmentVariables: [=~"^[A-Za-z_][A-Za-z0-9_]*$"]: #EnvValue

// Settings that can be set at app, stage, or target level. Most specific wins;
// env is merged per key.
#Layer: {
	replicas?: int & >=0 & <=100
	env:       #EnvironmentVariables
	// targets/<t>/environment-variables/<name>.yml of each deployed target; every listed
	// name must exist for every target it reaches. A KEY from a more specific layer's
	// envFrom wins; any env wins over all of them.
	envFrom?: [...#Name]
	resources: {
		cpu?:    =~"^[0-9]+m?$"
		memory?: =~"^[0-9]+(Mi|Gi)$"
	}
	// How promotion deploys a target: argocd (default) syncs its Argo CD app; gha dispatches
	// `workflow` with the promoted commit and image and waits for the run (ADR 0020).
	deployWith?: "argocd" | "gha" | error("expected argocd or gha")
	// GitHub Actions workflow for gha targets (ignored where deployWith is argocd); each field
	// defaults separately: repo this repo, file deploy.yaml, ref main.
	workflow?: {
		repo?: =~"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$" | error("expected <owner>/<name>")
		// file name under <repo>/.github/workflows/
		file?: =~"^[A-Za-z0-9_.-]+\\.ya?ml$" | error("expected a .yml or .yaml file name")
		ref?:  string & !="" | error("expected a non-empty git ref")
	}
}

// apps/<name>/app.yml
#App: {
	#Layer

	// Exactly one of image (K8s app) or module (terraform app, ADR 0021).
	// Image repository, no tag or digest: the app's Warehouse discovers SemVer tags, Freight pins one
	// (by digest) next to the config commit, and promotion writes it into the target (ADR 0016).
	image?: _ociRepo
	// SemVer range for discovered tags (e.g. ^1, ~1.2); unset: any SemVer tag.
	imageConstraint?: string & !=""
	// Terraform module as an OCI artifact repository (oras push, single manifest), no tag or digest;
	// discovered and pinned like image. Its targets must be TERRAFORM targets.
	module?: _ociRepo
	// SemVer range for discovered module tags; unset: any SemVer tag.
	moduleConstraint?: string & !=""
	port?:             int & >0 & <65536 // set => Service on :80

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

_ociRepo: string & =~"^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)+$"

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

_ghRepo: strings.TrimPrefix(_repoURL, "https://github.com/")
_defaults: {
	replicas:   2
	cpu:        "100m"
	memory:     "128Mi"
	deployWith: "argocd"
	workflow: {repo: _ghRepo, file: "deploy.yaml", ref: "main"}
	tfWorkflow: {repo: _ghRepo, file: "terraform.yaml", ref: "main"}
}

// app.yml checks on the raw files (conditions on apps itself would cycle): exactly one of
// image/module; a terraform app takes no K8s-only settings at any layer.
_appFiles: {for k, v in in if k =~ _route.apps {(regexp.FindSubmatch(_route.apps, k)[1]): v}}
_tfLayer: {
	replicas?:   error("replicas not allowed on a terraform app")
	deployWith?: error("deployWith not allowed on a terraform app (always gha)")
	resources: {
		cpu?:    error("resources not allowed on a terraform app")
		memory?: error("resources not allowed on a terraform app")
	}
}
for an, v in _appFiles {
	if (v.image != _|_) == (v.module != _|_) {
		apps: (an): module: error("set exactly one of image (K8s app) or module (terraform app)")
	}
	if v.module != _|_ {
		apps: (an): _tfLayer & {
			port?:            error("port not allowed on a terraform app")
			imageConstraint?: error("imageConstraint not allowed on a terraform app (use moduleConstraint)")
			stages: [_]: _tfLayer & {targets: [_]: _tfLayer}
		}
	}
	if v.image != _|_ {
		apps: (an): moduleConstraint?: error("moduleConstraint not allowed on a K8s app (use imageConstraint)")
	}
}

// One entry per (app, target): the effective, fully merged config.
// Keyed by app/target, so using a target in two stages is a conflict.
deploys: {for an, a in apps for sn, s in a.stages for tn, t in s.targets {
	"\(an)/\(tn)": {
		app:    an
		stage:  sn
		target: tn
		tgt:    targets[tn].config
		kind: [if a.module != _|_ {"terraform"}, "k8s"][0]
		let _want = {k8s: "K8S", terraform: "TERRAFORM"}[kind]
		if tgt.platform != _want {
			platform: error("target \(tn) is \(tgt.platform); \({k8s: "K8s", terraform: "terraform"}[kind]) apps need \(_want) targets")
		}

		if kind == "k8s" {
			image: a.image
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

			deployWith: [
				if t.deployWith != _|_ {t.deployWith},
				if s.deployWith != _|_ {s.deployWith},
				if a.deployWith != _|_ {a.deployWith},
				_defaults.deployWith,
			][0]

			if tgt.platform == "K8S" {
				// two targets of one app must not share cluster+namespace
				_placement: _placements[an]["\(tgt.cluster)/\(tgt.namespace)"] & tn
			}
		}
		if kind == "terraform" {
			module:     a.module
			deployWith: "gha"
			if tgt.platform == "TERRAFORM" {
				backend: {
					bucket: tgt.terraform.backend.bucket
					prefix: *tgt.terraform.backend.prefix | "\(_cap)/\(an)/\(tn)"
				}
			}
		}
		let _wfDefaults = [if kind == "terraform" {_defaults.tfWorkflow}, _defaults.workflow][0]
		workflow: {for f, dv in _wfDefaults {
			(f): [
				if t.workflow[f] != _|_ {t.workflow[f]},
				if s.workflow[f] != _|_ {s.workflow[f]},
				if a.workflow[f] != _|_ {a.workflow[f]},
				dv,
			][0]
		}}

		envFrom: list.SortStrings([for n, _ in {for l in [a, s, t] if l.envFrom != _|_ for n in l.envFrom {(n): _}} {n}])
		for n in envFrom if targetEnvs[tn][n] == _|_ {
			envFrom: error("target \(tn) has no environment-variables/\(n).yml")
		}

		// Per layer: KEY -> value -> files. A KEY from files listed at the most specific
		// layer wins; equal values are fine, different values at one layer are ambiguous.
		_byLayer: {for ln, l in {app: a, stage: s, target: t} {
			(ln): {if l.envFrom != _|_ for n in l.envFrom if targetEnvs[tn][n] != _|_ for k, v in targetEnvs[tn][n] {(k): (v): (n): _}}
		}}
		for ln, m in _byLayer for k, vs in m if len(vs) > 1 {
			envFrom: error("target \(tn): \(k) differs between environment-variables/{\(strings.Join(list.SortStrings([for _, ns in vs for n, _ in ns {n}]), ","))}.yml (both in \(ln)-level envFrom)")
		}
		_layerEnv: {for ln, m in _byLayer {(ln): {for k, vs in m if len(vs) == 1 for v, _ in vs {(k): v}}}}
		_fromEnv: {
			for k, v in _layerEnv.app if _layerEnv.stage[k] == _|_ if _layerEnv.target[k] == _|_ {(k): v}
			for k, v in _layerEnv.stage if _layerEnv.target[k] == _|_ {(k): v}
			for k, v in _layerEnv.target {(k): v}
		}

		env: {
			for k, v in _fromEnv if a.env[k] == _|_ if s.env[k] == _|_ if t.env[k] == _|_ {(k): v}
			for k, v in a.env if s.env[k] == _|_ if t.env[k] == _|_ {(k): v}
			for k, v in s.env if t.env[k] == _|_ {(k): v}
			for k, v in t.env {(k): v}
		}

	}
}}

_placements: {for an, a in apps {
	(an): {for _, s in a.stages for tn, _ in s.targets if targets[tn].config.platform == "K8S" {
		"\(targets[tn].config.cluster)/\(targets[tn].config.namespace)": tn
	}}
}}
