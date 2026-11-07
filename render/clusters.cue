package render

// Platform-owned. Team targets may only point at clusters listed here,
// and only with an environment the cluster accepts.
#Environment: "dev" | "staging" | "perf" | "prod"

#Cluster: {
	argocdName: string // Argo CD cluster secret name
	kind:       "kind" | "gke" | "onprem"
	region?:    string
	environments: [...#Environment]
	// false: no External Secrets store; secret: env values are validated but not rendered
	secretStore: *true | bool
}

clusters: [N=string]: #Cluster & {argocdName: *N | string}
clusters: {
	// local kind cluster (cluster/kind.yaml): every environment, one namespace per target
	"in-cluster": {kind: "kind", environments: ["dev", "staging", "perf", "prod"], secretStore: false}
}

_clusterNames: [for k, _ in clusters {k}]

// per-environment policy
policy: [#Environment]: {minReplicas: int}
policy: {
	dev: minReplicas:     0
	staging: minReplicas: 1
	perf: minReplicas:    1
	prod: minReplicas:    1
}
