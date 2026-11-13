package system

import (
	"sort"
	"strconv"
	"strings"

	. "github.com/pitoniak32/gitops-cd/platform/cli/internal/emit"
)

// files is everything rendered for the system, keyed by path under rendered/.
// render.System first wipes this system's paths: apps/<sys>/, argocd/{projects/<region>/<sys>.yaml,
// deploys/<region>/<sys>/}, kargo/<sys>.yaml, pipelines/<sys>.json, codeowners/<sys>
func (m *model) files() map[string]string {
	out := map[string]string{}
	for _, d := range m.deploys {
		dir := "apps/" + m.sys + "/" + d.app.name + "/" + d.stage + "/" + d.target + "/"
		switch d.tgt.platform.name {
		case "K8S":
			// the Deployment gets its own single-document file: promotion pins the Freight's image in it
			// with yaml-update, which only addresses a file's first document (ADR 0016)
			var rest []any
			for _, o := range m.k8s(d) {
				if o.Get("kind") == "Deployment" {
					out[dir+"deployment.yaml"] = YAML(o)
				} else {
					rest = append(rest, o)
				}
			}
			out[dir+"manifests.yaml"] = YAMLStream(rest)
		case "CLOUD_RUN":
			out[dir+"service.yaml"] = YAML(m.cloudRun(d))
		case "TERRAFORM":
			out[dir+"terraform.yaml"] = YAML(m.terraform(d))
		}
	}
	out["kargo/"+m.sys+".yaml"] = YAMLStream(m.kargo())
	for region, p := range m.argocdProjects() {
		out["argocd/projects/"+region+"/"+m.sys+".yaml"] = YAML(p)
	}
	for _, d := range m.argoDeploys() {
		out["argocd/deploys/"+m.region(d)+"/"+m.sys+"/"+d.app.name+"-"+d.target+".json"] = JSONIndent(m.argocdParams(d))
	}
	out["pipelines/"+m.sys+".json"] = JSONIndent(m.pipelinesJSON())
	// CODEOWNERS fragment; `gcd codeowners` splices all systems into .github/CODEOWNERS
	out["codeowners/"+m.sys] = strings.Join(m.codeowners(), "\n") + "\n"
	return out
}

func (m *model) codeowners() []string {
	gh := func(teams []string) string {
		var l []string
		for _, t := range teams {
			l = append(l, "@"+m.teams[t])
		}
		return strings.Join(l, " ")
	}
	lines := []string{"/systems/" + m.sys + "/ " + gh(m.owners.allOwners)}
	var names []string
	for _, a := range m.apps {
		names = append(names, a.name)
	}
	for _, an := range sorted(names) {
		lines = append(lines, "/systems/"+m.sys+"/apps/"+an+"/ "+gh(m.owners.codeOwners[an]))
	}
	return lines
}

func (m *model) pipelinesJSON() Map {
	out := Map{}
	for _, a := range m.apps {
		p := m.pipelines[a.name]
		stages := Map{}
		for _, s := range p.stages {
			targets := Map{}
			for _, tn := range s.targets {
				t := m.target(tn)
				e := Map{}
				for _, f := range t.platform.pipeline {
					e = append(e, KV{K: f, V: map[string]string{"cluster": t.cluster, "namespace": t.namespace, "gcpProject": t.gcpProject, "region": t.region}[f]})
				}
				e = append(e, M("platform", t.platform.name, "environment", t.environment, "environmentType", t.environmentType,
					"path", "apps/"+m.sys+"/"+a.name+"/"+s.name+"/"+tn, "deployWith", m.deploy(a.name, tn).deployWith)...)
				targets = append(targets, KV{K: tn, V: e})
			}
			stages = append(stages, KV{K: s.name, V: M("dependsOn", Strings(s.dependsOn), "autoPromote", s.autoPromote, "level", s.level, "targets", targets)})
		}
		levels := List{}
		for _, l := range p.levels {
			levels = append(levels, Strings(l))
		}
		out = append(out, KV{K: a.name, V: M("system", m.sys, "app", a.name, "stages", stages, "levels", levels)})
	}
	return out
}

