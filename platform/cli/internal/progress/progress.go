// Package progress draws one in-place progress line per phase of a long command (lint, render) on a
// terminal, and leaves a one-line summary when the phase is done. With a termstatus.Reporter it also
// reports the running phase and its percentage to the terminal tab (OSC 7501). Every method is safe on a nil
// *Tracker (no output), so callers pass nil when progress is off; Step is safe for concurrent use.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/termstatus"
)

const (
	barWidth = 24
	// redraw at most this often: thousands of fast steps must not cost thousands of writes
	frameEvery = 50 * time.Millisecond
)

// Tracker writes phases to w (nil: no bars), one at a time, and reports them to st (nil: none).
type Tracker struct {
	mu        sync.Mutex
	w         io.Writer
	st        *termstatus.Reporter
	now       func() time.Time
	nameWidth int
	cur       *Phase
	lastDraw  time.Time
	// whole-command progress for the tab status: steps planned over all phases, steps done, and the
	// last percentage reported (never goes back)
	planned, stepsDone, pct int
}

// Plan adds n steps to the whole command's total; the tab status shows one percentage over all
// phases, not one per phase. Call it before those steps run.
func (t *Tracker) Plan(n int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.planned += n
}

// clear returns to the line start and erases it (only ever written to a terminal)
const clear = "\r\x1b[K"

// Phase is one counted part of the work.
type Phase struct {
	t           *Tracker
	name        string
	total, done int
	item        string
	start       time.Time
}

// New draws on w.
func New(w io.Writer) *Tracker { return &Tracker{w: w, now: time.Now, nameWidth: 14} }

// WithStatus also reports phases to st; returns t.
func (t *Tracker) WithStatus(st *termstatus.Reporter) *Tracker {
	if t != nil {
		t.st = st
	}
	return t
}

// Auto draws bars on the controlling terminal (/dev/tty), so `cmd > file` or `2> file` never
// captures one. No bars when off, when errOut isn't a process stream (tests capture output in a
// buffer), or with no terminal (CI). Phases still go to st (the tab status) when bars are off; nil
// when there is neither.
func Auto(errOut io.Writer, off bool, st *termstatus.Reporter) *Tracker {
	var w io.Writer
	if _, ok := errOut.(*os.File); !off && ok {
		if f := color.OpenTTY(); f != nil {
			w = f
		}
	}
	if w == nil && st == nil {
		return nil
	}
	return &Tracker{w: w, st: st, now: time.Now, nameWidth: 14}
}

// Start begins a phase of total steps; a phase still running is finished first.
func (t *Tracker) Start(name string, total int) *Phase {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur != nil {
		t.finish(t.cur)
	}
	if len(name) > t.nameWidth {
		t.nameWidth = len(name)
	}
	p := &Phase{t: t, name: name, total: total, start: t.now()}
	t.cur = p
	t.draw(true)
	return p
}

// Step counts one finished step; item (e.g. the system) is shown next to the bar.
func (p *Phase) Step(item string) {
	if p == nil {
		return
	}
	t := p.t
	t.mu.Lock()
	defer t.mu.Unlock()
	p.done++
	t.stepsDone++
	p.item = item
	if t.cur == p {
		t.draw(false)
	}
}

// Done ends the phase, leaving "✓ <name>  <done>/<total>  <elapsed>".
func (p *Phase) Done() {
	if p == nil {
		return
	}
	t := p.t
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cur == p {
		t.finish(p)
	}
}

func (t *Tracker) finish(p *Phase) {
	el := t.now().Sub(p.start)
	if t.w != nil {
		_, _ = fmt.Fprintf(t.w, "%s✓ %-*s %s  %s\n", clear, t.nameWidth, p.name, count(p), dur(el))
	}
	t.cur = nil
}

// draw redraws the running phase; force skips the frame limit.
func (t *Tracker) draw(force bool) {
	p := t.cur
	now := t.now()
	if !force && p.done < p.total && now.Sub(t.lastDraw) < frameEvery {
		return
	}
	t.lastDraw = now
	msg := strings.TrimSpace(fmt.Sprintf("%s %s %s", p.name, count(p), p.item))
	if t.planned > 0 {
		t.pct = max(t.pct, min(100*t.stepsDone/t.planned, 100))
		t.st.Progress(msg, t.pct)
	} else {
		t.st.Working(msg) // nothing planned: no percentage (indeterminate)
	}
	if t.w == nil {
		return
	}
	fill := 0
	if p.total > 0 {
		fill = barWidth * p.done / p.total
	}
	_, _ = fmt.Fprintf(t.w, "%s  %-*s [%s%s] %s %s", clear, t.nameWidth, p.name,
		strings.Repeat("#", fill), strings.Repeat("-", barWidth-fill), count(p), p.item)
}

func count(p *Phase) string {
	w := len(fmt.Sprint(p.total))
	return fmt.Sprintf("%*d/%d", w, p.done, p.total)
}

func dur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
