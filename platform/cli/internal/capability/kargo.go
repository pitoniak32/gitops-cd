package capability

import (
	"strconv"
	"strings"

	. "github.com/pitoniak32/gitops-cd/platform/cli/internal/emit"
)

// Promotion model (rendered-manifests + Kargo), one Kargo Stage per target (ADR 0012):
//   CI renders every (app, stage, target) to <repo>@rendered (branch, ADR 0022): rendered/apps/<cap>/<app>/<stage>/<target>/
//   Kargo Warehouse per app watches that path and the app's image repo (SemVer tags); Freight creation
//   is Manual: promoters assemble Freight = (config commit, image digest) in the UI (ADR 0016).
//   Target Stage <target> (in the app's project, ADR 0029) takes Freight from its upstream node (graph.go,
//   ADR 0019) and copies its target folder from that commit onto branch env/<cap>/<app>/<target>, pins the
//   Freight's image in it, then syncs that target's Argo CD app.
//   Argo CD Application <cap>-<app>-<target> tracks that branch, path ".".
//   deployWith: gha targets (ADR 0020) get no Application: after the push the Stage dispatches a GitHub
//   Actions workflow and waits for its run.

const kargoAPI = "kargo.akuity.io/v1alpha1"

// kargo is rendered/kargo/<cap>.yaml: per app (sorted) its project, Warehouse, Stages and RBAC, then
// the capability's project-listing access.
func (m *model) kargo() []any {
	var docs []any
	var names []string
	for _, a := range m.apps {
		names = append(names, a.name)
	}
	for _, an := range sorted(names) {
		a := m.app(an)
		for _, l := range [][]Map{m.kargoCore(a), m.kargoPromoters(a), m.kargoViewers(a)} {
			for _, d := range l {
				docs = append(docs, d)
			}
		}
	}
	for _, d := range m.kargoUsers() {
		docs = append(docs, d)
	}
	return docs
}

func jsonList(s []string) string { return JSON(Strings(s)) }

// a Stage's Freight request from its graph node: the Warehouse directly, or upstream Stages
func freightRequest(app string, n *graphNode) List {
	var sources Map
	switch {
	case n.direct:
		sources = M("direct", true)
	case n.all:
		sources = M("availabilityStrategy", "All", "stages", Strings(n.sources))
	default:
		sources = M("stages", Strings(n.sources))
	}
	return List{M("origin", M("kind", "Warehouse", "name", app), "sources", sources)}
}