// ---- workloads

func (m *model) k8sLabels(d *deploy) Map {
	return M(
		"app.kubernetes.io/managed-by", "cue-platform",
		"app.kubernetes.io/name", d.app.name,
		"example.com/system", m.sys,
		"example.com/stage", d.stage,
		"example.com/target", d.target,
		"example.com/environment", d.tgt.environment,
		"example.com/environment-type", d.tgt.environmentType,
		"example.com/billing", d.tgt.billing,
	)
}

// secretRef is an env value secret:<gcp-project>/<secret>[@<version>]
type secretRef struct{ project, secret, version string }

func parseSecret(v string) (secretRef, bool) {
	mm := secretRe.FindStringSubmatch(v)
	if mm == nil {
		return secretRef{}, false
	}
	s := secretRef{mm[1], mm[2], mm[3]}
	if s.version == "" {
		s.version = "latest"
	}
	return s, true
}

// k8s objects of an image app on a K8S target: ServiceAccount (WIF), Deployment, Service when the app
// has a port, ExternalSecret when it has secret env vars. Kubernetes-typed objects list their fields
// alphabetically.
func (m *model) k8s(d *deploy) []Map {
	name, labels := d.app.name, m.k8sLabels(d)
	selector := M("app.kubernetes.io/name", name)
	cl, _ := m.cfg.cluster(d.tgt.cluster)
	// without a secret store, secret-backed env vars are omitted (values still validated)
	env, secrets := List{}, List{}
	for _, e := range d.env {
		s, isSecret := parseSecret(e.v)
		switch {
		case !isSecret:
			env = append(env, M("name", e.k, "value", e.v))
		case cl.SecretStore:
			env = append(env, M("name", e.k, "valueFrom", M("secretKeyRef", M("key", e.k, "name", name+"-secrets"))))
			// external-secrets.io; one ClusterSecretStore per GCP project: gcp-<project>
			secrets = append(secrets, M("secretKey", e.k, "remoteRef", M("key", s.secret, "version", s.version),
				"sourceRef", M("storeRef", M("kind", "ClusterSecretStore", "name", "gcp-"+s.project))))
		}
	}
	container := Map{}
	if len(env) > 0 {
		container = append(container, KV{K: "env", V: env})
	}
	container = append(container, M("image", d.app.image, "name", name)...)
	if d.port != nil {
		container = append(container, KV{K: "ports", V: List{M("containerPort", *d.port, "name", "http")}})
	}
	container = append(container, KV{K: "resources", V: M("limits", M("memory", d.memory), "requests", M("cpu", d.cpu, "memory", d.memory))})

	objs := []Map{
		M("apiVersion", "v1", "kind", "ServiceAccount", "metadata", M(
			"annotations", M("iam.gke.io/gcp-service-account", d.tgt.wifEmail),
			"labels", labels, "name", name, "namespace", d.tgt.namespace)),
		M("apiVersion", "apps/v1", "kind", "Deployment",
			"metadata", M("labels", labels, "name", name, "namespace", d.tgt.namespace),
			"spec", M(
				"replicas", d.replicas,
				"selector", M("matchLabels", selector),
				"template", M(
					"metadata", M("labels", labels),
					"spec", M("containers", List{container}, "serviceAccountName", name)))),
	}
	if d.port != nil {
		objs = append(objs, M("apiVersion", "v1", "kind", "Service",
			"metadata", M("labels", labels, "name", name, "namespace", d.tgt.namespace),
			"spec", M("ports", List{M("name", "http", "port", 80, "targetPort", "http")}, "selector", selector)))
	}
	if len(secrets) > 0 {
		objs = append(objs, M("apiVersion", "external-secrets.io/v1", "kind", "ExternalSecret",
			"metadata", M("name", name+"-secrets", "namespace", d.tgt.namespace, "labels", labels),
			"spec", M("refreshInterval", "1h", "target", M("name", name+"-secrets"), "data", secrets)))
	}
	return objs
}

