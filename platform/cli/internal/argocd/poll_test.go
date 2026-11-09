package argocd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPollParentCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := poll(ctx, time.Millisecond, time.Hour, "x", func() (bool, error) { return false, errors.New("flaky") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPollOwnTimeout(t *testing.T) {
	flaky := errors.New("flaky")
	err := poll(context.Background(), time.Millisecond, 20*time.Millisecond, "x", func() (bool, error) { return false, flaky })
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("own timeout must not look like parent cancel: %v", err)
	}
	if !strings.Contains(err.Error(), "timed out waiting for x") || !errors.Is(err, flaky) {
		t.Fatalf("err = %v", err)
	}
}