// kargoCore is the app's project Namespace, Project, ProjectConfig, Warehouse and Stages.
func (m *model) kargoCore(a *appDef) []Map {
	an, project, nodes := a.name, m.project(a.name), m.graphs[a.name]
	p := m.pipelines[an]
	// auto-promoted: every control-flow Stage (it only means "upstream verified") + targets of
	// autoPromote stages
	var auto []string
	for _, n := range nodes {
		if n.control || p.stage(n.stage).autoPromote {
			auto = append(auto, n.name)
		}
	}
	policies := List{}
	for _, n := range sorted(auto) {
		policies = append(policies, M("stageSelector", M("name", n), "autoPromotionEnabled", true))
	}
	// a terraform module (OCI artifact, ADR 0021) is discovered like an image; never set `platform`
	// here: artifacts have none, so every tag would be filtered out
	image := Map{}
	for _, c := range []string{a.imageConstraint, a.moduleConstraint} {
		if c != "" {
			image = append(image, KV{K: "constraint", V: c})
		}
	}
	image = append(image, M("repoURL", a.artifactRepo(), "imageSelectionStrategy", "SemVer", "discoveryLimit", 10)...)
	docs := []Map{
		// Rendered (not left to Kargo) so Argo CD's dry-run of the namespaced resources below passes on
		// first sync; Kargo adopts a pre-existing namespace carrying this label. kargo-restore finds a
		// capability's projects by example.com/capability.
		M("apiVersion", "v1", "kind", "Namespace", "metadata", M("name", project, "labels", M(
			"kargo.akuity.io/project", "true", "example.com/capability", m.cap, "example.com/app", an))),
		M("apiVersion", kargoAPI, "kind", "Project", "metadata", M("name", project)),
		M("apiVersion", kargoAPI, "kind", "ProjectConfig", "metadata", M("name", project, "namespace", project),
			"spec", M("promotionPolicies", policies)),
		M("apiVersion", kargoAPI, "kind", "Warehouse", "metadata", M("name", an, "namespace", project),
			"spec", M("freightCreationPolicy", "Manual", "subscriptions", List{
				M("git", M("repoURL", m.cfg.RepoURL, "branch", renderedBranch,
					"includePaths", List{"regexp:^" + renderedDir + "/apps/" + m.cap + "/" + an + "/"})),
				M("image", image),
			})),
	}
	annotations := func(stage string) Map {
		return M(
			// read by the break-glass guard (platform/infrastructure/break-glass): who may approve
			// unverified Freight for this Stage (ADR 0013)
			"example.com/break-glass", jsonList(m.owners.breakGlass),
			// Kargo UI: a stage's control-flow and target Stages share its color
			"kargo.akuity.io/color", m.stage(stage).color)
	}
	// control-flow Stages (no promotionTemplate, hub controller): Freight promoted here is verified at once
	for _, n := range nodes {
		if !n.control {
			continue
		}
		docs = append(docs, M("apiVersion", kargoAPI, "kind", "Stage",
			"metadata", M("name", n.name, "namespace", project,
				"labels", M("example.com/capability", m.cap, "example.com/app", an, "example.com/kind", "control-flow", "example.com/stage", n.stage),
				"annotations", annotations(n.stage)),
			"spec", M("requestedFreight", freightRequest(an, n))))
	}
	for _, n := range nodes {
		if n.control {
			continue
		}
		sn, tn := n.stage, n.target
		d := m.deploy(an, tn)
		P := d.tgt.platform
		// platform pre-deploy checks for this target's environment type (ADR 0028)
		checks := m.cfg.PreDeploy[d.tgt.environmentType]
		ann := annotations(sn)
		if len(checks) > 0 {
			var l []string
			for _, c := range checks {
				l = append(l, a.sourceRepo+"/"+c.File+"@"+c.Ref)
			}
			ann = append(ann, KV{K: "example.com/pre-deploy", V: strings.Join(l, ",")})
		}
		v := func(name, value string) Map { return M("name", name, "value", value) }
		// the shared ClusterPromotionTask's vars (promotiontasks.go, ADR 0027); kargo-restore reads
		// repo/branch/image by name
		vars := List{
			v("repo", m.cfg.RepoURL),
			v("src", "./src/"+renderedDir+"/apps/"+m.cap+"/"+an+"/"+sn+"/"+tn+"/"),
			v("branch", "env/"+m.cap+"/"+an+"/"+tn),
			v("image", a.artifactRepo()),
			v("capability", m.cap), v("app", an), v("stage", sn), v("target", tn),
			v("pinFile", P.pinFile), v("pinKey", P.pinKey), v("artifactKey", d.artifact),
		}
		if d.deployWith == "gha" {
			vars = append(vars, v("workflowRepo", d.wfRepo), v("workflowFile", d.wfFile), v("workflowRef", d.wfRef))
		}
		// pre-<n>: gha-check per platform check, dispatched to the app's sourceRepo; workflow vars on the
		// step itself so a gha deploy's Promotion-level workflow vars stay for `promote`
		steps := List{}
		for i, c := range checks {
			steps = append(steps, M("as", "pre-"+strconv.Itoa(i+1), "task", M("kind", "ClusterPromotionTask", "name", "gha-check"),
				"vars", List{v("workflowRepo", a.sourceRepo), v("workflowFile", c.File), v("workflowRef", c.Ref), v("checkTimeout", c.Timeout)}))
		}
		steps = append(steps, M("as", "promote", "task", M("kind", "ClusterPromotionTask", "name", "promote-"+d.deployWith)))
		spec := Map{}
		// the region's Kargo controller promotes it (control-flow Stages and Warehouses stay on the hub
		// controller); terraform and Cloud Run targets touch no cluster: hub controller too
		if P.name == "K8S" {
			spec = append(spec, KV{K: "shard", V: m.region(d)})
		}
		spec = append(spec, M("requestedFreight", freightRequest(an, n), "promotionTemplate", M("spec", M("vars", vars, "steps", steps)))...)
		docs = append(docs, M("apiVersion", kargoAPI, "kind", "Stage",
			"metadata", M("name", tn, "namespace", project,
				"labels", M("example.com/capability", m.cap, "example.com/app", an, "example.com/kind", "target", "example.com/stage", sn,
					"example.com/target", tn, "example.com/deploy-with", d.deployWith, "example.com/platform", P.label),
				"annotations", ann),
			"spec", spec))
	}
	return docs
}

