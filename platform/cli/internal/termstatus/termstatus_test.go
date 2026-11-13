package termstatus

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestReports(t *testing.T) {
	var b bytes.Buffer
	r := New(&b, "gcd-up")
	r.Working("Creating clusters")
	r.Done("Ready")
	r.Error("boom")
	r.Idle("Interrupted")
	want := "\x1b]7501;state=working:app=gcd-up:msg=" + base64.StdEncoding.EncodeToString([]byte("Creating clusters")) + "\a" +
		"\x1b]7501;state=done:app=gcd-up:msg=" + base64.StdEncoding.EncodeToString([]byte("Ready")) + "\a" +
		"\x1b]7501;state=error:app=gcd-up:msg=" + base64.StdEncoding.EncodeToString([]byte("boom")) + "\a" +
		"\x1b]7501;state=idle:app=gcd-up:msg=" + base64.StdEncoding.EncodeToString([]byte("Interrupted")) + "\a"
	if b.String() != want {
		t.Fatalf("got %q\nwant %q", b.String(), want)
	}
}

// msg must be one line with no control characters, or the terminal drops the whole report.
func TestMsgSanitized(t *testing.T) {
	var b bytes.Buffer
	New(&b, "gcd-up").Error("first line\tx\nsecond line\x1b[31m")
	got := b.String()
	enc := strings.TrimSuffix(strings.SplitN(got, "msg=", 2)[1], "\a")
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != "first line x" {
		t.Fatalf("msg = %q", dec)
	}
}

func TestMsgTruncated(t *testing.T) {
	var b bytes.Buffer
	New(&b, "x").Working(strings.Repeat("é", 500))
	enc := strings.TrimSuffix(strings.SplitN(b.String(), "msg=", 2)[1], "\a")
	dec, _ := base64.StdEncoding.DecodeString(enc)
	if len(dec) > maxMsg || !strings.HasSuffix(string(dec), "…") {
		t.Fatalf("len %d, %q", len(dec), dec[len(dec)-8:])
	}
}

func TestNilIsNoop(t *testing.T) {
	var r *Reporter
	r.Working("x")
	r.Done("x")
	r.Close()
}

func TestEnd(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []struct {
		ctx  context.Context
		err  error
		want string
	}{
		{context.Background(), nil, "state=done"},
		{context.Background(), errors.New("kind create failed"), "state=error"},
		{cancelled, errors.New("signal: interrupt"), "state=idle"},
	} {
		var b bytes.Buffer
		New(&b, "gcd-up").End(c.ctx, c.err, "Ready")
		if !strings.Contains(b.String(), c.want) {
			t.Errorf("err %v: got %q, want %s", c.err, b.String(), c.want)
		}
	}
}

func TestProgress(t *testing.T) {
	var b bytes.Buffer
	r := New(&b, "gcd-lint")
	r.Progress("systems 3/10", 30)
	r.Progress("x", 150)
	if !strings.Contains(b.String(), "state=working:progress=30:app=gcd-lint:msg=") || !strings.Contains(b.String(), "progress=100:") {
		t.Fatalf("got %q", b.String())
	}
}

func TestContext(t *testing.T) {
	r := New(&bytes.Buffer{}, "x")
	if From(With(context.Background(), r)) != r || From(context.Background()) != nil {
		t.Fatal("context round trip")
	}
}
