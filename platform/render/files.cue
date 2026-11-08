package render

import (
	"encoding/json"
	"encoding/yaml"
	"list"
	"strings"
)

// Everything render.sh writes, keyed by path under rendered/.
// render.sh first wipes this capability's paths: apps/<cap>/, argocd/<region>/<cap>.yaml, kargo/<cap>.yaml, pipelines/<cap>.json, codeowners/<cap>
files: {
	for _, m in manifests {
		"apps/\(_cap)/\(m.app)/\(m.stage)/\(m.target)/manifests.yaml": yaml.MarshalStream([for o in m.objects if o.kind != "Deployment" {o}])
		// own single-document file: promotion pins the Freight's image in it with yaml-update, which
		// only addresses a file's first document (ADR 0016)
		for o in m.objects if o.kind == "Deployment" {
			"apps/\(_cap)/\(m.app)/\(m.stage)/\(m.target)/deployment.yaml": yaml.Marshal(o)
		}
	}
	"kargo/\(_cap).yaml": yaml.MarshalStream(kargo)
	for r, objs in argocd {
		"argocd/\(r)/\(_cap).yaml": yaml.MarshalStream(objs)
	}
	"pipelines/\(_cap).json": json.Indent(json.Marshal(pipelines), "", "  ") + "\n"
	// CODEOWNERS fragment; scripts/codeowners.sh splices all capabilities into .github/CODEOWNERS
	"codeowners/\(_cap)": strings.Join(_codeowners, "\n") + "\n"
}

_ghTeams: {
	#in: [...string]
	out: strings.Join([for t in #in {"@\(teams[t].githubTeam)"}], " ")
}
_codeowners: list.Concat([
	["/capabilities/\(_cap)/ \((_ghTeams & {#in: _allOwners}).out)"],
	[for an in list.SortStrings([for an, _ in apps {an}]) {
		"/capabilities/\(_cap)/apps/\(an)/ \((_ghTeams & {#in: owners[an]}).out)"
	}],
])
