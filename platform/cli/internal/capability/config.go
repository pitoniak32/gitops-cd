// Package capability is the renderer: it reads a capability's YAML (capabilities/<cap>/, teams/),
// validates it into located diagnostics, and builds every generated file under rendered/.
package capability

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Cluster is one entry of the platform-owned cluster registry.
type Cluster struct {
	Name string
	// Region = Kargo shard name = the Argo CD instance that deploys to this cluster
	Region string
	// the cluster's name inside its regional Argo CD
	ArgocdName string
	// environment types a target on this cluster may have
	EnvironmentTypes []string
	// false: no External Secrets store; secret: env values are validated but not rendered
	SecretStore bool
}

// Check is a platform pre-deploy check (ADR 0028): a GitHub Actions workflow in the app's sourceRepo
// that Kargo dispatches and waits for before a target Stage pushes anything.
type Check struct {
	File    string // workflow file name under <sourceRepo>/.github/workflows/
	Ref     string // default main
	Timeout string // whole minutes 1m..120m, default 30m
}

var (
	checkFileRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.ya?ml$`)
	checkTimeoutRe = regexp.MustCompile(`^([1-9]|[1-9][0-9]|1[01][0-9]|120)m$`)
)

// Normalize applies the defaults and validates the check.
func (c Check) Normalize() (Check, error) {
	if c.Ref == "" {
		c.Ref = "main"
	}
	if c.Timeout == "" {
		c.Timeout = "30m"
	}
	if !checkFileRe.MatchString(c.File) {
		return c, fmt.Errorf("pre-deploy check file %q: expected a .yml or .yaml file name", c.File)
	}
	if !checkTimeoutRe.MatchString(c.Timeout) {
		return c, fmt.Errorf("pre-deploy check %s: timeout: whole minutes, 1m..120m", c.File)
	}
	return c, nil
}

// Config is everything platform-owned that the renderer needs besides the platform table.
type Config struct {
	// The gitops monorepo; generated output lives under rendered/ on branch `rendered` (ADR 0002).
	RepoURL  string
	Clusters []Cluster
	// regions a TERRAFORM or CLOUD_RUN target can sit in (ADR 0025)
	GcpRegions []string
	// per environment type: K8S replicas floor
	MinReplicas map[string]int
	// per environment type: pre-deploy checks, in order
	PreDeploy map[string][]Check
}

// EnvironmentTypes are the allowed target environment types.
var EnvironmentTypes = []string{"dev", "npr", "prd"}

const (
	renderedDir    = "rendered"
	renderedBranch = "rendered"
	emailDomain    = "example.com"
)

// Default is the platform's configuration. Team targets may only point at clusters listed here, and
// only with an environment type the cluster accepts. New region: add a cluster, `mise run regions`.
func Default() Config {
	return Config{
		RepoURL: "https://github.com/pitoniak32/gitops-cd",
		Clusters: []Cluster{
			// local kind clusters, one per region (platform/cluster/): every environment type
			{Name: "us-east4", Region: "us-east4", ArgocdName: "in-cluster", EnvironmentTypes: EnvironmentTypes},
			{Name: "dc1", Region: "dc1", ArgocdName: "in-cluster", EnvironmentTypes: EnvironmentTypes},
		},
		GcpRegions:  []string{"us-central1", "us-east1", "us-east4"},
		MinReplicas: map[string]int{"dev": 0, "npr": 1, "prd": 1},
		PreDeploy:   map[string][]Check{"prd": {{File: "precheck.yaml"}}},
	}
}

func (c Config) cluster(name string) (Cluster, bool) {
	for _, x := range c.Clusters {
		if x.Name == name {
			return x, true
		}
	}
	return Cluster{}, false
}

func (c Config) clusterNames() []string {
	var out []string
	for _, x := range c.Clusters {
		out = append(out, x.Name)
	}
	return out
}

// Regions are the distinct cluster regions, sorted.
func (c Config) Regions() []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range c.Clusters {
		if !seen[x.Region] {
			seen[x.Region] = true
			out = append(out, x.Region)
		}
	}
	sort.Strings(out)
	return out
}

func (c Config) ghRepo() string { return strings.TrimPrefix(c.RepoURL, "https://github.com/") }

// platform is everything that differs between target platforms: a new platform is an entry in
// platforms, and its renderer (output.go).
type platform struct {
	name string
	// target name suffix (ADR 0025) and the Kargo Stage's example.com/platform label
	suffix, label string
	// what an app must ship to deploy here: "image" or "module" (terraform app)
	artifact string
	// target.yml config fields (of targetFields) this platform requires; the others are not allowed
	fields []string
	// those of fields shown per target in pipelines/<cap>.json
	pipeline []string
	// the file in the target folder, and the key in it, where promotion pins the Freight's artifact
	pinFile, pinKey string
	// set: every deploy here uses it, whatever the layers say
	deployWith string
	// K8S: replicas must meet the environment type's MinReplicas
	minReplicas bool
	// the layer below app.yml (then defaults)
	defaults layer
	// default workflow file for gha deploys
	workflowFile string
}

func intp(i int) *int { return &i }

var platforms = []platform{
	{
		name: "K8S", suffix: "k8s", label: "k8s", artifact: "image",
		fields: []string{"cluster", "namespace"}, pipeline: []string{"cluster", "namespace"},
		pinFile: "./out/deployment.yaml", pinKey: "spec.template.spec.containers.0.image",
		minReplicas: true, workflowFile: "deploy.yaml",
	},
	// ADR 0024. Always gha; scale to zero; Cloud Run's default container port and smallest sensible
	// container (1 vCPU, 512Mi)
	{
		name: "CLOUD_RUN", suffix: "cr", label: "cloudrun", artifact: "image",
		fields: []string{"gcpProject", "region"}, pipeline: []string{"gcpProject", "region"},
		pinFile: "./out/service.yaml", pinKey: "spec.template.spec.containers.0.image",
		deployWith: "gha", workflowFile: "cloud-run.yaml",
		defaults: layer{replicas: intp(0), port: intp(8080), cpu: "1", memory: "512Mi", note: "platform default (CLOUD_RUN)"},
	},
	// ADR 0021
	{
		name: "TERRAFORM", suffix: "tf", label: "terraform", artifact: "module",
		fields: []string{"gcpProject", "backend", "region"}, pipeline: []string{"gcpProject"},
		pinFile: "./out/terraform.yaml", pinKey: "module", workflowFile: "terraform.yaml",
	},
}

func platformByName(n string) *platform {
	for i := range platforms {
		if platforms[i].name == n {
			return &platforms[i]
		}
	}
	return nil
}

func platformNames(artifact string) []string {
	var out []string
	for _, p := range platforms {
		if artifact == "" || p.artifact == artifact {
			out = append(out, p.name)
		}
	}
	return out
}

// the last layer, under the platform's
var defaults = layer{replicas: intp(2), cpu: "100m", memory: "128Mi", deployWith: "argocd", note: "global default"}

// What an app ships (exactly one of app.yml image / module); the name is also the Freight artifact's
// name in dispatch inputs and commit messages. A terraform app is always applied by a workflow.
var artifacts = map[string]struct{ app, deployWith string }{
	"image":  {app: "K8s"},
	"module": {app: "terraform", deployWith: "gha"},
}

func has(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
