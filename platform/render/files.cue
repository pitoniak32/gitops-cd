package render

import (
	"encoding/json"
	"encoding/yaml"
	"list"
	"strings"
)

// Everything render.sh writes, keyed by path under rendered/.
// render.sh first wipes this capability's paths: apps/<cap>/, argocd/{projects/<region>/<cap>.yaml,deploys/<region>/<cap>/}, kargo/<cap>.yaml, pipelines/<cap>.json, codeowners/<cap>
files: {
	for _, m in manifests {
		"apps/\(_cap)/\(m.app)/\(m.stage)/\(m.target)/manifests.yaml": yaml.MarshalStream([for o in m.objects if o.kind != "Deployment" {o}])
		// own single-document file: promotion pins the Freight's image in it with yaml-update, which
		// only addresses a file's first document (ADR 0016)
		for o in m.objects if o.kind == "Deployment" {
			"apps/\(_cap)/\(m.app)/\(m.stage)/\(m.target)/deployment.yaml": yaml.Marshal(o)
		}
	}
	for p, f in terraformFiles {(p): yaml.Marshal(f)}
	"kargo/\(_cap).yaml": yaml.MarshalStream(kargo)
	for r, p in argocdProjects {
		"argocd/projects/\(r)/\(_cap).yaml": yaml.Marshal(p)
	}
	for k, d in argocdDeploys {
		"argocd/deploys/\(k).json": json.Indent(json.Marshal(d), "", "  ") + "\n"
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
		"/capabilities/\(_cap)/apps/\(an)/ \((_ghTeams & {#in: codeOwners[an]}).out)"
	}],
])
