package argocd

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func fast(t *testing.T) {
	pi, et, rt, st, ht := PollInterval, ExistTimeout, RefreshTimeout, SyncTimeout, HealthTimeout
	PollInterval, ExistTimeout, RefreshTimeout, SyncTimeout, HealthTimeout = time.Millisecond, 200*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { PollInterval, ExistTimeout, RefreshTimeout, SyncTimeout, HealthTimeout = pi, et, rt, st, ht })
}

func newDyn() *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{AppGVR: "ApplicationList"})
}

func app(annotations map[string]any, sync, health string) *unstructured.Unstructured {
	md := map[string]any{"name": "root", "namespace": "argocd"}
	if annotations != nil {
		md["annotations"] = annotations
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": md,
		"status": map[string]any{"sync": map[string]any{"status": sync}, "health": map[string]any{"status": health}},
	}}
}

func TestWaitApp(t *testing.T) {
	fast(t)
	dyn := newDyn()
	var gets atomic.Int32
	var refreshed atomic.Bool
	dyn.PrependReactor("get", "applications", func(a clienttesting.Action) (bool, runtime.Object, error) {
		n := gets.Add(1)
		switch {
		case n <= 2:
			return false, nil, nil // fall through to tracker: not found
		case n == 3: // exists, refresh pending
			return true, app(map[string]any{refreshAnnotation: "normal"}, "OutOfSync", "Progressing"), nil
		case n == 4:
			return true, app(nil, "OutOfSync", "Progressing"), nil
		case n == 5:
			return true, app(nil, "Synced", "Progressing"), nil
		}
		return true, app(nil, "Synced", "Healthy"), nil
	})
	dyn.PrependReactor("patch", "applications", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if strings.Contains(string(a.(clienttesting.PatchAction).GetPatch()), refreshAnnotation) {
			refreshed.Store(true)
		}
		return true, app(nil, "Synced", "Healthy"), nil
	})
	var log bytes.Buffer
	if err := WaitApp(context.Background(), &kube.Clients{Ctx: "c", Dyn: dyn}, "root", &log); err != nil {
		t.Fatal(err)
	}
	if !refreshed.Load() {
		t.Error("refresh annotation not set")
	}
	if gets.Load() < 6 {
		t.Errorf("gets = %d, want >= 6", gets.Load())
	}
}

func TestWaitAppNeverCreated(t *testing.T) {
	fast(t)
	err := WaitApp(context.Background(), &kube.Clients{Ctx: "c", Dyn: newDyn()}, "root", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "never created") {
		t.Fatalf("err = %v", err)
	}
}

func TestRegionValues(t *testing.T) {
	in := map[string]any{
		"a": "x-{{.values.region}}",
		"b": map[string]any{"c": []any{"{{.values.region}}", 3, map[string]any{"d": "{{.values.region}}"}}},
	}
	out := RegionValues(in, "eu")
	want := map[string]any{
		"a": "x-eu",
		"b": map[string]any{"c": []any{"eu", 3, map[string]any{"d": "eu"}}},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("out = %v", out)
	}
	if in["a"] != "x-{{.values.region}}" || in["b"].(map[string]any)["c"].([]any)[0] != "{{.values.region}}" {
		t.Error("input mutated")
	}
}

func TestAppSetChart(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	repo, chart, ver, vo, err := AppSetChart(root, "argocd-region")
	if err != nil {
		t.Fatal(err)
	}
	if repo != "https://argoproj.github.io/argo-helm" || chart != "argo-cd" || ver != "10.14.0" {
		t.Errorf("got %s %s %s", repo, chart, ver)
	}
	g := vo["global"].(map[string]any)["domain"]
	if g != "argocd-{{.values.region}}.localhost" {
		t.Errorf("domain = %v", g)
	}
}
