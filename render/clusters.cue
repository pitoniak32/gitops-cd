package render

// Platform-owned. Team targets may only point at clusters listed here,
// and only with an environment the cluster accepts.
#Environment: "dev" | "staging" | "perf" | "prod"

#Cluster: {
	argocdName: string // Argo CD cluster secret name
	kind:       "gke" | "onprem"
	region?:    string
	environments: [...#Environment]
}

clusters: [N=string]: #Cluster & {argocdName: *N | string}
clusters: {
	"gke-use4-dev": {kind: "gke", region: "us-east4", environments: ["dev"]}
	"onprem-d1": {kind: "onprem", environments: ["dev"]}
	"gke-use1-nonprod": {kind: "gke", region: "us-east1", environments: ["staging", "perf"]}
	"onprem-s1": {kind: "onprem", environments: ["staging", "perf"]}
	"gke-use1-prod": {kind: "gke", region: "us-east1", environments: ["prod"]}
	"gke-euw4-prod": {kind: "gke", region: "europe-west4", environments: ["prod"]}
	"onprem-p1": {kind: "onprem", environments: ["prod"]}
	"onprem-p2": {kind: "onprem", environments: ["prod"]}
}

_clusterNames: [for k, _ in clusters {k}]

// per-environment policy
policy: [#Environment]: {minReplicas: int}
policy: {
	dev: minReplicas:     0
	staging: minReplicas: 1
	perf: minReplicas:    1
	prod: minReplicas:    2
}
