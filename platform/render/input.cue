package render

import (
	"list"
	"regexp"
	"strings"
	"struct"
)

// Injected by scripts/render.sh.
capability: =~"^[a-z][a-z0-9-]{0,29}$" @tag(capability)
// The gitops monorepo; generated output lives under renderedDir in it.
repoURL:     *"https://github.com/pitoniak32/gitops-cd" | string @tag(repo)
renderedDir: "rendered"
// CI commits renderedDir to this branch (ADR 0022); Warehouses and Argo CD read it from there, never main.
renderedBranch: "rendered"

// -t generic=true: export editor schemas without capability data. Stage/target names
// become a name pattern instead of the capability's actual names (checked by lint/CI).
generic: *false | bool @tag(generic,type=bool)

_nameRe: "^[a-z][a-z0-9-]{0,39}$"
#Name:   =~_nameRe

// YAML is routed here by render.sh as in."<path relative to repo root>":
//   capabilities/<cap>/stages/dev.yml, .../targets/<t>/target.yml,
//   .../targets/<t>/environment-variables/<name>.yml, .../apps/<app>/app.yml,
//   capabilities/<cap>/owners.yml, teams/<team>.yml
in: [string]: _
let C = "capabilities/\(capability)/"
_route: {
	stages:  "^\(C)stages/([^/]+)\\.ya?ml$"
	targets: "^\(C)targets/([^/]+)/target\\.ya?ml$"
	apps:    "^\(C)apps/([^/]+)/app\\.ya?ml$"
	teams:   "^teams/([^/]+)\\.ya?ml$"
}
for f, re in _route {(f): {for k, v in in if k =~ re {(regexp.FindSubmatch(re, k)[1]): v}}}

// targets/<t>/environment-variables/<name>.yml: env vars an app pulls in with envFrom: [<name>]
_targetEnvRe: "^\(C)targets/([^/]+)/environment-variables/([^/]+)\\.ya?ml$"
targetEnvs: {for k, v in in if k =~ _targetEnvRe let m = regexp.FindSubmatch(_targetEnvRe, k) {(m[1]): (m[2]): v}}
targetEnvs: [_]: [_]:         #EnvironmentVariables
targetEnvs: [_]: [!~_nameRe]: badName
// raw file names again (conditions on targetEnvs/targets themselves would cycle)
_targetFiles: {for k, _ in in if k =~ _route.targets {(regexp.FindSubmatch(_route.targets, k)[1]): _}}
for k, _ in in if k =~ _targetEnvRe let tn = regexp.FindSubmatch(_targetEnvRe, k)[1] if _targetFiles[tn] == _|_ {
	targetEnvs: (tn): error("environment-variables without targets/\(tn)/target.yml")
}

// environmentType is required; the raw file is checked so a missing value gets a located message
for k, v in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if v.config.environmentType == _|_ {
	targets: (tn): config: environmentType: error("expected dev, npr or prd")
}

// a TERRAFORM or CLOUD_RUN target needs a region (a K8S target's is its cluster's)
for k, v in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if v.config.platform != _|_ if list.Contains(["TERRAFORM", "CLOUD_RUN"], v.config.platform) if v.config.region == _|_ {
	targets: (tn): config: region: error("region required on a \(v.config.platform) target; allowed: \(strings.Join(_gcpRegions, ", "))")
}

// a TERRAFORM or CLOUD_RUN target needs a GCP project; TERRAFORM also a state backend
for k, v in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if v.config.platform != _|_ if list.Contains(["TERRAFORM", "CLOUD_RUN"], v.config.platform) if v.config.gcpProject == _|_ {
	targets: (tn): config: gcpProject: error("expected a GCP project id")
}
for k, v in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if v.config.platform == "TERRAFORM" if v.config.backend == _|_ {
	targets: (tn): config: backend: error("backend required on a TERRAFORM target (bucket, optional prefix)")
}

// the nested terraform: key moved up to config.gcpProject / config.backend
for k, v in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if v.config.terraform != _|_ {
	targets: (tn): config: terraform: error("moved: use config.gcpProject / config.backend")
}

