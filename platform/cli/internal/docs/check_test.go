package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
)

const fixture = "../../../render/testdata/root"

// guideRoot is a temp repo root: the fixture copied to the root and to FixtureRoot, plus
// docs/guide/x.md = page.
func guideRoot(t *testing.T, page string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, filepath.FromSlash(capability.FixtureRoot)), os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	if page != "" {
		g := filepath.Join(root, "docs", "guide")
		if err := os.MkdirAll(g, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g, "x.md"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func fixtureFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixture, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func check(t *testing.T, page string) []string {
	t.Helper()
	d, err := Check(guideRoot(t, page))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range d {
		out = append(out, x.String())
	}
	return out
}

func want(t *testing.T, got []string, w ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(w, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(w, "\n"))
	}
}

func fence(attrs, body string) string { return "```yaml " + attrs + "\n" + body + "```\n" }

func TestCheckValid(t *testing.T) {
	want(t, check(t, "# x\n\n"+fence("path=capabilities/payments/stages/dev.yml", fixtureFile(t, "capabilities/payments/stages/dev.yml"))))
}

func TestCheckSnippetError(t *testing.T) {
	// fence on line 3; bogus on snippet line 2 → markdown line 5
	body := "image: ghcr.io/example/web\nbogus: 1\n" + strings.Replace(fixtureFile(t, "capabilities/payments/apps/web/app.yml"), "image: ghcr.io/example/web\n", "", 1)
	want(t, check(t, "# x\n\n"+fence("path=capabilities/payments/apps/web/app.yml", body)),
		"docs/guide/x.md:5:1: bogus: field not allowed")
}

func TestCheckMissingRequired(t *testing.T) {
	// lint reports at the document (1:1) → first snippet line
	got := check(t, "# x\n\n"+fence("path=capabilities/payments/apps/web/app.yml", "image: ghcr.io/example/web\nstages:\n  dev:\n    targets: {us-east4-dev-k8s: {}}\n"))
	want(t, got, "docs/guide/x.md:4:1: sourceRepo: required")
}

func TestCheckCrossFile(t *testing.T) {
	// a cycle: lint also reports in staging.yml, which the snippet didn't write → fence line
	got := check(t, "# x\n\n"+fence("path=capabilities/payments/stages/dev.yml", "dependsOn: [staging]\n"))
	want(t, got,
		"docs/guide/x.md:3:1: capabilities/payments/stages/staging.yml: dependsOn: stage staging is in a dependency cycle",
		"docs/guide/x.md:4:1: dependsOn: stage dev is in a dependency cycle")
}

func TestCheckExpect(t *testing.T) {
	bad := "bogus: 1\n" + fixtureFile(t, "capabilities/payments/stages/dev.yml")
	want(t, check(t, fence(`path=capabilities/payments/stages/dev.yml expect="field not allowed"`, bad)))
	want(t, check(t, fence(`path=capabilities/payments/stages/dev.yml expect="nope"`, bad)),
		`docs/guide/x.md:1:1: expected lint error containing "nope", got: docs/guide/x.md:2:1: bogus: field not allowed`)
	want(t, check(t, fence(`path=capabilities/payments/stages/dev.yml expect="nope"`, fixtureFile(t, "capabilities/payments/stages/dev.yml"))),
		`docs/guide/x.md:1:1: expected lint error containing "nope", got: no errors`)
}

func TestCheckFragment(t *testing.T) {
	want(t, check(t, fence("fragment", "env: [not: valid\n")))
}

func TestCheckUnannotated(t *testing.T) {
	want(t, check(t, "x\n```yaml\na: 1\n```\n```sh\nls\n```\n```mermaid\nflowchart LR\n```\n"),
		"docs/guide/x.md:2:1: yaml block needs path=…, expect=… or fragment")
}

func TestCheckBadPath(t *testing.T) {
	root := guideRoot(t, fence("path=../../escape.yml", "a: 1\n")+fence("path=README.md", "a: 1\n"))
	d, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range d {
		got = append(got, x.String())
	}
	want(t, got,
		"docs/guide/x.md:1:1: path must be under capabilities/ or teams/",
		"docs/guide/x.md:4:1: path must be under capabilities/ or teams/")
	if _, err := os.Stat(filepath.Join(root, "..", "escape.yml")); err == nil {
		t.Error("wrote outside the temp copy")
	}
}

func TestCheckNoGuide(t *testing.T) {
	d, err := Check(guideRoot(t, ""))
	if d != nil || err != nil {
		t.Errorf("got %v, %v", d, err)
	}
}

func TestCheckIsolation(t *testing.T) {
	page := fence("path=capabilities/payments/stages/qa.yml", "bogus: 1\n") +
		fence("path=capabilities/payments/stages/dev.yml", fixtureFile(t, "capabilities/payments/stages/dev.yml"))
	want(t, check(t, page), "docs/guide/x.md:2:1: bogus: field not allowed")
}

func TestRepoDocs(t *testing.T) {
	changed, d, err := Run("../../../..", false)
	if err != nil {
		t.Fatal(err)
	}
	if changed != nil {
		t.Errorf("stale embeds in %v: run gcd docs", changed)
	}
	for _, x := range d {
		t.Error(x)
	}
}

func TestCheckReferenceDir(t *testing.T) {
	root := guideRoot(t, "")
	ref := filepath.Join(root, "docs", "reference")
	if err := os.MkdirAll(ref, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ref, "x.md"), []byte(fence("path=capabilities/payments/stages/dev.yml", "bogus: 1\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Check(root)
	if err != nil || len(d) != 1 || d[0].String() != "docs/reference/x.md:2:1: bogus: field not allowed" {
		t.Errorf("got %v %v", d, err)
	}
}

func TestCheckEmbedFenceExempt(t *testing.T) {
	page := "<!-- embed path=capabilities/payments/stages/dev.yml -->\n```yaml\na: 1\n```\n<!-- /embed -->\n```yaml\nb: 1\n```\n"
	want(t, check(t, page), "docs/guide/x.md:6:1: yaml block needs path=…, expect=… or fragment")
}

func TestRunFixtureLint(t *testing.T) {
	root := guideRoot(t, "# x\n")
	dev := filepath.Join(root, filepath.FromSlash(capability.FixtureRoot), "capabilities", "payments", "stages", "dev.yml")
	if err := os.WriteFile(dev, []byte("bogus: 1\n"+fixtureFile(t, "capabilities/payments/stages/dev.yml")), 0o644); err != nil {
		t.Fatal(err)
	}
	_, d, err := Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range d {
		got = append(got, x.String())
	}
	want(t, got, "platform/render/testdata/root/capabilities/payments/stages/dev.yml:1:1: bogus: field not allowed")
}

func TestCheckIndentedFence(t *testing.T) {
	// a fence inside a list item: still checked, body dedented by the fence's indent
	page := "- item\n\n  ```yaml path=capabilities/payments/stages/dev.yml\n  bogus: 1\n  ```\n"
	want(t, check(t, page), "docs/guide/x.md:4:1: bogus: field not allowed")
	want(t, check(t, "- item\n\n  ```yaml\n  a: 1\n  ```\n"),
		"docs/guide/x.md:3:1: yaml block needs path=…, expect=… or fragment")
}

func TestCheckMarkerInPlainFence(t *testing.T) {
	// a marker shown in a plain fence must not exempt later yaml fences
	page := "```\n<!-- embed path=capabilities/payments/stages/dev.yml -->\n```\n```yaml\na: 1\n```\n"
	want(t, check(t, page), "docs/guide/x.md:4:1: yaml block needs path=…, expect=… or fragment")
}

func TestCheckEvaluateDir(t *testing.T) {
	root := guideRoot(t, "")
	ev := filepath.Join(root, "docs", "evaluate")
	if err := os.MkdirAll(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ev, "x.md"), []byte(fence("path=capabilities/payments/stages/dev.yml", "bogus: 1\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Check(root)
	if err != nil || len(d) != 1 || d[0].String() != "docs/evaluate/x.md:2:1: bogus: field not allowed" {
		t.Errorf("got %v %v", d, err)
	}
}

func TestCheckIndentedEmbedFenceExempt(t *testing.T) {
	page := "- item\n\n  <!-- embed path=capabilities/payments/stages/dev.yml -->\n  ```yaml\n  a: 1\n  ```\n  <!-- /embed -->\n"
	want(t, check(t, page))
}
