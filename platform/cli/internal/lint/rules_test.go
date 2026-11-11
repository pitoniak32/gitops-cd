package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
	"gopkg.in/yaml.v3"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, []byte(s), 0o644))
}

func cp(t *testing.T, src, dst string) {
	t.Helper()
	must(t, filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	}))
}

func matches(t *testing.T, got, re string) bool {
	t.Helper()
	return regexp.MustCompile("(?m)" + re).MatchString(got)
}

const payments = "capabilities/payments"

// test.sh: "lowercase env ok"
// env keys: any C identifier (some apps need lowercase, terraform TF_VAR_<name>)
func TestLowercaseEnv(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	must(t, yamledit.Set(filepath.Join(r, payments, "apps/my-app/app.yml"), []string{"env", "TF_VAR_lower_ok"}, "x"))
	if got := lintOut(t, r); got != "" {
		t.Errorf("lowercase env key must lint clean:\n%s", got)
	}
}

// test.sh: "no spurious lines: <expr>"
// A forbidden field is reported by its "not allowed" line only: first line, no spurious pattern lines
func TestForbiddenFieldNoSpuriousLines(t *testing.T) {
	t.Parallel()
	const tg = payments + "/targets/"
	for _, c := range []struct {
		name, file string
		edit       edit
		first      string
	}{
		{`.config.gcpProject = "acme-payments-dev"`, tg + "us-east4-dev-k8s/target.yml", setTo("acme-payments-dev", "config", "gcpProject"), `gcpProject not allowed on a K8S target`},
		{`.config.region = "us-east4"`, tg + "us-east4-dev-k8s/target.yml", setTo("us-east4", "config", "region"), `region not allowed on a K8S target`},
		{`.config.cluster = "us-east4"`, tg + "us-east4-npr-cr/target.yml", setTo("us-east4", "config", "cluster"), `cluster not allowed on a CLOUD_RUN target`},
		{`.config.namespace = "payments"`, tg + "us-east4-npr-cr/target.yml", setTo("payments", "config", "namespace"), `namespace not allowed on a CLOUD_RUN target`},
		{`.config.cluster = "us-east4"`, tg + "us-east4-dev-tf/target.yml", setTo("us-east4", "config", "cluster"), `cluster not allowed on a TERRAFORM target`},
		{`.config.namespace = "payments"`, tg + "us-east4-dev-tf/target.yml", setTo("payments", "config", "namespace"), `namespace not allowed on a TERRAFORM target`},
	} {
		t.Run(c.name+" "+c.file, func(t *testing.T) {
			t.Parallel()
			r := fixture(t)
			c.edit(t, filepath.Join(r, c.file))
			got := lintOut(t, r)
			if first, _, _ := strings.Cut(got, "\n"); !regexp.MustCompile(c.first).MatchString(first) {
				t.Errorf("first lint line wrong: %s\nwant: %s\ngot:\n%s", c.name, c.first, got)
			}
			if matches(t, got, `expected a GCP project id|unknown region|unknown cluster`) {
				t.Errorf("spurious pattern line: %s\ngot:\n%s", c.name, got)
			}
		})
	}
}

// test.sh: "legacy app owners ok"
// Leftover apps/<a>/owners.yml (pre-ADR 0023 layout) fails loudly
func TestLegacyAppOwners(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	write(t, filepath.Join(r, payments, "apps/worker/owners.yml"), "owners: [team1]\n")
	got := lintOut(t, r)
	if !matches(t, got, `apps/worker/owners\.yml:[0-9]+:[0-9]+: .*move into capabilities/payments/owners\.yml apps\.worker`) {
		t.Errorf("legacy app owners.yml not rejected:\n%s", got)
	}
}

