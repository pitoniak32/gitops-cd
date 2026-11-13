package system

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// schema describes what one YAML value may be: the only definition of the input files, enforced by
// lint and render.
type schema struct {
	kind string // map | dict | list | str | int | bool

	fields []field // map: the allowed keys; anything else is "field not allowed"

	key    *regexp.Regexp // dict: key pattern
	keyMsg string         //       message for a key that doesn't match
	elem   *schema        // dict, list: the values

	re       *regexp.Regexp    // str: must match
	enum     []string          // str: or be one of
	valid    func(string) bool // str: or satisfy
	min, max int               // int: bounds (max 0 = none)
	// message for a value of the wrong type, pattern, enum or range (default: a generic one)
	msg string
}

type field struct {
	name     string
	s        *schema
	required bool
	// message when missing (default "required")
	missing string
	desc    string // one line for users, shown by the schema docs
	// target config fields: the platforms that take it (nil = every platform)
	platforms []string
}

func (s *schema) wrong(def string) string {
	if s.msg != "" {
		return s.msg
	}
	return def
}

// check validates val against s. where: the node errors about val itself point at (its key or item).
func (r reporter) check(s *schema, where, val *yaml.Node, path []string) {
	val = deref(val)
	if where == nil {
		where = val
	}
	switch s.kind {
	case "map":
		if !isMap(val) {
			r.at(where, path, s.wrong("expected a map"))
			return
		}
		seen := map[string]bool{}
		for _, p := range pairs(val) {
			seen[p.k] = true
			var f *field
			for i := range s.fields {
				if s.fields[i].name == p.k {
					f = &s.fields[i]
				}
			}
			if f == nil {
				r.at(p.key, join(path, p.k), "field not allowed")
				continue
			}
			if f.s != nil {
				r.check(f.s, p.key, p.v, join(path, p.k))
			}
		}
		for _, f := range s.fields {
			if f.required && !seen[f.name] {
				msg := f.missing
				if msg == "" {
					msg = "required"
				}
				r.at(nil, join(path, f.name), msg)
			}
		}
	case "dict":
		if !isMap(val) {
			r.at(where, path, s.wrong("expected a map"))
			return
		}
		for _, p := range pairs(val) {
			if s.key != nil && !s.key.MatchString(p.k) {
				r.at(p.key, join(path, p.k), s.keyMsg)
				continue
			}
			if s.elem != nil {
				r.check(s.elem, p.key, p.v, join(path, p.k))
			}
		}
	case "list":
		if !isSeq(val) {
			r.at(where, path, s.wrong("expected a list"))
			return
		}
		for i, it := range items(val) {
			if s.elem != nil {
				r.check(s.elem, it, it, join(path, strconv.Itoa(i)))
			}
		}
	case "str":
		switch {
		case !isStr(val):
			r.at(where, path, s.wrong("expected a string"))
		case s.enum != nil && !has(s.enum, val.Value):
			r.at(where, path, s.wrong("expected one of: "+strings.Join(s.enum, ", ")))
		case s.valid != nil && !s.valid(val.Value):
			r.at(where, path, s.wrong(fmt.Sprintf("invalid value %q", val.Value)))
		case s.re != nil && !s.re.MatchString(val.Value):
			r.at(where, path, s.wrong(fmt.Sprintf("invalid value %q (must match %s)", val.Value, s.re)))
		}
	case "int":
		i, err := strconv.Atoi(valueOf(val))
		switch {
		case !isInt(val) || err != nil:
			r.at(where, path, s.wrong("expected an integer"))
		case i < s.min || (s.max != 0 && i > s.max):
			r.at(where, path, s.wrong(fmt.Sprintf("expected an integer from %d to %d", s.min, s.max)))
		}
	case "bool":
		if !isBool(val) {
			r.at(where, path, s.wrong("expected true or false"))
		}
	}
}

