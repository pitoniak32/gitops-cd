package render

import (
	"list"
	"strings"
)

// stages/<name>.yml
#Stage: {
	dependsOn: [...[if generic {#Name}, or(_stageNames) | error("unknown stage; allowed: \(_stageList)")][0]]
	// promote automatically once upstream stages are verified
	autoPromote: *false | bool
	// Kargo UI color for all of this stage's Kargo Stages (gate + targets); default picked per stage
	color?: or(_kargoColors) | error("unknown color; allowed: \(strings.Join(_kargoColors, ", "))")
}

// Kargo UI named stage colors (kargo.akuity.io/color)
_kargoColors: ["red", "salmon", "orange", "amber", "yellow", "lime", "avocado", "green", "teal", "cyan", "sky", "blue", "indigo", "periwinkle", "violet", "purple", "fuchsia", "pink", "rose", "dragonfruit", "gray"]
// default: spread sorted stage names across the palette
_defaultColors: ["blue", "amber", "violet", "green", "red", "teal", "pink", "orange", "indigo", "lime"]
_stageColor: {
	let names = list.SortStrings(_stageNames)
	for i, n in names {(n): [if stages[n].color != _|_ {stages[n].color}, _defaultColors[mod(i, len(_defaultColors))]][0]}
}

// targets/<name>/target.yml — where a stage deploys. Reusable across apps.
// Sibling environment-variables/<n>.yml files: see #EnvironmentVariables, envFrom.
#Target: {
	config: {
		cluster:     or(_clusterNames) | error("unknown cluster; allowed: \(strings.Join(_clusterNames, ", "))")
		namespace:   =~"^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$"
		environment: #Environment
		if !generic if clusters[cluster] != _|_ {
			// the cluster must accept this environment
			environment: or(clusters[cluster].environments) | error("cluster \(cluster) accepts environments: \(strings.Join(clusters[cluster].environments, ", "))")
		}
		auth: wifEmail: =~"^[a-z0-9-]+@[a-z0-9-]+\\.iam\\.gserviceaccount\\.com$"
		billing:  =~"^[a-z0-9-]+$"
		platform: "K8S" // only K8S is rendered today
	}
}

// Longest path from a root. A dependency cycle shows up as "structural cycle".
stageDepth: {for s, v in stages {
	(s): list.Max([0, for d in v.dependsOn {stageDepth[d] + 1}])
}}

// Prod is only ever promoted by a person: no autoPromote on a stage that deploys to a prod target.
_prodStages: {for _, d in deploys if d.tgt.environment == "prod" {(d.stage): _}}
stages: [S=_]: {
	if _prodStages[S] != _|_ {
		autoPromote: false | error("stage \(S) deploys to prod targets; autoPromote not allowed")
	}
}
