package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunParallelFailFast(t *testing.T) {
	boom := errors.New("boom")
	sawCancel := make(chan bool, 1)
	err := runRegions(context.Background(), []string{"bad", "good"}, func(ctx context.Context, r string) error {
		if r == "bad" {
			return boom
		}
		select {
		case <-ctx.Done():
			sawCancel <- true
		case <-time.After(5 * time.Second):
			sawCancel <- false
		}
		return ctx.Err()
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if !<-sawCancel {
		t.Fatal("other region did not see ctx cancelled")
	}
}

func TestRunRegionsOK(t *testing.T) {
	done := make(chan string, 2)
	if err := runRegions(context.Background(), []string{"a", "b"}, func(_ context.Context, r string) error {
		done <- r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(done) != 2 {
		t.Fatalf("ran %d regions", len(done))
	}
}

func TestBootstrapErr(t *testing.T) {
	reg := errors.New("region us: helm failed")
	hub := errors.New("timed out waiting for app/traefik")
	err := bootstrapErr(reg, hub)
	if !errors.Is(err, reg) || !errors.Is(err, hub) {
		t.Fatalf("want both: %v", err)
	}
	if s := err.Error(); strings.Index(s, reg.Error()) > strings.Index(s, hub.Error()) {
		t.Fatalf("region error must come first: %q", s)
	}
	if err := bootstrapErr(nil, hub); !errors.Is(err, hub) {
		t.Fatalf("hub-only: %v", err)
	}
	if err := bootstrapErr(reg, nil); !errors.Is(err, reg) {
		t.Fatalf("region-only: %v", err)
	}
	if err := bootstrapErr(nil, nil); err != nil {
		t.Fatalf("none: %v", err)
	}
}
