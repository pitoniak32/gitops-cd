// Package termstatus reports what a long command is doing to the terminal with the Program Status
// Protocol (OSC 7501: https://www.superlogical.com/rex/docs/build/program-status). Terminals that
// support it (Rex) show it on unfocused tabs; others ignore the sequence.
package termstatus

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
)

// maxMsg bounds the decoded msg (bytes); a terminal drops a report over its limits.
const maxMsg = 200

// Reporter writes status reports for one app. A nil Reporter does nothing.
type Reporter struct {
	w   io.Writer
	app string
	c   io.Closer
}

// New reports to w as app (characters [A-Za-z0-9_.,+/=-]).
func New(w io.Writer, app string) *Reporter { return &Reporter{w: w, app: app} }

// Open reports to the controlling terminal (/dev/tty), so reports reach it even when stdout/stderr
// are piped (tee, wrappers). nil, a no-op, when GCD_NO_STATUS is set, when errOut isn't a
// process stream (tests capture output in a buffer), or with no terminal (CI).
func Open(errOut io.Writer, app string) *Reporter {
	if _, ok := errOut.(*os.File); !ok || os.Getenv("GCD_NO_STATUS") != "" {
		return nil
	}
	f := color.OpenTTY()
	if f == nil {
		return nil
	}
	return &Reporter{w: f, app: app, c: f}
}

func (r *Reporter) Working(msg string) { r.report("working", msg) }

// Progress is Working with a percentage (clamped to 0..100).
func (r *Reporter) Progress(msg string, pct int) {
	r.report("working:progress="+fmt.Sprint(min(max(pct, 0), 100)), msg)
}
func (r *Reporter) Done(msg string)  { r.report("done", msg) }
func (r *Reporter) Error(msg string) { r.report("error", msg) }

// Idle: the user interrupted the command.
func (r *Reporter) Idle(msg string) { r.report("idle", msg) }

type ctxKey struct{}

// With returns ctx carrying r; From returns it (nil: none).
func With(ctx context.Context, r *Reporter) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

func From(ctx context.Context) *Reporter {
	r, _ := ctx.Value(ctxKey{}).(*Reporter)
	return r
}

// End leaves the final status of a command: done (ok), idle (interrupted: Ctrl+C) or error.
func (r *Reporter) End(ctx context.Context, err error, ok string) {
	switch {
	case err == nil:
		r.Done(ok)
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		r.Idle("Interrupted")
	default:
		r.Error(err.Error())
	}
}

func (r *Reporter) Close() {
	if r != nil && r.c != nil {
		r.c.Close()
	}
}

func (r *Reporter) report(state, msg string) {
	if r == nil {
		return
	}
	enc := base64.StdEncoding.EncodeToString([]byte(clean(msg)))
	fmt.Fprintf(r.w, "\x1b]7501;state=%s:app=%s:msg=%s\a", state, r.app, enc)
}

// clean keeps msg's first line, turns other control characters into spaces, drops escape sequences'
// ESC, and truncates to maxMsg bytes.
func clean(msg string) string {
	msg, _, _ = strings.Cut(msg, "\n")
	msg = strings.Map(func(c rune) rune {
		switch {
		case c == '\x1b':
			return -1
		case unicode.IsControl(c):
			return ' '
		}
		return c
	}, msg)
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxMsg {
		return msg
	}
	const ell = "…"
	cut := maxMsg - len(ell)
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + ell
}