// cloudRun is the Knative Service of an image app on a CLOUD_RUN target (ADR 0024): the gha workflow's
// whole input. Cloud Run YAML rules:
//   - label keys: lowercase letters, digits, _ and - only, so these differ from the K8S labels; the
//     region is the system label cloud.googleapis.com/location
//   - a secret in the target's own gcpProject is referenced by its name; one from another project needs
//     an alias declared in the run.googleapis.com/secrets annotation (<alias>:projects/<p>/secrets/<s>,
//     comma-joined, sorted, deduped). Alias = "<p>-<s>" lowercased with _ replaced by -.
//   - under 1 vCPU (cpu "<n>m", n < 1000) Cloud Run requires containerConcurrency 1
func (m *model) cloudRun(d *deploy) Map {
	labels := func(location bool) Map {
		l := M("app", d.app.name)
		if location {
			l = append(l, KV{K: "cloud.googleapis.com/location", V: d.tgt.region})
		}
		return append(l, M("managed-by", "cue-platform", "system", m.sys, "stage", d.stage, "target", d.target,
			"environment", d.tgt.environment, "environment-type", d.tgt.environmentType, "billing", d.tgt.billing)...)
	}
	env := append([]kv{}, d.env...)
	sort.Slice(env, func(i, j int) bool { return env[i].k < env[j].k })
	envList := List{}
	aliases := map[string]string{}
	for _, e := range env {
		s, isSecret := parseSecret(e.v)
		if !isSecret {
			envList = append(envList, M("name", e.k, "value", e.v))
			continue
		}
		ref := s.secret
		if s.project != d.tgt.gcpProject {
			ref = strings.ReplaceAll(strings.ToLower(s.project+"-"+s.secret), "_", "-")
			aliases[ref] = "projects/" + s.project + "/secrets/" + s.secret
		}
		envList = append(envList, M("name", e.k, "valueFrom", M("secretKeyRef", M("name", ref, "key", s.version))))
	}
	annotations := M("autoscaling.knative.dev/minScale", strconv.Itoa(d.replicas))
	if len(aliases) > 0 {
		var names, l []string
		for a := range aliases {
			names = append(names, a)
		}
		for _, a := range sorted(names) {
			l = append(l, a+":"+aliases[a])
		}
		annotations = append(annotations, KV{K: "run.googleapis.com/secrets", V: strings.Join(l, ",")})
	}
	container := M("name", d.app.name, "image", d.app.image, "ports", List{M("containerPort", *d.port)},
		"resources", M("limits", M("cpu", d.cpu, "memory", d.memory)))
	if len(envList) > 0 {
		container = append(container, KV{K: "env", V: envList})
	}
	spec := Map{}
	if n, err := strconv.Atoi(strings.TrimSuffix(d.cpu, "m")); strings.HasSuffix(d.cpu, "m") && err == nil && n < 1000 {
		spec = append(spec, KV{K: "containerConcurrency", V: 1})
	}
	spec = append(spec, KV{K: "containers", V: List{container}})
	return M("apiVersion", "serving.knative.dev/v1", "kind", "Service",
		"metadata", M("name", d.app.name, "namespace", d.tgt.gcpProject, "labels", labels(true)),
		"spec", M("template", M("metadata", M("labels", labels(false), "annotations", annotations), "spec", spec)))
}

