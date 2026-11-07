package render

import (
	"encoding/json"
	"encoding/yaml"
	"list"
	"strings"
)

// Everything render.sh writes, keyed by path under rendered/.
// render.sh first wipes this capability's paths: apps/<cap>/, {argocd,kargo}/<cap>.yaml, pipelines/<cap>.json, codeowners/<cap>
files: {
	for _, m in manifests {
		"apps/\(_cap)/\(m.app)/\(m.stage)/\(m.target)/manifests.yaml": yaml.MarshalStream(m.objects)
	}
	"kargo/\(_cap).yaml":     yaml.MarshalStream(kargo)
	"argocd/\(_cap).yaml":    yaml.MarshalStream(argocd)
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
