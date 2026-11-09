package infra

import (
	"context"
	"errors"
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