// test.sh: "bad name ok: <d>"
// Invalid app/stage/target directory names fail lint (they become k8s/Kargo names)
func TestInvalidDirNames(t *testing.T) {
	t.Parallel()
	for _, d := range []string{"apps/worker", "stages/perf", "targets/dc1-dev-k8s"} {
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			r := fixture(t)
			c := filepath.Join(r, payments)
			dir := filepath.Dir(d)
			if err := os.Rename(filepath.Join(c, d), filepath.Join(c, dir, "Bad_Name")); err != nil {
				must(t, os.Rename(filepath.Join(c, d+".yml"), filepath.Join(c, dir, "Bad_Name.yml")))
			}
			got := lintOut(t, r)
			if !matches(t, got, `^capabilities/payments/[a-z]+/Bad_Name(/app|/target)?\.yml:[0-9]+:[0-9]+: .*invalid name`) {
				t.Errorf("bad name not rejected for %s:\n%s", d, got)
			}
		})
	}
}

// test.sh: "target/stage name collision ok", "target <stage>-done collision ok"
func TestTargetStageNameCollision(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	cp(t, filepath.Join(r, payments, "targets/dc1-dev-k8s"), filepath.Join(r, payments, "targets/dev"))
	got := lintOut(t, r)
	if !matches(t, got, `^capabilities/payments/targets/dev/target\.yml:[0-9]+:[0-9]+: .*also a stage name`) {
		t.Errorf("target/stage name collision not rejected:\n%s", got)
	}
}

// ...nor like <stage>-done (fan-out control-flow Stage <stage>-done, ADR 0019)
func TestTargetDoneCollision(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	cp(t, filepath.Join(r, payments, "targets/dc1-dev-k8s"), filepath.Join(r, payments, "targets/dev-done"))
	got := lintOut(t, r)
	if !matches(t, got, `^capabilities/payments/targets/dev-done/target\.yml:[0-9]+:[0-9]+: .*target name dev-done is <stage>-done; Kargo control-flow Stage name dev-done would collide`) {
		t.Errorf("target named <stage>-done not rejected:\n%s", got)
	}
}

// test.sh: "target layout ok"
// Target layout: old targets/<t>.yml and bundles without target.yml fail lint
func TestTargetLayout(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	tg := filepath.Join(r, payments, "targets")
	b, err := os.ReadFile(filepath.Join(tg, "dc1-dev-k8s/target.yml"))
	must(t, err)
	write(t, filepath.Join(tg, "old.yml"), string(b))
	write(t, filepath.Join(tg, "ghost/environment-variables/x.yml"), "A: b\n")
	got := lintOut(t, r)
	if !matches(t, got, `^capabilities/payments/targets/old\.yml:1:1: .*move to targets/old/target\.yml`) {
		t.Errorf("legacy target file not rejected:\n%s", got)
	}
	if !matches(t, got, `^capabilities/payments/targets/ghost/environment-variables/x\.yml:[0-9]+:[0-9]+: .*without targets/ghost/target\.yml`) {
		t.Errorf("bundle without target not rejected:\n%s", got)
	}
}

