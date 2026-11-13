package progress

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/termstatus"
)

func TestNilIsNoop(t *testing.T) {
	var tr *Tracker
	p := tr.Start("x", 3)
	p.Step("a")
	p.Done()
}

func TestPhases(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)
	clock := time.Unix(0, 0)
	tr.now = func() time.Time { return clock }

	p := tr.Start("systems", 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.Step("sys") }()
	}
	wg.Wait()
	clock = clock.Add(1500 * time.Millisecond)
	p.Done()
	q := tr.Start("teams", 2)
	q.Step("t1")
	clock = clock.Add(3 * time.Millisecond)
	tr.Start("names", 1).Done() // Start finishes teams

	out := buf.String()
	// the in-place frames start with \r; summaries end each phase on their own line
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	var summaries []string
	for _, l := range lines {
		i := strings.LastIndex(l, clear)
		summaries = append(summaries, l[i+len(clear):])
	}
	want := []string{
		"✓ systems        100/100  1.5s",
		"✓ teams          1/2  3ms",
		"✓ names          0/1  0ms",
	}
	if strings.Join(summaries, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s\nraw %q", strings.Join(summaries, "\n"), strings.Join(want, "\n"), out)
	}
	if !strings.Contains(out, "[########################] 100/100 sys") {
		t.Errorf("no full bar frame: %q", out)
	}
	// same clock: only the forced first frame and the final one are drawn, not one per step
	if n := strings.Count(out, "systems ["); n > 3 {
		t.Errorf("%d frames for one instant, want frame limiting", n)
	}
}

// The tab status shows one percentage for the whole command: it doesn't restart at each phase.
func TestStatusWholeCommand(t *testing.T) {
	var st bytes.Buffer
	tr := New(&bytes.Buffer{}).WithStatus(termstatus.New(&st, "gcd-lint"))
	tr.Plan(4)
	a := tr.Start("names", 2)
	a.Step("x")
	a.Step("y")
	a.Done()
	b := tr.Start("systems", 2)
	b.Step("p")
	b.Step("q")
	b.Done()
	var got []string
	for _, m := range regexp.MustCompile(`progress=(\d+)`).FindAllStringSubmatch(st.String(), -1) {
		got = append(got, m[1])
	}
	if strings.Join(got, ",") != "0,50,50,100" { // mid-phase steps within one frame are skipped
		t.Fatalf("percentages %v", got)
	}
}
