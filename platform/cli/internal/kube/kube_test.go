package kube

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestApplyYAMLMultiDoc(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	var patches []clienttesting.PatchAction
	dyn.PrependReactor("patch", "*", func(a clienttesting.Action) (bool, runtime.Object, error) {
		patches = append(patches, a.(clienttesting.PatchAction))
		return true, nil, nil
	})
	c := &Clients{Dyn: dyn, Mapper: mapper}
	y := "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: foo\n---\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n  namespace: foo\ndata: {a: b}\n"
	if err := c.ApplyYAML(context.Background(), []byte(y), "gcd"); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("patches = %d, want 2", len(patches))
	}
	if patches[0].GetNamespace() != "" || patches[0].GetName() != "foo" {
		t.Errorf("namespace patch: %q/%q", patches[0].GetNamespace(), patches[0].GetName())
	}
	if patches[1].GetNamespace() != "foo" || patches[1].GetName() != "cm" {
		t.Errorf("cm patch: %q/%q", patches[1].GetNamespace(), patches[1].GetName())
	}
}
