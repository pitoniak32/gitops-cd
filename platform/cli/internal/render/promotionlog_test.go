package render

import (
	"strings"
	"testing"
)

// Both deploying ClusterPromotionTasks end with the promotion-log record step (ADR 0032): it runs after any
// failure (always()), never fails or blocks the Promotion (continueOnError, short retry), and sends the
// record fields; gha-check (a pre-deploy check, not a deploy) has none.
func TestPromotionRecordStep(t *testing.T) {
	t.Parallel()
	ts := promotionTasks(t)
	for _, c := range []struct{ task, deployWith, runURL string }{
		{"promote-argocd", "argocd", "''"},
		{"promote-gha", "gha", "task.outputs.dispatch?.runURL ?? ''"},
	} {
		task := find(ts, "ClusterPromotionTask", c.task)
		steps := list(task, "spec.steps")
		last := steps[len(steps)-1].(map[string]any)
		for path, want := range map[string]string{
			"uses": "http", "as": "record", "if": "${{ always() }}", "continueOnError": "true", "retry.timeout": "30s",
			// a dial error is an Errored attempt: retried up to the threshold, not just once (Kargo default 1)
			"retry.errorThreshold": "5", "config.pollInterval": "5s",
			"config.method": "POST", "config.url": "http://promotions.localhost/promotions",
			"config.successExpression": "response.status == 204",
			"config.failureExpression": "response.status >= 400 && response.status < 500",
		} {
			if got := str(get(last, path)); got != want {
				t.Errorf("%s last step %s = %q, want %q", c.task, path, got, want)
			}
		}
		body := str(get(last, "config.body"))
		for _, f := range []string{
			"'promotion': ctx.promotion", "'project': ctx.project", "'system': vars.system", "'app': vars.app",
			"'stage': vars.stage", "'target': vars.target", "'platform': vars.platform",
			"'deployWith': '" + c.deployWith + "'",
			"'freight': ctx.targetFreight.name", "'freightAlias': ctx.targetFreight.alias",
			"'warehouse': ctx.targetFreight.origin.name",
			"'artifact': vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest",
			// skipped after a failed pre-deploy check: no outputs, so never a bare reference
			"'commit': task.outputs.commit?.commit ?? ''",
			"'actor': ctx.meta.promotion.actor",
			// ctx actor = create-actor annotation after its first ':' (admin@example.com; a controller:
			// system:serviceaccount:kargo:kargo-controller-<shard>); control-plane auto-promotions may have none
			"'automatic': ctx.meta.promotion.actor == '' || ctx.meta.promotion.actor startsWith 'system:serviceaccount:kargo:kargo-controller'",
			"'rollback': ctx.meta.promotion.rollback",
			"'result': success() ? 'Succeeded' : 'Failed'",
			"'ghaRunURL': " + c.runURL,
			// absolute: ctx.uiBaseUrl is empty in region shards (their Kargo API is disabled)
			"'uiURL': 'https://kargo.localhost/project/' + ctx.project + '/promotion/' + ctx.promotion",
		} {
			if !strings.Contains(body, f) {
				t.Errorf("%s record body missing %s", c.task, f)
			}
		}
		if !strings.HasPrefix(body, "${{ quote({") || !strings.HasSuffix(body, "}) }}") || strings.Contains(body, "}}'") {
			t.Errorf("%s record body: %s", c.task, body)
		}
	}
	for _, s := range list(find(ts, "ClusterPromotionTask", "gha-check"), "spec.steps") {
		if str(get(s, "as")) == "record" {
			t.Error("gha-check must not record (it deploys nothing)")
		}
	}
}

// Every target Stage passes its platform label as the platform var (the record's platform field).
func TestStagePlatformVar(t *testing.T) {
	t.Parallel()
	for _, app := range []string{"worker", "my-app", "infra"} {
		for _, s := range findAll(kargoApp(t, app), "Stage") {
			if label(s, "example.com/kind") != "target" {
				continue
			}
			n := str(get(s, "metadata.name"))
			if got, want := varValue([]map[string]any{s}, n, "platform"), label(s, "example.com/platform"); got != want || got == "" {
				t.Errorf("%s/%s platform var %q, want %q", app, n, got, want)
			}
		}
	}
}
