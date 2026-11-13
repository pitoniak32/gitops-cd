package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const devOpen = "<!-- embed path=systems/payments/stages/dev.yml -->"

func embed(t *testing.T, page string) (string, []string) {
	t.Helper()
	out, d := Embed("docs/guide/x.md", page, os.DirFS(fixture))
	var ds []string
	for _, x := range d {
		ds = append(ds, x.String())
	}
	return out, ds
}

func TestEmbedWholeFile(t *testing.T) {
	got, d := embed(t, "x\n"+devOpen+"\nold\n<!-- /embed -->\ny\n")
	want := "x\n" + devOpen + "\n```yaml\n" + fixtureFile(t, "systems/payments/stages/dev.yml") + "```\n\n" +
		"Source: [`payments/stages/dev.yml`](../../platform/render/testdata/root/systems/payments/stages/dev.yml)\n" +
		"<!-- /embed -->\ny\n"
	if got != want || d != nil {
		t.Errorf("got %q (%v)\nwant %q", got, d, want)
	}
}

func TestEmbedRange(t *testing.T) {
	got, d := embed(t, "<!-- embed path=systems/payments/apps/my-app/app.yml lines=2-3 -->\n<!-- /embed -->\n")
	want := "```yaml\nsourceRepo: acme/my-app\nimageConstraint: ^1\n```\n\n" +
		"Source: [`payments/apps/my-app/app.yml:2-3`](../../platform/render/testdata/root/systems/payments/apps/my-app/app.yml#L2-L3)\n"
	if !strings.Contains(got, want) || d != nil {
		t.Errorf("got %q (%v)", got, d)
	}
}

func TestEmbedDedent(t *testing.T) {
	got, _ := embed(t, "<!-- embed path=systems/payments/apps/my-app/app.yml lines=12-14 -->\n<!-- /embed -->\n")
	if !strings.Contains(got, "```yaml\nenv:\n  STAGE_ENV: 'false'\nreplicas: 1\n```") {
		t.Errorf("got %q", got)
	}
}

func TestEmbedIdempotent(t *testing.T) {
	once, _ := embed(t, "a\n"+devOpen+"\n<!-- /embed -->\nb\n")
	twice, _ := embed(t, once)
	if once != twice {
		t.Errorf("not idempotent:\n%q\n%q", once, twice)
	}
}

func TestEmbedNoTrailingNewline(t *testing.T) {
	fx := fstest.MapFS{"a.yml": {Data: []byte("a: 1")}}
	got, d := Embed("docs/guide/x.md", "<!-- embed path=a.yml -->\n<!-- /embed -->\n", fx)
	if !strings.HasPrefix(got, "<!-- embed path=a.yml -->\n```yaml\na: 1\n```\n") || d != nil {
		t.Errorf("got %q (%v)", got, d)
	}
}

func TestEmbedInsideFenceIgnored(t *testing.T) {
	page := "```markdown\n" + devOpen + "\nkeep\n<!-- /embed -->\n```\n"
	got, d := embed(t, page)
	if got != page || d != nil {
		t.Errorf("got %q (%v)", got, d)
	}
}

func TestEmbedErrors(t *testing.T) {
	for _, tc := range []struct{ open, msg string }{
		{"<!-- embed path=systems/payments/stages/nope.yml -->", "systems/payments/stages/nope.yml: no such file in the fixture"},
		{"<!-- embed path=../x.yml -->", "path must be a file under the fixture"},
		{"<!-- embed path=systems/payments/stages/dev.yml lines=2-1 -->", "lines must be A-B with 1 <= A <= B <= 2"},
		{"<!-- embed path=systems/payments/stages/dev.yml lines=0-1 -->", "lines must be A-B with 1 <= A <= B <= 2"},
		{"<!-- embed path=systems/payments/stages/dev.yml lines=1-999 -->", "lines must be A-B with 1 <= A <= B <= 2"},
		{"<!-- embed path=systems/payments/stages/dev.yml lines=abc -->", "lines must be A-B with 1 <= A <= B <= 2"},
		{"<!-- embed path=systems/payments/stages/dev.yml foo=1 -->", `unknown embed attribute "foo"`},
		{"<!-- embed lines=1-2 -->", "embed needs path="},
	} {
		page := "x\n" + tc.open + "\nkeep\n<!-- /embed -->\n"
		got, d := embed(t, page)
		if got != page || len(d) != 1 || d[0] != "docs/guide/x.md:2:1: "+tc.msg {
			t.Errorf("%s: got %q %v", tc.open, got, d)
		}
	}
	for _, page := range []string{
		"x\n" + devOpen + "\nkeep\n",
		"x\n" + devOpen + "\n" + devOpen + "\n<!-- /embed -->\n",
	} {
		got, d := embed(t, page)
		if got != page || len(d) == 0 || !strings.HasPrefix(d[0], "docs/guide/x.md:2:1: ") {
			t.Errorf("%q: got %q %v", page, got, d)
		}
	}
	if _, d := embed(t, "<!-- /embed -->\n"); len(d) != 1 || d[0] != "docs/guide/x.md:1:1: /embed without embed" {
		t.Errorf("stray close: %v", d)
	}
}