// test.sh: "envFrom clash ok"
// envFrom KEY clash: most specific layer wins; same layer: equal ok, different = error
func TestEnvFromClash(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	c := filepath.Join(r, payments)
	write(t, filepath.Join(c, "targets/us-east4-dev-k8s/environment-variables/flags.yml"), "LD_RELAY_URL: https://other\nLD_SDK_KEY: secret:acme-dev-us/launchdarkly-sdk-key\n")
	app := filepath.Join(c, "apps/my-app/app.yml")
	envFrom := []string{"stages", "dev", "targets", "us-east4-dev-k8s", "envFrom"}
	must(t, yamledit.Set(app, envFrom, []string{"flags"}))
	o := t.TempDir()
	if _, err := render.Capability(r, "payments", o); err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(strings.NewReader(readFile(t, filepath.Join(o, "apps/payments/my-app/dev/us-east4-dev-k8s/deployment.yaml"))))
	v := ""
	for {
		var d struct {
			Kind string `yaml:"kind"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Env []struct{ Name, Value string } `yaml:"env"`
						} `yaml:"containers"`
					} `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		}
		if err := dec.Decode(&d); err != nil {
			break
		}
		if d.Kind != "Deployment" {
			continue
		}
		for _, e := range d.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "LD_RELAY_URL" {
				v = e.Value
			}
		}
	}
	if v != "https://other" {
		t.Errorf("target-level envFrom must win: LD_RELAY_URL=%s", v)
	}
	must(t, yamledit.Set(app, envFrom, []string{"flags", "launchdarkly"}))
	got := lintOut(t, r)
	if !matches(t, got, `^capabilities/payments/apps/my-app/app\.yml:[0-9]+:[0-9]+: targets\.us-east4-dev-k8s\.envFrom: .*LD_RELAY_URL differs between environment-variables/\{flags,launchdarkly\}\.yml \(both in target-level`) {
		t.Errorf("same-layer clash not rejected:\n%s", got)
	}
	if strings.Contains(got, "LD_SDK_KEY") {
		t.Errorf("equal values must not clash:\n%s", got)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

// test.sh: "deployWith resolution ok"
// deployWith/workflow resolve per field, target > stage > app > default
func TestDeployWithResolution(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	c := filepath.Join(r, payments, "apps/my-app/app.yml")
	must(t, yamledit.Set(c, []string{"workflow", "ref"}, "release"))
	must(t, yamledit.Set(c, []string{"stages", "dev", "deployWith"}, "gha"))
	must(t, yamledit.Set(c, []string{"stages", "dev", "targets", "dc1-dev-k8s"}, map[string]any{"deployWith": "argocd", "workflow": map[string]any{"file": "x.yml"}}))
	must(t, yamledit.Set(c, []string{"stages", "dev", "targets", "us-east4-dev-k8s", "workflow", "repo"}, "acme/deploys"))
	if got := lintOut(t, r); got != "" {
		t.Fatalf("app-level workflow with argocd targets must lint clean:\n%s", got)
	}
	o := t.TempDir()
	if _, err := render.Capability(r, "payments", o); err != nil {
		t.Fatal(err)
	}
	var pl map[string]any
	must(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(o, "pipelines/payments.json"))), &pl))
	pt := func(stage, target string) string {
		app, _ := pl["my-app"].(map[string]any)
		st, _ := app["stages"].(map[string]any)[stage].(map[string]any)
		tg, _ := st["targets"].(map[string]any)[target].(map[string]any)
		return fmt.Sprint(tg["deployWith"])
	}
	got := pt("dev", "us-east4-dev-k8s") + " " + pt("dev", "dc1-dev-k8s") + " " + pt("prod", "us-east4-prd-k8s")
	if got != "gha argocd argocd" {
		t.Errorf("deployWith resolution: %s", got)
	}
}

// test.sh: "cluster types ok"
// A cluster that narrows its environmentTypes rejects other types, naming what it accepts
// (copy of the renderer into the root: clusters.cue is platform-owned, so the case can't be a table edit)
func TestClusterTypes(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	renderSrc := filepath.Join("..", "..", "..", "render")
	cp(t, filepath.Join("..", "..", "..", "cue.mod"), filepath.Join(r, "platform/cue.mod"))
	ents, err := os.ReadDir(renderSrc)
	must(t, err)
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".cue") {
			write(t, filepath.Join(r, "platform/render", e.Name()), readFile(t, filepath.Join(renderSrc, e.Name())))
		}
	}
	cf := filepath.Join(r, "platform/render/clusters.cue")
	old := `dc1: {region: "dc1", kind: "kind", environmentTypes: ["dev", "npr", "prd"]`
	src := readFile(t, cf)
	write(t, cf, strings.Replace(src, old, `dc1: {region: "dc1", kind: "kind", environmentTypes: ["dev"]`, 1))
	if !strings.Contains(readFile(t, cf), `environmentTypes: ["dev"]`) {
		t.Fatal("cluster-types case: edit did not narrow dc1")
	}
	got := lintOut(t, r)
	if !matches(t, got, `^capabilities/payments/targets/dc1-prd-k8s/target\.yml:[0-9]+:[0-9]+: config\.environmentType: cluster dc1 accepts environment types: dev$`) {
		t.Errorf("cluster environment-type check failed:\n%s", got)
	}
}

