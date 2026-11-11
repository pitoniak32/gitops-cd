package render

// Platform-owned. Team targets may only point at clusters listed here,
// and only with an environment type the cluster accepts.
#EnvironmentType: "dev" | "npr" | "prd"

// regions a target or cluster can sit in (ADR 0025)
_gcpRegions: ["us-central1", "us-east1", "us-east4"]
_dcRegions: ["dc1", "dc2"]
#GcpRegion:        or(_gcpRegions)
#DatacenterRegion: or(_dcRegions)

#Cluster: {
	// Region = Kargo shard name = Argo CD instance that deploys to this cluster (rendered/argocd/<region>/)
	region: #GcpRegion | #DatacenterRegion
	// the cluster's name inside its regional Argo CD (its own cluster by default)
	argocdName: *"in-cluster" | string
	kind:       "kind" | "gke" | "onprem"
	environmentTypes: [...#EnvironmentType]
	// false: no External Secrets store; secret: env values are validated but not rendered
	secretStore: *true | bool
}

clusters: [string]: #Cluster
clusters: {
	// local kind clusters, one per region (platform/cluster/): every environment type, one namespace per target
	"us-east4": {region: "us-east4", kind: "kind", environmentTypes: ["dev", "npr", "prd"], secretStore: false}
	dc1: {region: "dc1", kind: "kind", environmentTypes: ["dev", "npr", "prd"], secretStore: false}
}

_clusterNames: [for k, _ in clusters {k}]

// per-environment-type policy
policy: [#EnvironmentType]: {minReplicas: int}
policy: {
	dev: minReplicas: 0
	npr: minReplicas: 1
	prd: minReplicas: 1
}