func valueOf(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func re(s string) *regexp.Regexp { return regexp.MustCompile(s) }

func strRe(pattern, msg string) *schema { return &schema{kind: "str", re: re(pattern), msg: msg} }

const nameRe = `^[a-z][a-z0-9-]{0,39}$`

var (
	nameRx   = re(nameRe)
	secretRe = re(`^secret:([a-z][a-z0-9-]{4,28}[a-z0-9])/([A-Za-z0-9_-]+)(?:@([0-9]+|latest))?$`)
	badName  = "invalid name; use lowercase letters, digits and -, starting with a letter (" + nameRe + ")"
	nameS    = strRe(nameRe, "")

	// Platform-owned: emails are matched against the OIDC `email` claim; only this domain is allowed.
	emailS = strRe(`^[a-z0-9._%+-]+@`+regexp.QuoteMeta(emailDomain)+`$`, "expected a lowercase @"+emailDomain+" email")
	emails = &schema{kind: "list", elem: emailS}

	ghRepoRx = `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`
	ociRepoS = strRe(`^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)+$`, "")
	nonEmpty = &schema{kind: "str", re: re(`.`)}

	// Kargo UI named stage colors (kargo.akuity.io/color)
	kargoColors = strings.Fields("red salmon orange amber yellow lime avocado green teal cyan sky blue indigo periwinkle violet purple fuchsia pink rose dragonfruit gray")
	// default color: sorted stage names spread across this palette
	defaultColors = strings.Fields("blue amber violet green red teal pink orange indigo lime")

	// teams/<team>.yml: a GitHub team, used only for CODEOWNERS (code review). UI access is by email
	// (owners.yml), never by IdP groups (ADR 0011).
	teamS = &schema{kind: "map", fields: []field{
		{name: "githubTeam", required: true, desc: "GitHub team, <org>/<team>, used for CODEOWNERS", s: strRe(`^[a-z0-9-]+/[a-z0-9._-]+$`, "expected <org>/<team>, e.g. acme/payments-core")},
	}}

	// Plain value, or a Secret Manager ref: secret:<gcp-project>/<secret>[@<version>]
	envValueS = &schema{kind: "str", valid: func(v string) bool { return !strings.HasPrefix(v, "secret:") || secretRe.MatchString(v) },
		msg: "expected a plain string or secret:<gcp-project>/<secret>[@<version>]"}
	// targets/<t>/environment-variables/<name>.yml, and `env` at any layer: flat KEY: value map
	envVarsS = &schema{kind: "dict", key: re(`^[A-Za-z_][A-Za-z0-9_]*$`), keyMsg: "invalid environment variable name; use letters, digits and _, not starting with a digit", elem: envValueS}

	// Settings that can be set at app, stage, or target level. Most specific wins; env is merged per key.
	layerFields = []field{
		{name: "replicas", desc: "number of replicas, 0 to 100", s: &schema{kind: "int", min: 0, max: 100}},
		{name: "env", desc: "environment variables, KEY: value; a value may be secret:<gcp-project>/<secret>[@<version>]; merged per key", s: envVarsS},
		// targets/<t>/environment-variables/<name>.yml of each deployed target
		{name: "envFrom", desc: "names of the target's environment-variables files to include", s: &schema{kind: "list", elem: nameS}},
		{name: "resources", desc: "container resource requests", s: &schema{kind: "map", fields: []field{
			{name: "cpu", desc: "CPU, whole cores or millicores (100m)", s: strRe(`^[0-9]+m?$`, "")},
			{name: "memory", desc: "memory in Mi or Gi (128Mi)", s: strRe(`^[0-9]+(Mi|Gi)$`, "")},
		}}},
		// argocd (default) syncs the target's Argo CD app; gha dispatches `workflow` (ADR 0020)
		{name: "deployWith", desc: "argocd (default) syncs the target's Argo CD app; gha dispatches a GitHub Actions workflow", s: &schema{kind: "str", enum: []string{"argocd", "gha"}, msg: "expected argocd or gha"}},
		{name: "workflow", desc: "GitHub Actions workflow dispatched when deployWith is gha", s: &schema{kind: "map", fields: []field{
			{name: "repo", desc: "workflow repository, <owner>/<name>", s: strRe(ghRepoRx, "expected <owner>/<name>")},
			{name: "file", desc: "workflow file name, .yml or .yaml", s: strRe(`^[A-Za-z0-9_.-]+\.ya?ml$`, "expected a .yml or .yaml file name")},
			{name: "ref", desc: "git ref the workflow runs on", s: &schema{kind: "str", re: re(`.`), msg: "expected a non-empty git ref"}},
		}}},
	}
	layerS      = &schema{kind: "map", fields: layerFields}
	stageLayerS = &schema{kind: "map", fields: append(append([]field{}, layerFields...),
		// overrides stages/<s>.yml autoPromote for this app only (each app is its own Kargo project, ADR 0029)
		field{name: "autoPromote", desc: "this app only: overrides the stage's autoPromote (default false: promoted by a person)", s: &schema{kind: "bool"}},
		field{name: "targets", desc: "targets this stage deploys to, by target name; settings here override the stage's and app's", s: &schema{kind: "dict", elem: layerS}})}

	// apps/<name>/app.yml
	appS = &schema{kind: "map", fields: append(append([]field{}, layerFields...),
		// exactly one of image (K8s app) or module (terraform app, ADR 0021): a repository, no tag or digest
		field{name: "image", desc: "container image repository, no tag or digest (K8s and Cloud Run apps; set this or module)", s: ociRepoS},
		field{name: "imageConstraint", desc: "SemVer constraint for the image tags to deploy", s: nonEmpty},
		field{name: "module", desc: "terraform module OCI repository, no tag or digest (terraform apps; set this or image)", s: ociRepoS},
		field{name: "moduleConstraint", desc: "SemVer constraint for the module versions to deploy", s: nonEmpty},
		field{name: "port", desc: "container port, 1 to 65535", s: &schema{kind: "int", min: 1, max: 65535}},
		// GitHub repo of the app's code; platform pre-deploy checks are dispatched there (ADR 0028)
		field{name: "sourceRepo", required: true, desc: "GitHub repository of the app's code, <owner>/<name>; pre-deploy checks run there", s: strRe(ghRepoRx, "expected <owner>/<name>")},
		field{name: "stages", desc: "stages the app deploys through, by stage name, each with its targets", s: &schema{kind: "dict", elem: stageLayerS}},
	)}

	backendS = &schema{kind: "map", fields: []field{
		{name: "bucket", required: true, desc: "GCS bucket holding terraform state", s: strRe(`^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$`, "expected a GCS bucket name")},
		{name: "prefix", desc: "GCS object prefix for the state", s: strRe(`^[A-Za-z0-9._/-]+$`, "expected a GCS object prefix")},
	}}
)

// field returns the named field of a map schema, or nil.
func (s *schema) field(name string) *field {
	for i := range s.fields {
		if s.fields[i].name == name {
			return &s.fields[i]
		}
	}
	return nil
}

// stageSchema is stages/<name>.yml; stages: the system's stage names.
func stageSchema(stages []string) *schema {
	return &schema{kind: "map", fields: []field{
		{name: "dependsOn", desc: "stages that must be verified before this one", s: &schema{kind: "list", elem: &schema{kind: "str", enum: stages, msg: allowed("stage", stages)}}},
		// default false: a person promotes; app.yml stages.<s>.autoPromote overrides it per app
		{name: "autoPromote", desc: "promote automatically once upstream stages are verified; default false (a person promotes); apps may override", s: &schema{kind: "bool"}},
		// Kargo UI color for all of this stage's Kargo Stages; default picked per stage
		{name: "color", desc: "Kargo UI color for this stage's Kargo Stages; default picked per stage", s: &schema{kind: "str", enum: kargoColors, msg: allowed("color", kargoColors)}},
	}}
}

// targetSchema is targets/<name>/target.yml: the file is just `config`, whose fields are the common
// ones (platforms nil) and each platform's (platforms: those that take it). validator.target checks
// them per platform.
func targetSchema(cfg Config) *schema {
	gcp := cfg.GcpRegions
	clusters := cfg.clusterNames()
	on := func(field string) []string {
		var out []string
		for _, p := range platforms {
			if has(p.fields, field) {
				out = append(out, p.name)
			}
		}
		sort.Strings(out)
		return out
	}
	return &schema{kind: "map", fields: []field{{name: "config", required: true, desc: "where a stage deploys; reusable across apps", s: &schema{kind: "map", fields: []field{
		{name: "platform", desc: "K8S (default), TERRAFORM or CLOUD_RUN", s: &schema{kind: "str", enum: platformNames(""), msg: "expected K8S, TERRAFORM or CLOUD_RUN"}},
		// free name (namespaces, labels); the type drives policy
		{name: "environment", required: true, desc: "environment name, free-form (namespaces, labels)", s: nameS},
		{name: "environmentType", required: true, missing: "expected dev, npr or prd", desc: "dev, npr or prd; drives policy (replicas, pre-deploy checks, cluster choice)", s: &schema{kind: "str", enum: EnvironmentTypes, msg: "expected dev, npr or prd"}},
		{name: "auth", required: true, desc: "workload identity used to deploy", s: &schema{kind: "map", fields: []field{
			{name: "wifEmail", required: true, desc: "service account email of the workload identity", s: strRe(`^[a-z0-9-]+@[a-z0-9-]+\.iam\.gserviceaccount\.com$`, "")},
		}}},
		{name: "billing", required: true, desc: "billing label for the target's cost", s: strRe(`^[a-z0-9-]+$`, "")},
		{name: "cluster", platforms: on("cluster"), desc: "name of the platform's cluster to deploy to; must accept the environment type", s: &schema{kind: "str", enum: clusters, msg: allowed("cluster", clusters)}},
		{name: "namespace", platforms: on("namespace"), desc: "Kubernetes namespace", s: strRe(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`, "")},
		{name: "region", platforms: on("region"), desc: "GCP region", s: &schema{kind: "str", enum: gcp, msg: allowed("region", gcp)}},
		{name: "gcpProject", platforms: on("gcpProject"), desc: "GCP project id", s: strRe(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`, "expected a GCP project id")},
		{name: "backend", platforms: on("backend"), desc: "terraform state backend", s: backendS},
	}}}}}
}

// ownersSchema is systems/<sys>/owners.yml; teams: names in teams/, stages: the system's.
func ownersSchema(teams, stages []string) *schema {
	teamsS := &schema{kind: "list", elem: &schema{kind: "str", enum: teams, msg: allowed("team", teams)}}
	promoters := &schema{kind: "dict", elem: emails}
	return &schema{kind: "map", fields: []field{
		{name: "codeOwners", required: true, desc: "teams in teams/ that approve code changes (CODEOWNERS)", s: teamsS},
		{name: "promoters", required: true, desc: "per stage name (a stage of this system), emails that may promote into it", s: promoters},
		{name: "viewers", desc: "emails with read-only access in Kargo and Argo CD", s: emails},
		{name: "breakGlass", desc: "emails that may approve Freight that skipped upstream verification", s: emails},
		{name: "apps", desc: "per app name (an app of this system) overrides; each field set replaces the system's", s: &schema{kind: "dict", elem: &schema{kind: "map", fields: []field{
			{name: "codeOwners", desc: "teams in teams/ that approve this app's code changes", s: teamsS},
			{name: "promoters", desc: "per stage name, emails that may promote this app into it; replaces the system's for that stage", s: promoters},
			{name: "viewers", desc: "emails with read-only access to this app", s: emails},
		}}}},
	}}
}
