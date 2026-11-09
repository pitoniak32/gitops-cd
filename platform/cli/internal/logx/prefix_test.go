package logx

import (
	"bytes"
	"testing"
)

func TestPrefixWriter(t *testing.T) {
	var buf bytes.Buffer
	w := Prefix(&buf, "[hub] ")
	for _, s := range []string{"par", "tial\nline", " two\nthree\nfour\n", "\n"} {
		if n, err := w.Write([]byte(s)); err != nil || n != len(s) {
			t.Fatalf("write %q: n=%d err=%v", s, n, err)
		}
	}
	want := "[hub] partial\n[hub] line two\n[hub] three\n[hub] four\n[hub] \n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
