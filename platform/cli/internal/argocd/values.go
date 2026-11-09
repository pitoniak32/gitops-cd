package argocd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppSetChart reads platform/appsets/addons/<addon>.yaml .spec.template.spec.sources[0].
func AppSetChart(root, addon string) (repo, chart, version string, valuesObject map[string]any, err error) {
	b, err := os.ReadFile(filepath.Join(root, "platform", "appsets", "addons", addon+".yaml"))
	if err != nil {
		return "", "", "", nil, fmt.Errorf("read appset %s: %w", addon, err)
	}
	var doc struct {
		Spec struct {
			Template struct {
				Spec struct {
					Sources []struct {
						RepoURL        string `yaml:"repoURL"`
						Chart          string `yaml:"chart"`
						TargetRevision string `yaml:"targetRevision"`
						Helm           struct {
							ValuesObject map[string]any `yaml:"valuesObject"`
						} `yaml:"helm"`
					} `yaml:"sources"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return "", "", "", nil, fmt.Errorf("parse appset %s: %w", addon, err)
	}
	s := doc.Spec.Template.Spec.Sources
	if len(s) == 0 {
		return "", "", "", nil, fmt.Errorf("appset %s: no spec.template.spec.sources", addon)
	}
	return s[0].RepoURL, s[0].Chart, s[0].TargetRevision, s[0].Helm.ValuesObject, nil
}

// RegionValues deep-copies obj replacing {{.values.region}} in every string.
func RegionValues(obj map[string]any, region string) map[string]any {
	return replaceAny(obj, region).(map[string]any)
}

func replaceAny(v any, region string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = replaceAny(e, region)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = replaceAny(e, region)
		}
		return out
	case string:
		return strings.ReplaceAll(t, "{{.values.region}}", region)
	default:
		return v
	}
}