func rbacMeta(name, namespace string, claims []string) Map {
	ann := M("rbac.kargo.akuity.io/managed", "true")
	if claims != nil {
		ann = append(ann, KV{K: "rbac.kargo.akuity.io/claims", V: JSON(M("email", Strings(claims)))})
	}
	return M("name", name, "namespace", namespace, "labels", M("app.kubernetes.io/managed-by", "gitops-platform"), "annotations", ann)
}

func saSubject(name, namespace string) List {
	return List{M("kind", "ServiceAccount", "name", name, "namespace", namespace)}
}

// kargoPromoters is the promote RBAC (ADR 0023, 0029): in the app's project, one Kargo ServiceAccount
// promote-<stage> per stage, mapped from the OIDC email claim to that stage's promoters + break-glass,
// allowed to promote (and manually approve Freight for) the Kargo Stages of that capability stage. Only
// stages with Kargo Stages: an empty resourceNames would mean *every* stage.
func (m *model) kargoPromoters(a *appDef) []Map {
	project := m.project(a.name)
	var stages []string
	for sn := range m.owners.promoters[a.name] {
		stages = append(stages, sn)
	}
	var docs []Map
	for _, sn := range sorted(stages) {
		var names []string
		for _, n := range m.graphs[a.name] {
			if has(n.stages, sn) {
				names = append(names, n.name)
			}
		}
		if len(names) == 0 {
			continue
		}
		name := "promote-" + sn
		docs = append(docs,
			M("apiVersion", "v1", "kind", "ServiceAccount",
				"metadata", rbacMeta(name, project, uniqSorted(m.owners.promoters[a.name][sn], m.owners.breakGlass))),
			M("apiVersion", "rbac.authorization.k8s.io/v1", "kind", "Role", "metadata", rbacMeta(name, project, nil), "rules", List{
				M("apiGroups", List{"kargo.akuity.io"}, "resources", List{"stages"}, "resourceNames", Strings(sorted(names)), "verbs", List{"promote"}),
				// the promote API creates the Promotion as the user; which stages is limited above
				M("apiGroups", List{"kargo.akuity.io"}, "resources", List{"promotions"}, "verbs", List{"create", "get", "list", "watch"}),
				// Freight assembly (Warehouses are Manual, ADR 0016). Names are content hashes, so this
				// can't name a Warehouse; the project holds only this app's (ADR 0029).
				M("apiGroups", List{"kargo.akuity.io"}, "resources", List{"freights"}, "verbs", List{"create"}),
			}),
			M("apiVersion", "rbac.authorization.k8s.io/v1", "kind", "RoleBinding", "metadata", rbacMeta(name, project, nil),
				"roleRef", M("apiGroup", "rbac.authorization.k8s.io", "kind", "Role", "name", name),
				"subjects", saSubject(name, project)),
		)
	}
	return docs
}

// kargoViewers is read access to the app's project for its promoters, viewers and break-glass: Kargo's
// own per-project kargo-viewer Role.
func (m *model) kargoViewers(a *appDef) []Map {
	project := m.project(a.name)
	return []Map{
		M("apiVersion", "v1", "kind", "ServiceAccount", "metadata", rbacMeta("viewers", project, m.owners.appEmails[a.name])),
		M("apiVersion", "rbac.authorization.k8s.io/v1", "kind", "RoleBinding", "metadata", rbacMeta("viewers", project, nil),
			"roleRef", M("apiGroup", "rbac.authorization.k8s.io", "kind", "Role", "name", "kargo-viewer"),
			"subjects", saSubject("viewers", project)),
	}
}

// kargoUsers: listing projects needs cluster-scoped read (Kargo's kargo-user ClusterRole). Promoters and
// viewers get it via a global ServiceAccount (api.oidc.globalServiceAccounts: kargo-shared-resources).
// They see other projects' names; contents only where they promote or view.
func (m *model) kargoUsers() []Map {
	const ns = "kargo-shared-resources"
	name := "users-" + m.cap
	managed := M("app.kubernetes.io/managed-by", "gitops-platform")
	return []Map{
		M("apiVersion", "v1", "kind", "ServiceAccount", "metadata", rbacMeta(name, ns, m.owners.allEmails)),
		M("apiVersion", "rbac.authorization.k8s.io/v1", "kind", "ClusterRoleBinding",
			"metadata", M("name", "kargo-"+name, "labels", managed),
			"roleRef", M("apiGroup", "rbac.authorization.k8s.io", "kind", "ClusterRole", "name", "kargo-user"),
			"subjects", saSubject(name, ns)),
	}
}