// name must be <region>-<env>-<platform>[-<qualifier>] (ADR 0025); from the raw file so it can't cycle through targets.
// Skipped while the region can't be resolved (unknown cluster / missing region already error).
for k, v in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if v.config.environmentType != _|_ if list.Contains(["dev", "npr", "prd"], v.config.environmentType) {
	let _plat = [if v.config.platform != _|_ {v.config.platform}, "K8S"][0]
	let _reg = [if list.Contains(["TERRAFORM", "CLOUD_RUN"], _plat) if v.config.region != _|_ if list.Contains(_gcpRegions, v.config.region) {v.config.region}, if _plat == "K8S" if v.config.cluster != _|_ if clusters[v.config.cluster] != _|_ {clusters[v.config.cluster].region}, _|_][0]
	if _platformSuffix[_plat] != _|_ if _reg != _|_ {
		let _p = "\(_reg)-\(v.config.environmentType)-\(_platformSuffix[_plat])"
		if !regexp.Match("^\(_p)(-[a-z0-9]+)?$", tn) {
			targets: (tn): error("name must be <region>-<env>-<platform>[-<qualifier>]: expected \(_p)[-<qualifier>]")
		}
	}
}

// pre-dir layout targets/<t>.yml: inputs.sh still passes them so they fail loudly
_legacyTargetRe: "^\(C)targets/([^/]+)\\.ya?ml$"
for k, _ in in if k =~ _legacyTargetRe let tn = regexp.FindSubmatch(_legacyTargetRe, k)[1] {
	legacyTargets: (tn): error("move to targets/\(tn)/target.yml")
}

// pre-ADR 0023 apps/<app>/owners.yml: inputs.sh still passes them so they fail loudly
_legacyAppOwnersRe: "^\(C)apps/([^/]+)/owners\\.ya?ml$"
for k, _ in in if k =~ _legacyAppOwnersRe let an = regexp.FindSubmatch(_legacyAppOwnersRe, k)[1] {
	legacyAppOwners: (an): error("move into capabilities/\(capability)/owners.yml apps.\(an)")
}

_capOwnerFiles: [for k, v in in if k =~ "^\(C)owners\\.ya?ml$" {v}] // 0 or 1 element
capOwners:                                                          _capOwnerFiles

stages: [string]:  #Stage
targets: [string]: #Target
apps: [string]:    #App
// folder/file names become k8s, Kargo and Argo CD names
let badName = error("invalid name; use lowercase letters, digits and -, starting with a letter (\(_nameRe))")
stages: [!~_nameRe]:  badName
targets: [!~_nameRe]: badName
apps: [!~_nameRe]:    badName
// Kargo Stage names are <app>-<target> and, for control flow (graph.cue, ADR 0019), <app>-<stage> and
// <app>-<stage>-done: a target can't be named like a stage or <stage>-done.
// Checked on the raw file names (a condition on targets itself would cycle).
for k, _ in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if stages[tn] != _|_ {
	targets: (tn): error("target name \(tn) is also a stage name; Kargo stage names <app>-<name> would collide")
}

// raw stage file names (reading `stages` here would cycle through the prod-stage rules)
_stageFiles: {for k, _ in in if k =~ _route.stages {(regexp.FindSubmatch(_route.stages, k)[1]): _}}
for tn, _ in _targetFiles if strings.HasSuffix(tn, "-done") if _stageFiles[strings.TrimSuffix(tn, "-done")] != _|_ {
	targets: (tn): error("target name \(tn) is <stage>-done; Kargo control-flow stage names <app>-\(tn) would collide")
}

// kept outside #App so the generated JSON Schema stays precise
apps: [_]: {
	stages: struct.MinFields(1)
	stages: [_]: targets: struct.MinFields(1)
}

_stageNames: [for k, _ in stages {k}]
_appNames: [for k, _ in apps {k}]
_targetNames: [for k, _ in targets {k}]

// "not one of these names", for custom unknown-key errors
_notStage: and([for n in _stageNames {!=n}])
_notTarget: and([for n in _targetNames {!=n}])
_stageList: strings.Join(_stageNames, ", ")
_teamNames: [for k, _ in teams {k}]
_teamList:   strings.Join(_teamNames, ", ")
_targetList: strings.Join(_targetNames, ", ")
_appList:    strings.Join(_appNames, ", ")
_notApp: and([for n in _appNames {!=n}])

// unshadowable aliases for use inside structs that have fields of the same name
_cap:        capability
_repoURL:    repoURL
_capStages:  stages
_capApps:    apps
_capTargets: targets
