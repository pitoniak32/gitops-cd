package capability

import (
	"fmt"
	"regexp"
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
		{name: "githubTeam", required: true, s: strRe(`^[a-z0-9-]+/[a-z0-9._-]+$`, "expected <org>/<team>, e.g. acme/payments-core")},
	}}

	// Plain value, or a Secret Manager ref: secret:<gcp-project>/<secret>[@<version>]
	envValueS = &schema{kind: "str", valid: func(v string) bool { return !strings.HasPrefix(v, "secret:") || secretRe.MatchString(v) },
		msg: "expected a plain string or secret:<gcp-project>/<secret>[@<version>]"}
	// targets/<t>/environment-variables/<name>.yml, and `env` at any layer: flat KEY: value map
	envVarsS = &schema{kind: "dict", key: re(`^[A-Za-z_][A-Za-z0-9_]*$`), keyMsg: "invalid environment variable name; use letters, digits and _, not starting with a digit", elem: envValueS}

	// Settings that can be set at app, stage, or target level. Most specific wins; env is merged per key.
	layerFields = []field{
		{name: "replicas", s: &schema{kind: "int", min: 0, max: 100}},
		{name: "env", s: envVarsS},
		// targets/<t>/environment-variables/<name>.yml of each deployed target
		{name: "envFrom", s: &schema{kind: "list", elem: nameS}},
		{name: "resources", s: &schema{kind: "map", fields: []field{
			{name: "cpu", s: strRe(`^[0-9]+m?$`, "")},
			{name: "memory", s: strRe(`^[0-9]+(Mi|Gi)$`, "")},
		}}},
		// argocd (default) syncs the target's Argo CD app; gha dispatches `workflow` (ADR 0020)
		{name: "deployWith", s: &schema{kind: "str", enum: []string{"argocd", "gha"}, msg: "expected argocd or gha"}},
		{name: "workflow", s: &schema{kind: "map", fields: []field{
			{name: "repo", s: strRe(ghRepoRx, "expected <owner>/<name>")},
			{name: "file", s: strRe(`^[A-Za-z0-9_.-]+\.ya?ml$`, "expected a .yml or .yaml file name")},
			{name: "ref", s: &schema{kind: "str", re: re(`.`), msg: "expected a non-empty git ref"}},
		}}},
	}
	layerS      = &schema{kind: "map", fields: layerFields}
	stageLayerS = &schema{kind: "map", fields: append(append([]field{}, layerFields...),
		field{name: "targets", s: &schema{kind: "dict", elem: layerS}})}

	// apps/<name>/app.yml
	appS = &schema{kind: "map", fields: append(append([]field{}, layerFields...),
		// exactly one of image (K8s app) or module (terraform app, ADR 0021): a repository, no tag or digest
		field{name: "image", s: ociRepoS},
		field{name: "imageConstraint", s: nonEmpty},
		field{name: "module", s: ociRepoS},
		field{name: "moduleConstraint", s: nonEmpty},
		field{name: "port", s: &schema{kind: "int", min: 1, max: 65535}},
		// GitHub repo of the app's code; platform pre-deploy checks are dispatched there (ADR 0028)
		field{name: "sourceRepo", required: true, s: strRe(ghRepoRx, "expected <owner>/<name>")},
		field{name: "stages", s: &schema{kind: "dict", elem: stageLayerS}},
	)}

	backendS = &schema{kind: "map", fields: []field{
		{name: "bucket", required: true, s: strRe(`^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$`, "expected a GCS bucket name")},
		{name: "prefix", s: strRe(`^[A-Za-z0-9._/-]+$`, "expected a GCS object prefix")},
	}}
)
