package kube

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
)

func memCached(d discovery.DiscoveryInterface) discovery.CachedDiscoveryInterface {
	return memory.NewMemCacheClient(d)
}

func metaPatchOptions(fm string, force bool) metav1.PatchOptions {
	return metav1.PatchOptions{FieldManager: fm, Force: &force}
}
