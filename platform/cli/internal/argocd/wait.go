package argocd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// Timing knobs (vars so tests can shorten them).
var (
	PollInterval   = 2 * time.Second
	ExistTimeout   = 600 * time.Second // 300 polls x 2s in the bash original
	RefreshTimeout = 120 * time.Second // 60 polls x 2s
	SyncTimeout    = 600 * time.Second
	HealthTimeout  = 600 * time.Second
)

const refreshAnnotation = "argocd.argoproj.io/refresh"

func poll(ctx context.Context, every, timeout time.Duration, what string, f func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		ok, err := f()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s", what)
		case <-t.C:
		}
	}
}

// WaitApp waits until the Application exists (ApplicationSets create them asynchronously), forces a
// refresh, then waits for Synced + Healthy.
func WaitApp(ctx context.Context, c *kube.Clients, name string, log io.Writer) error {
	apps := c.Dyn.Resource(AppGVR).Namespace(Namespace)
	get := func() (*unstructured.Unstructured, error) {
		u, err := apps.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return u, err
	}

	err := poll(ctx, PollInterval, ExistTimeout, "app/"+name, func() (bool, error) {
		u, err := get()
		return u != nil, err
	})
	if err != nil {
		return fmt.Errorf("app/%s never created in %s (check its ApplicationSet status): %w", name, c.Ctx, err)
	}
	fmt.Fprintf(log, "app/%s exists in %s\n", name, c.Ctx)

	// force compare against the current spec, else the wait sees a stale Synced status
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:"normal"}}}`, refreshAnnotation)
	if _, err := apps.Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("refresh app/%s: %w", name, err)
	}
	// a timeout here is not fatal (bash: loop just ends)
	_ = poll(ctx, PollInterval, RefreshTimeout, "refresh of app/"+name, func() (bool, error) {
		u, err := get()
		if err != nil {
			return false, err
		}
		return u == nil || u.GetAnnotations()[refreshAnnotation] == "", nil
	})

	for _, w := range []struct {
		field []string
		want  string
		d     time.Duration
	}{
		{[]string{"status", "sync", "status"}, "Synced", SyncTimeout},
		{[]string{"status", "health", "status"}, "Healthy", HealthTimeout},
	} {
		fmt.Fprintf(log, "waiting app/%s %s=%s\n", name, w.field[len(w.field)-2], w.want)
		err := poll(ctx, PollInterval, w.d, fmt.Sprintf("app/%s %v=%s", name, w.field, w.want), func() (bool, error) {
			u, err := get()
			if err != nil || u == nil {
				return false, err
			}
			v, _, _ := unstructured.NestedString(u.Object, w.field...)
			return v == w.want, nil
		})
		if err != nil {
			return fmt.Errorf("app/%s in %s: %w", name, c.Ctx, err)
		}
	}
	return nil
}
