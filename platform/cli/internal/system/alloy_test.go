package system

import (
	"os"
	"regexp"
	"testing"
)

// Alloy parses Kargo Promotion events' target names into region / environment_type / platform labels
// (platform/infrastructure/alloy/values.yaml, ADR 0031). Its regex must follow the naming rule (ADR 0025):
// every environment type and platform suffix, a single [a-z0-9]+ qualifier.
func TestAlloyTargetRegexFollowsNaming(t *testing.T) {
	b, err := os.ReadFile("../../../infrastructure/alloy/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`expression = "(\^\(\?P<region>[^"]+)"`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no target-name expression (with <region>) in alloy values.yaml")
	}
	re, err := regexp.Compile(string(m[1]))
	if err != nil {
		t.Fatalf("alloy target regex: %v", err)
	}
	for _, region := range []string{"us-east4", "dc1", "us-central1"} {
		for _, et := range EnvironmentTypes {
			for _, p := range platforms {
				for _, name := range []string{region + "-" + et + "-" + p.suffix, region + "-" + et + "-" + p.suffix + "-perf2"} {
					g := re.FindStringSubmatch(name)
					if g == nil {
						t.Errorf("%s: not parsed", name)
						continue
					}
					got := map[string]string{}
					for i, n := range re.SubexpNames() {
						if n != "" {
							got[n] = g[i]
						}
					}
					if got["region"] != region || got["environment_type"] != et || got["platform"] != p.suffix {
						t.Errorf("%s: parsed %v", name, got)
					}
				}
			}
		}
	}
	for _, bad := range []string{"us-east4-dev-k8s-a-b", "us-east4-qa-k8s", "us-east4-dev-vm"} {
		if re.MatchString(bad) {
			t.Errorf("%s: parsed, but it breaks the naming rule", bad)
		}
	}
}
