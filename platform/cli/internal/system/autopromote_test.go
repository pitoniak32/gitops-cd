package system

import "testing"

// autoPromote: manual by default, stages/<s>.yml sets it for every app, app.yml stages.<s> overrides it
// for one app (the fixture's worker opts out of dev).
func TestAutoPromoteOverride(t *testing.T) {
	m := srcModel(t)
	const worker = "systems/payments/apps/worker/app.yml"
	for _, c := range []struct {
		app, target string
		want        bool
		file        string
		line        int
		note        string
	}{
		{"my-app", "us-east4-dev-k8s", true, "systems/payments/stages/dev.yml", lineOf(t, "systems/payments/stages/dev.yml", "", "autoPromote"), ""},
		{"worker", "us-east4-dev-k8s", false, worker, lineOf(t, worker, "dev:", "autoPromote"), ""},
		{"worker", "us-east4-npr-k8s", false, "", 0, "default: manual"},
	} {
		d := m.deploy(c.app, c.target)
		if d == nil {
			t.Fatalf("no deploy %s/%s", c.app, c.target)
		}
		if d.autoPromote != c.want {
			t.Errorf("%s autoPromote = %v, want %v", d.key(), d.autoPromote, c.want)
		}
		if got := d.src["autoPromote"]; got.File != c.file || got.Line != c.line || got.Note != c.note {
			t.Errorf("%s src[autoPromote] = %+v, want %s:%d %q", d.key(), got, c.file, c.line, c.note)
		}
		if got := m.pipelines[c.app].stage(d.stage).autoPromote; got != c.want {
			t.Errorf("%s pipeline autoPromote = %v, want %v", d.key(), got, c.want)
		}
	}
}
