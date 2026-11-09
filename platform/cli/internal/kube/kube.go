// Package kube builds cluster clients and applies manifests (server-side apply).
package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// FieldManager is the server-side apply field manager used by gcd.
const FieldManager = "gcd"

// Clients bundles the clients for one kube context.
type Clients struct {
	Ctx    string
	Core   kubernetes.Interface
	Dyn    dynamic.Interface
	Mapper meta.RESTMapper
	Cfg    *rest.Config
}

// ForContext builds clients for the named context of the KUBECONFIG (env / default rules).
func ForContext(ctx string) (*Clients, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: ctx},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kube config for context %s: %w", ctx, err)
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube core client %s: %w", ctx, err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube dynamic client %s: %w", ctx, err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube discovery client %s: %w", ctx, err)
	}
	return &Clients{
		Ctx: ctx, Core: core, Dyn: dyn, Cfg: cfg,
		Mapper: restmapper.NewDeferredDiscoveryRESTMapper(memCached(disc)),
	}, nil
}

// ApplyYAML server-side applies every document of a (multi-doc) YAML stream, forcing conflicts.
// An empty fieldManager defaults to FieldManager.
func (c *Clients) ApplyYAML(ctx context.Context, yaml []byte, fieldManager string) error {
	if fieldManager == "" {
		fieldManager = FieldManager
	}
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(yaml), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := dec.Decode(&obj.Object); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode manifest: %w", err)
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := c.applyOne(ctx, obj, fieldManager); err != nil {
			return err
		}
	}
}

func (c *Clients) applyOne(ctx context.Context, obj *unstructured.Unstructured, fm string) error {
	gvk := obj.GroupVersionKind()
	m, err := c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("map %s: %w", gvk, err)
	}
	var ri dynamic.ResourceInterface
	if m.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "default"
		}
		ri = c.Dyn.Resource(m.Resource).Namespace(ns)
	} else {
		ri = c.Dyn.Resource(m.Resource)
	}
	data, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}
	force := true
	_, err = ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metaPatchOptions(fm, force))
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("apply %s/%s: not found (missing CRD or namespace?): %w", gvk.Kind, obj.GetName(), err)
		}
		return fmt.Errorf("apply %s/%s: %w", gvk.Kind, obj.GetName(), err)
	}
	return nil
}
