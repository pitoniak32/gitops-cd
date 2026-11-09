package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitServing(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 2 {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	if err := waitServing(context.Background(), srv.URL, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("requests = %d", n.Load())
	}
}

func TestWaitServingTimeout(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	if err := waitServing(context.Background(), srv.URL, 100*time.Millisecond); err == nil {
		t.Fatal("want timeout error")
	}
}