// test.sh: "kargo stage name length ok"
// Kargo Stage names must fit a label value (63): Kargo 1.12 labels every PromotionRequest with the
// unshortened Stage name, so a longer name creates the Stage but fails every promotion into it.
// Stage names carry no app prefix (one project per app, ADR 0029).
func TestKargoStageNameLength(t *testing.T) {
	t.Parallel()
	longTgt := "us-east4-dev-k8s-" + strings.Repeat("a", 47)         // 64
	longA, longB := strings.Repeat("a", 30), strings.Repeat("b", 30) // join node <a>-<b>-done: 66
	r := fixture(t)
	c := filepath.Join(r, payments)
	cp(t, filepath.Join(c, "targets/us-east4-dev-k8s"), filepath.Join(c, "targets", longTgt))
	must(t, yamledit.Set(filepath.Join(c, "targets", longTgt, "target.yml"), []string{"config", "namespace"}, "payments-long"))
	must(t, yamledit.Set(filepath.Join(c, "apps/web/app.yml"), []string{"stages", "dev", "targets"}, map[string]any{longTgt: map[string]any{}}))
	got := lintOut(t, r)
	if !matches(t, got, fmt.Sprintf(`^capabilities/payments/apps/web/app\.yml:[0-9]+:[0-9]+: .*Kargo Stage name %s is 64 characters \(max 63\): shorten the target name$`, longTgt)) {
		t.Errorf("long target Stage name not rejected:\n%s", got)
	}

	// control flow: stages c and d both depend on <a> and <b> (2 targets each side): one shared join
	// Stage <a>-<b>-done, reported on <a>
	r = fixture(t)
	c = filepath.Join(r, payments)
	tg := func(n string) map[string]any { return map[string]any{"targets": map[string]any{n: map[string]any{}}} }
	for st, deps := range map[string]string{longA: "[]", longB: "[]", "c": "[" + longA + ", " + longB + "]", "d": "[" + longA + ", " + longB + "]"} {
		write(t, filepath.Join(c, "stages", st+".yml"), "dependsOn: "+deps+"\n")
		must(t, yamledit.Set(filepath.Join(c, "owners.yml"), []string{"promoters", st}, []string{"doug@example.com"}))
	}
	must(t, yamledit.Set(filepath.Join(c, "apps/web/app.yml"), []string{"stages"}, map[string]any{
		longA: tg("us-east4-dev-k8s"), longB: tg("dc1-dev-k8s"), "c": tg("us-east4-npr-k8s"), "d": tg("dc1-npr-k8s"),
	}))
	got = lintOut(t, r)
	if !matches(t, got, fmt.Sprintf(`^capabilities/payments/apps/web/app\.yml:[0-9]+:[0-9]+: .*Kargo Stage name %s-%s-done is 66 characters \(max 63\): shorten the stage names$`, longA, longB)) {
		t.Errorf("long control-flow Stage name not rejected:\n%s", got)
	}
}