func TestEmbedsWalk(t *testing.T) {
	root := guideRoot(t, "")
	ref := filepath.Join(root, "docs", "reference")
	if err := os.MkdirAll(ref, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := devOpen + "\nold\n<!-- /embed -->\n"
	p := filepath.Join(ref, "a.md")
	if err := os.WriteFile(p, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, d, err := Embeds(root, false)
	if err != nil || d != nil || strings.Join(changed, ",") != "docs/reference/a.md" {
		t.Fatalf("dry: %v %v %v", changed, d, err)
	}
	if b, _ := os.ReadFile(p); string(b) != stale {
		t.Error("dry run wrote")
	}
	if changed, _, err := Embeds(root, true); err != nil || len(changed) != 1 {
		t.Fatalf("write: %v %v", changed, err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "Source: [") {
		t.Errorf("not written: %s", b)
	}
	if changed, _, _ := Embeds(root, true); changed != nil {
		t.Errorf("second run changed %v", changed)
	}
}

func TestEmbedHunks(t *testing.T) {
	got, d := embed(t, `<!-- embed path=systems/payments/apps/my-app/app.yml lines="1-2,(9-10;text='dev overrides'),(17-20;text='target wins')" -->`+"\n<!-- /embed -->\n")
	want := "```yaml\n" +
		"image: ghcr.io/pitoniak32/nginx\n" +
		"sourceRepo: acme/my-app\n" +
		"# …\n" +
		"# dev overrides\n" +
		"stages:\n" +
		"  dev:\n" +
		"      # …\n" +
		"      # target wins\n" +
		"      dc1-dev-k8s:  # target level wins over stage and app level\n" +
		"        env:\n" +
		"          APP_ENV: 'false'\n" +
		"        replicas: 3\n" +
		"```\n\n" +
		"Source: [`payments/apps/my-app/app.yml:1-2,9-10,17-20`](../../platform/render/testdata/root/systems/payments/apps/my-app/app.yml#L1-L20)\n"
	if !strings.Contains(got, want) || d != nil {
		t.Errorf("got %q (%v)\nwant %q", got, d, want)
	}
}

func TestEmbedHunksAdjacentAndSingle(t *testing.T) {
	got, d := embed(t, `<!-- embed path=systems/payments/apps/my-app/app.yml lines="1-2, 3" -->`+"\n<!-- /embed -->\n")
	if !strings.Contains(got, "```yaml\nimage: ghcr.io/pitoniak32/nginx\nsourceRepo: acme/my-app\nimageConstraint: ^1\n```\n\nSource: [`payments/apps/my-app/app.yml:1-3`](") || d != nil {
		t.Errorf("adjacent: %q %v", got, d)
	}
	got, d = embed(t, "<!-- embed path=systems/payments/apps/my-app/app.yml lines=3 -->\n<!-- /embed -->\n")
	if !strings.Contains(got, "```yaml\nimageConstraint: ^1\n```\n\nSource: [`payments/apps/my-app/app.yml:3`](") || d != nil {
		t.Errorf("single: %q %v", got, d)
	}
}

func TestEmbedHunkErrors(t *testing.T) {
	const p = "<!-- embed path=systems/payments/apps/my-app/app.yml lines="
	for _, tc := range []struct{ lines, msg string }{
		{`"(9-10;'x')"`, "hunk options are key='value': (9-10;'x')"},
		{`"(9-10;foo='x')"`, `unknown hunk option "foo"`},
		{`"5-10,8-12"`, "hunks must be in order without overlap: 5-10,8-12"},
		{`"9-10,1-2"`, "hunks must be in order without overlap: 9-10,1-2"},
		{`"(9-10;text='x)"`, "unterminated quote"},
		{`"(9-10"`, "unbalanced parentheses"},
		{`"1-2,,3"`, "lines must be A-B with 1 <= A <= B <= 44"},
		{`"1-2`, "unterminated quote"},
	} {
		page := p + tc.lines + " -->\nkeep\n<!-- /embed -->\n"
		got, d := embed(t, page)
		if got != page || len(d) != 1 || d[0] != "docs/guide/x.md:1:1: "+tc.msg {
			t.Errorf("%s: got %v", tc.lines, d)
		}
	}
}

func TestEmbedIndented(t *testing.T) {
	page := "- item\n\n  <!-- embed path=systems/payments/stages/dev.yml -->\n  old\n  <!-- /embed -->\n"
	got, d := embed(t, page)
	want := "- item\n\n  <!-- embed path=systems/payments/stages/dev.yml -->\n" +
		"  ```yaml\n  dependsOn: []\n  autoPromote: true\n  ```\n\n" +
		"  Source: [`payments/stages/dev.yml`](../../platform/render/testdata/root/systems/payments/stages/dev.yml)\n" +
		"  <!-- /embed -->\n"
	if got != want || d != nil {
		t.Errorf("got %q (%v)\nwant %q", got, d, want)
	}
	if again, _ := embed(t, got); again != got {
		t.Errorf("not idempotent: %q", again)
	}
}
