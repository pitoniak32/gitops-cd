package lint

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

// lintOut is `gcd lint --root r payments` output: one diagnostic per line. An error that is not a
// diagnostic is returned as text so the expectation fails with it visible.
func lintOut(t *testing.T, root string) string {
	t.Helper()
	d, err := All(root, []string{"payments"})
	var lines []string
	for _, x := range d {
		lines = append(lines, x.String())
	}
	if err != nil {
		lines = append(lines, "error: "+err.Error())
	}
	return strings.Join(lines, "\n")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type edit func(t *testing.T, file string)

func setTo(v any, path ...string) edit {
	return func(t *testing.T, f string) { t.Helper(); must(t, yamledit.Set(f, path, v)) }
}

func delAt(path ...string) edit {
	return func(t *testing.T, f string) { t.Helper(); must(t, yamledit.Del(f, path)) }
}

type m = map[string]any

const (
	ow   = "capabilities/payments/owners.yml"
	tgK8 = "capabilities/payments/targets/us-east4-dev-k8s/target.yml"
	tgTF = "capabilities/payments/targets/us-east4-dev-tf/target.yml"
	tgCR = "capabilities/payments/targets/us-east4-npr-cr/target.yml"
	appW = "capabilities/payments/apps/worker/app.yml"
	appI = "capabilities/payments/apps/infra/app.yml"
)

// lint-cases.tsv: file, yq edit (name), edit, and a regexp one output line must match.
var lintCases = []struct {
	name, file string
	edit       edit
	want       string
}{
	{`.codeOwners = ["nobody"]`, ow, setTo([]string{"nobody"}, "codeOwners"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*unknown team; allowed: team1, team2`},
	{`.apps.worker.codeOwners = []`, ow, setTo([]string{}, "apps", "worker", "codeOwners"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*needs at least one team`},
	{`.apps.worker.codeOwners = ["team1", "team1"]`, ow, setTo([]string{"team1", "team1"}, "apps", "worker", "codeOwners"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*teams must not repeat`},
	{`.promoters.dev = ["doug@evil.com"]`, ow, setTo([]string{"doug@evil.com"}, "promoters", "dev"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: promoters\.dev\.0: .*expected a lowercase @example\.com email`},
	{`.viewers = ["Bob@example.com"]`, ow, setTo([]string{"Bob@example.com"}, "viewers"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: viewers\.0: .*expected a lowercase @example\.com email`},
	{`del(.promoters)`, ow, delAt("promoters"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*needs promoters`},
	{`.promoters = ["doug@example.com"]`, ow, setTo([]string{"doug@example.com"}, "promoters"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*expected a map`},
	{`.owners = .codeOwners | del(.codeOwners)`, ow, func(t *testing.T, f string) {
		t.Helper()
		must(t, yamledit.Rename(f, []string{"codeOwners"}, "owners"))
	}, `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*renamed to codeOwners`},
	{`.promoters.qa = ["doug@example.com"]`, ow, setTo([]string{"doug@example.com"}, "promoters", "qa"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*unknown stage; allowed: dev, perf, prod, staging`},
	{`.apps.nope = {}`, ow, setTo(m{}, "apps", "nope"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*unknown app; allowed: infra, my-app, payments, web, worker`},
	{`del(.promoters.staging)`, ow, delAt("promoters", "staging"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*app my-app uses stage staging: needs promoters\.staging`},
	{`.promoters.dev = []`, ow, setTo([]string{}, "promoters", "dev"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*needs at least one email`},
	{`.apps.worker.promoters.dev = ["doug@example.com", "doug@example.com"]`, ow, setTo([]string{"doug@example.com", "doug@example.com"}, "apps", "worker", "promoters", "dev"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*emails must not repeat`},
	{`.apps.worker.breakGlass = ["carol@example.com"]`, ow, setTo([]string{"carol@example.com"}, "apps", "worker", "breakGlass"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*breakGlass is capability-wide`},
	{`.breakGlass = ["Oncall@example.com"]`, ow, setTo([]string{"Oncall@example.com"}, "breakGlass"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: breakGlass\.0: .*expected a lowercase @example\.com email`},
	{`.breakGlass = ["carol@example.com", "carol@example.com"]`, ow, setTo([]string{"carol@example.com", "carol@example.com"}, "breakGlass"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*emails must not repeat`},
	{`.githubTeam = "Bad Name"`, "teams/team2.yaml", setTo("Bad Name", "githubTeam"), `^teams/team2\.yaml:[0-9]+:[0-9]+:`},
	{`.autoPromote = true`, "capabilities/payments/stages/prod.yml", setTo(true, "autoPromote"), `^capabilities/payments/stages/prod\.yml:[0-9]+:[0-9]+: .*stage prod deploys to prod targets; autoPromote not allowed`},
	{`.idpGroup = "team1"`, "teams/team1.yml", setTo("team1", "idpGroup"), `^teams/team1\.yml:[0-9]+:[0-9]+: .*idpGroup`},
	{`.envFrom = ["statsig"]`, "capabilities/payments/apps/my-app/app.yml", setTo([]string{"statsig"}, "envFrom"), `^capabilities/payments/apps/my-app/app\.yml:[0-9]+:[0-9]+: targets\.[a-z0-9-]+\.envFrom: .*has no environment-variables/statsig\.yml`},
	{`."1BAD" = "x"`, "capabilities/payments/targets/us-east4-dev-k8s/environment-variables/launchdarkly.yml", setTo("x", "1BAD"), `^capabilities/payments/targets/us-east4-dev-k8s/environment-variables/launchdarkly\.yml:[0-9]+:[0-9]+: .*1BAD`},
	{`.LD_SDK_KEY = "secret:BAD"`, "capabilities/payments/targets/us-east4-dev-k8s/environment-variables/launchdarkly.yml", setTo("secret:BAD", "LD_SDK_KEY"), `^capabilities/payments/targets/us-east4-dev-k8s/environment-variables/launchdarkly\.yml:[0-9]+:[0-9]+: LD_SDK_KEY: .*secret:<gcp-project>`},
	{`.color = "puce"`, "capabilities/payments/stages/dev.yml", setTo("puce", "color"), `^capabilities/payments/stages/dev\.yml:[0-9]+:[0-9]+: .*unknown color; allowed: red`},
	{`.config.cluster = "eu"`, tgK8, setTo("eu", "config", "cluster"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: .*unknown cluster; allowed: us-east4, dc1`},
	{`.image = "ghcr.io/pitoniak32/nginx:1.0.0"`, appW, setTo("ghcr.io/pitoniak32/nginx:1.0.0", "image"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+:`},
	{`.stages.dev.workflow.file = "deploy.sh"`, appW, setTo("deploy.sh", "stages", "dev", "workflow", "file"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: .*workflow\.file: expected a \.yml or \.yaml file name`},
	{`.deployWith = "jenkins"`, appW, setTo("jenkins", "deployWith"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: deployWith: expected argocd or gha$`},
	{`.workflow.repo = "not-a-repo"`, appW, setTo("not-a-repo", "workflow", "repo"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: workflow\.repo: expected <owner>/<name>$`},
	{`.workflow.ref = ""`, appW, setTo("", "workflow", "ref"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: workflow\.ref: expected a non-empty git ref$`},
	{`.config.cluster = "us"`, tgTF, setTo("us", "config", "cluster"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: .*cluster not allowed on a TERRAFORM target`},
	{`del(.config.backend.bucket)`, tgTF, delAt("config", "backend", "bucket"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: .*bucket`},
	{`.config.gcpProject = "Bad"`, tgTF, setTo("Bad", "config", "gcpProject"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: .*gcpProject`},
	{`.config.gcpProject = "acme-payments-dev"`, tgK8, setTo("acme-payments-dev", "config", "gcpProject"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: .*gcpProject not allowed on a K8S target`},
	{`.config.backend = {"bucket": "b-xx"}`, tgK8, setTo(m{"bucket": "b-xx"}, "config", "backend"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: .*backend not allowed on a K8S target`},
	{`.config.terraform = {"gcpProject": "acme-x"}`, tgTF, setTo(m{"gcpProject": "acme-x"}, "config", "terraform"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: config\.terraform: moved: use config\.gcpProject / config\.backend`},
	{`del(.config.gcpProject)`, tgTF, delAt("config", "gcpProject"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: .*gcpProject`},
	{`del(.config.namespace)`, tgK8, delAt("config", "namespace"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: .*namespace`},
	{`.image = "ghcr.io/x/y"`, appI, setTo("ghcr.io/x/y", "image"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*set exactly one of image \(K8s app\) or module \(terraform app\)`},
	{`del(.module)`, appI, delAt("module"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*set exactly one of image \(K8s app\) or module \(terraform app\)`},
	{`.replicas = 2`, appI, setTo(2, "replicas"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*replicas not allowed on a terraform app`},
	{`.stages.dev.port = 80`, appI, setTo(80, "stages", "dev", "port"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*not allowed`},
	{`.stages.dev.targets.us-east4-dev-tf.deployWith = "argocd"`, appI, setTo("argocd", "stages", "dev", "targets", "us-east4-dev-tf", "deployWith"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*deployWith not allowed on a terraform app`},
	{`.resources.cpu = "1"`, appI, setTo("1", "resources", "cpu"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*resources not allowed on a terraform app`},
	{`.stages.dev.targets = {"us-east4-dev-k8s": {}}`, appI, setTo(m{"us-east4-dev-k8s": m{}}, "stages", "dev", "targets"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*target us-east4-dev-k8s is K8S; terraform apps need TERRAFORM targets`},
	{`.stages.dev.targets = {"us-east4-dev-tf": {}}`, appW, setTo(m{"us-east4-dev-tf": m{}}, "stages", "dev", "targets"), `^capabilities/payments/apps/worker/app\.yml:[0-9]+:[0-9]+: .*target us-east4-dev-tf is TERRAFORM; K8s apps need K8S or CLOUD_RUN targets`},
	{`.apps.worker.viewers = ["carol@example.com", "carol@example.com"]`, ow, setTo([]string{"carol@example.com", "carol@example.com"}, "apps", "worker", "viewers"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*emails must not repeat`},
	{`.apps.worker.promoters = ["doug@example.com"]`, ow, setTo([]string{"doug@example.com"}, "apps", "worker", "promoters"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*expected a map`},
	{`.apps.worker.promoters.perf = ["doug@example.com"]`, ow, setTo([]string{"doug@example.com"}, "apps", "worker", "promoters", "perf"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*app worker doesn't use stage perf`},
	{`.apps.worker.promoters.qa = ["doug@example.com"]`, ow, setTo([]string{"doug@example.com"}, "apps", "worker", "promoters", "qa"), `^capabilities/payments/owners\.yml:[0-9]+:[0-9]+: .*unknown stage; allowed: dev, perf, prod, staging`},
	{`del(.config.environmentType)`, tgK8, delAt("config", "environmentType"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: config\.environmentType: expected dev, npr or prd$`},
	{`.config.environmentType = "staging"`, tgK8, setTo("staging", "config", "environmentType"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: .*expected dev, npr or prd`},
	{`.config.region = "us-east9"`, tgTF, setTo("us-east9", "config", "region"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: .*unknown region; allowed: us-central1, us-east1, us-east4`},
	{`del(.config.region)`, tgTF, delAt("config", "region"), `^capabilities/payments/targets/us-east4-dev-tf/target\.yml:[0-9]+:[0-9]+: config\.region: region required on a TERRAFORM target; allowed: us-central1, us-east1, us-east4$`},
	{`.config.region = "us-east4"`, tgK8, setTo("us-east4", "config", "region"), `^capabilities/payments/targets/us-east4-dev-k8s/target\.yml:[0-9]+:[0-9]+: .*region not allowed on a K8S target \(it's the cluster's\)`},
	{`.config.environmentType = "dev"`, "capabilities/payments/targets/us-east4-npr-k8s/target.yml", setTo("dev", "config", "environmentType"), `^capabilities/payments/targets/us-east4-npr-k8s/target\.yml:[0-9]+:[0-9]+: .*expected us-east4-dev-k8s`},
	{`del(.config.region)`, tgCR, delAt("config", "region"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: config\.region: region required on a CLOUD_RUN target; allowed: us-central1, us-east1, us-east4$`},
	{`.config.region = "us-east9"`, tgCR, setTo("us-east9", "config", "region"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: .*unknown region; allowed:`},
	{`.config.cluster = "us-east4"`, tgCR, setTo("us-east4", "config", "cluster"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: config\.cluster: cluster not allowed on a CLOUD_RUN target$`},
	{`.config.namespace = "x"`, tgCR, setTo("x", "config", "namespace"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: config\.namespace: namespace not allowed on a CLOUD_RUN target$`},
	{`.config.backend = {"bucket": "bkt"}`, tgCR, setTo(m{"bucket": "bkt"}, "config", "backend"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: .*backend not allowed on a CLOUD_RUN target`},
	{`del(.config.gcpProject)`, tgCR, delAt("config", "gcpProject"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: config\.gcpProject: `},
	{`.config.platform = "LAMBDA"`, tgCR, setTo("LAMBDA", "config", "platform"), `^capabilities/payments/targets/us-east4-npr-cr/target\.yml:[0-9]+:[0-9]+: .*expected K8S, TERRAFORM or CLOUD_RUN`},
	{`.stages.dev.targets = {"us-east4-npr-cr": {}}`, appI, setTo(m{"us-east4-npr-cr": m{}}, "stages", "dev", "targets"), `^capabilities/payments/apps/infra/app\.yml:[0-9]+:[0-9]+: .*target us-east4-npr-cr is CLOUD_RUN; terraform apps need TERRAFORM targets`},
	{`.stages.staging.targets = {"us-east4-dev-tf": {}}`, "capabilities/payments/apps/web/app.yml", setTo(m{"us-east4-dev-tf": m{}}, "stages", "staging", "targets"), `^capabilities/payments/apps/web/app\.yml:[0-9]+:[0-9]+: .*target us-east4-dev-tf is TERRAFORM; K8s apps need K8S or CLOUD_RUN targets`},
}

// test.sh: "lint case ok: <expr>" loop over lint-cases.tsv
func TestLintCases(t *testing.T) {
	t.Parallel()
	if len(lintCases) != 64 {
		t.Fatalf("lint-cases.tsv had 64 rows, table has %d", len(lintCases))
	}
	for i, c := range lintCases {
		t.Run(strings.ReplaceAll(c.name, " ", "_"), func(t *testing.T) {
			t.Parallel()
			r := fixture(t)
			c.edit(t, filepath.Join(r, c.file))
			got := lintOut(t, r)
			if !regexp.MustCompile("(?m)" + c.want).MatchString(got) {
				t.Errorf("lint case FAILED (row %d): %s %s\nwant: %s\ngot:\n%s", i+1, c.file, c.name, c.want, got)
			}
		})
	}
}