// The Kargo project kargo-<cap>-<app> is also a namespace: 63 characters max (ADR 0029).
func TestProjectNameTooLong(t *testing.T) {
	t.Parallel()
	app := "w" + strings.Repeat("x", 39) // 40, the name max
	r := fixture(t)
	c := filepath.Join(r, "capabilities/search")
	must(t, os.Rename(filepath.Join(c, "apps/api"), filepath.Join(c, "apps", app)))
	capName := "searchsearchsearc" // kargo- (6) + 17 + - (1) + 40 = 64
	must(t, os.Rename(c, filepath.Join(r, "capabilities", capName)))
	d, err := All(r, []string{capName})
	var ls []string
	for _, x := range d {
		ls = append(ls, x.String())
	}
	if err != nil {
		ls = append(ls, "error: "+err.Error())
	}
	got := strings.Join(ls, "\n")
	if !matches(t, got, fmt.Sprintf(`^capabilities/%s/apps/%s/app\.yml:1:1: .*Kargo project name kargo-%s-%s is 64 characters \(max 63\): shorten the app name$`, capName, app, capName, app)) {
		t.Errorf("long project name not rejected:\n%s", got)
	}
}

// replaceToken is perl's s/(?<![\w-])from(?![\w-])/to/g (RE2 has no lookaround).
func replaceToken(s, from, to string) string {
	word := func(b byte) bool {
		return b == '-' || b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], from) && (i == 0 || !word(s[i-1])) && (i+len(from) == len(s) || !word(s[i+len(from)])) {
			out.WriteString(to)
			i += len(from)
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// test.sh: "target name ok: <to>"
// Target name must be <region>-<env>-<platform>[-<qualifier>] (ADR 0025); dir renames, so not table edits
func TestTargetNameConvention(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ from, to, want string }{
		{"us-east4-npr-k8s", "dc1-npr-k8s-x", `^capabilities/payments/targets/dc1-npr-k8s-x/target\.yml:[0-9]+:[0-9]+: .*name must be <region>-<env>-<platform>\[-<qualifier>\]: expected us-east4-npr-k8s\[-<qualifier>\]`},
		{"us-east4-npr-k8s", "us-east4-npr-k8sperf", `^capabilities/payments/targets/us-east4-npr-k8sperf/target\.yml:[0-9]+:[0-9]+: .*expected us-east4-npr-k8s\[-<qualifier>\]`},
		{"us-east4-npr-k8s", "us-east4-npr-k8s-", `^capabilities/payments/targets/us-east4-npr-k8s-/target\.yml:[0-9]+:[0-9]+: `},
		{"us-east4-npr-k8s", "us-east4-npr-k8s-blue", "ok"},
		{"us-east4-npr-cr", "us-east4-npr-k8s-x", `^capabilities/payments/targets/us-east4-npr-k8s-x/target\.yml:[0-9]+:[0-9]+: .*name must be <region>-<env>-<platform>\[-<qualifier>\]: expected us-east4-npr-cr\[-<qualifier>\]`},
		{"us-east4-npr-cr", "dc1-npr-cr", `^capabilities/payments/targets/dc1-npr-cr/target\.yml:[0-9]+:[0-9]+: .*expected us-east4-npr-cr\[-<qualifier>\]`},
		{"us-east4-npr-cr", "us-east4-npr-cr-blue", "ok"},
	} {
		t.Run(c.to, func(t *testing.T) {
			t.Parallel()
			r := fixture(t)
			tg := filepath.Join(r, payments, "targets")
			must(t, os.Rename(filepath.Join(tg, c.from), filepath.Join(tg, c.to)))
			apps, err := filepath.Glob(filepath.Join(r, payments, "apps/*/app.yml"))
			must(t, err)
			for _, a := range apps {
				write(t, a, replaceToken(readFile(t, a), c.from, c.to))
			}
			got := lintOut(t, r)
			if c.want == "ok" {
				if got != "" {
					t.Errorf("name %s should pass:\n%s", c.to, got)
				}
				return
			}
			if matches(t, got, `expected K8S, TERRAFORM or CLOUD_RUN|expected K8S or TERRAFORM|empty disjunction|invalid interpolation`) {
				t.Errorf("name %s: noisy output:\n%s", c.to, got)
			}
			if !matches(t, got, c.want) {
				t.Errorf("name %s not rejected as expected:\n%s", c.to, got)
			}
		})
	}
}
