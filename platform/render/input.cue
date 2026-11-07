package render

import (
	"regexp"
	"strings"
	"struct"
)

// Injected by scripts/render.sh.
capability: =~"^[a-z][a-z0-9-]{0,29}$" @tag(capability)
// The gitops monorepo; generated output lives under renderedDir in it.
repoURL:     *"https://github.com/pitoniak32/gitops-cd" | string @tag(repo)
renderedDir: "rendered"

// -t generic=true: export editor schemas without capability data. Stage/target names
// become a name pattern instead of the capability's actual names (checked by lint/CI).
generic: *false | bool @tag(generic,type=bool)

_nameRe: "^[a-z][a-z0-9-]{0,39}$"
#Name:   =~_nameRe

// YAML is routed here by render.sh as in."<path relative to repo root>":
//   capabilities/<cap>/stages/dev.yml, .../targets/<t>/target.yml,
//   .../targets/<t>/environment-variables/<name>.yml, .../apps/<app>/{app,owners}.yml,
//   capabilities/<cap>/owners.yml, teams/<team>.yml
in: [string]: _
let C = "capabilities/\(capability)/"
_route: {
	stages:    "^\(C)stages/([^/]+)\\.ya?ml$"
	targets:   "^\(C)targets/([^/]+)/target\\.ya?ml$"
	apps:      "^\(C)apps/([^/]+)/app\\.ya?ml$"
	appOwners: "^\(C)apps/([^/]+)/owners\\.ya?ml$"
	teams:     "^teams/([^/]+)\\.ya?ml$"
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

// pre-dir layout targets/<t>.yml: inputs.sh still passes them so they fail loudly
_legacyTargetRe: "^\(C)targets/([^/]+)\\.ya?ml$"
for k, _ in in if k =~ _legacyTargetRe let tn = regexp.FindSubmatch(_legacyTargetRe, k)[1] {
	legacyTargets: (tn): error("move to targets/\(tn)/target.yml")
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
// Kargo Stage names are <app>-<stage> (gates) and <app>-<target>: a target can't share a stage's name.
// Checked on the raw file names (a condition on targets itself would cycle).
for k, _ in in if k =~ _route.targets let tn = regexp.FindSubmatch(_route.targets, k)[1] if stages[tn] != _|_ {
	targets: (tn): error("target name \(tn) is also a stage name; Kargo stage names <app>-<name> would collide")
}

// kept outside #App so the generated JSON Schema stays precise
apps: [_]: {
	stages: struct.MinFields(1)
	stages: [_]: targets: struct.MinFields(1)
}

_stageNames: [for k, _ in stages {k}]
_targetNames: [for k, _ in targets {k}]

// "not one of these names", for custom unknown-key errors
_notStage: and([for n in _stageNames {!=n}])
_notTarget: and([for n in _targetNames {!=n}])
_stageList: strings.Join(_stageNames, ", ")
_teamNames: [for k, _ in teams {k}]
_teamList:   strings.Join(_teamNames, ", ")
_targetList: strings.Join(_targetNames, ", ")

// unshadowable aliases for use inside structs that have fields of the same name
_cap:        capability
_repoURL:    repoURL
_capStages:  stages
_capTargets: targets