// terraform is terraform.yaml of a terraform app on a TERRAFORM target (ADR 0021): the workflow's whole
// input. Promotion pins `module` to <repo>:<tag>@<digest> on the target branch; env is exported as-is.
func (m *model) terraform(d *deploy) Map {
	env := append([]kv{}, d.env...)
	sort.Slice(env, func(i, j int) bool { return env[i].k < env[j].k })
	e := Map{}
	for _, x := range env {
		e = append(e, KV{K: x.k, V: x.v})
	}
	return M("system", m.sys, "app", d.app.name, "stage", d.stage, "target", d.target,
		"environment", d.tgt.environment, "gcpProject", d.tgt.gcpProject, "region", d.tgt.region, "wifEmail", d.tgt.wifEmail,
		"backend", M("bucket", d.tgt.bucket, "prefix", d.prefix), "module", d.app.module, "env", e)
}

// ---- Argo CD (one per region): only argocd deploys get an Application / AppProject destination;
// gha deploys run a workflow (ADR 0020)

func (m *model) argoDeploys() []*deploy {
	var out []*deploy
	for _, d := range m.deploys {
		if d.deployWith == "argocd" {
			out = append(out, d)
		}
	}
	return out
}

func (m *model) region(d *deploy) string {
	c, _ := m.cfg.cluster(d.tgt.cluster)
	return c.Region
}

// argocdParams is the params file of one workload Application, generated in the regional Argo CD by the
// `workloads` ApplicationSet (platform/appsets/workloads.yaml). Keys must match that template; git files
// gen reserves `path` and `values`.
func (m *model) argocdParams(d *deploy) Map {
	c, _ := m.cfg.cluster(d.tgt.cluster)
	return M(
		"appName", m.sys+"-"+d.app.name+"-"+d.target,
		"system", m.sys, "app", d.app.name, "stage", d.stage, "target", d.target,
		"namespace", d.tgt.namespace,
		"branch", "env/"+m.sys+"/"+d.app.name+"/"+d.target,
		"destination", c.ArgocdName,
		"authorizedStage", m.project(d.app.name)+":"+d.target,
	)
}

// argocdProjects is, per region with argocd deploys, the system's AppProject with only that region's
// destinations (synced by the `projects` AppSet). Its roles: one per app with argocd deploys there, so the
// app's promoters, viewers and break-glass see its Applications, read-only (Kargo does all syncing).
func (m *model) argocdProjects() map[string]Map {
	out := map[string]Map{}
	for _, region := range m.cfg.Regions() {
		var dests, apps []string
		policies := map[string][]string{}
		for _, d := range m.argoDeploys() {
			if m.region(d) != region {
				continue
			}
			c, _ := m.cfg.cluster(d.tgt.cluster)
			dests = append(dests, c.ArgocdName+"/"+d.tgt.namespace)
			an := d.app.name
			apps = append(apps, an)
			// exact names: a glob <sys>-<app>-* would also match an app named <app>-<x>
			policies[an] = append(policies[an], "p, proj:"+m.sys+":viewer-"+an+", applications, get, "+m.sys+"/"+m.sys+"-"+an+"-"+d.target+", allow")
		}
		if len(dests) == 0 {
			continue
		}
		destinations, roles := List{}, List{}
		for _, k := range uniqSorted(dests) {
			name, ns, _ := strings.Cut(k, "/")
			destinations = append(destinations, M("name", name, "namespace", ns))
		}
		for _, an := range uniqSorted(apps) {
			roles = append(roles, M("name", "viewer-"+an,
				"description", "read-only view of "+m.sys+"/"+an+" for its promoters and viewers",
				"policies", Strings(sorted(policies[an])),
				// matched against the OIDC email claim (argocd-rbac-cm scopes: [email])
				"groups", Strings(m.owners.appEmails[an])))
		}
		out[region] = M("apiVersion", "argoproj.io/v1alpha1", "kind", "AppProject",
			"metadata", M("name", m.sys, "namespace", "argocd"),
			"spec", M(
				"sourceRepos", List{m.cfg.RepoURL},
				"destinations", destinations,
				"clusterResourceWhitelist", List{M("group", "", "kind", "Namespace")}, // CreateNamespace=true
				"namespaceResourceWhitelist", List{M("group", "*", "kind", "*")},
				"roles", roles))
	}
	return out
}
