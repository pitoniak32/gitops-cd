package argocd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// HelmInstallIfMissing installs the addon's chart (repo/chart/version from its AppSet) unless the release
// exists. No wait (hook-only strategy): the root app only needs the CRDs. Values: valueFiles merged, then
// extra, then (region != "") the AppSet valuesObject with {{.values.region}} substituted.
func HelmInstallIfMissing(ctx context.Context, kubeCtx, ns, release string, root string, addon string, valueFiles []string, extra map[string]any, region string) error {
	repo, chart, version, valuesObject, err := AppSetChart(root, addon)
	if err != nil {
		return err
	}
	for i, f := range valueFiles {
		if !filepath.IsAbs(f) {
			valueFiles[i] = filepath.Join(root, f)
		}
	}

	settings := cli.New()
	flags := genericclioptions.NewConfigFlags(true)
	flags.Context = &kubeCtx
	flags.Namespace = &ns
	cfg := new(action.Configuration)
	if err := cfg.Init(flags, ns, "secret"); err != nil {
		return fmt.Errorf("helm init: %w", err)
	}

	if _, err := action.NewStatus(cfg).Run(release); err == nil {
		return nil // release exists
	} else if !errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("helm status %s: %w", release, err)
	}

	vals, err := (&values.Options{ValueFiles: valueFiles}).MergeValues(getter.All(settings))
	if err != nil {
		return fmt.Errorf("helm values: %w", err)
	}
	merge(vals, extra)
	if region != "" {
		merge(vals, RegionValues(valuesObject, region))
	}

	inst := action.NewInstall(cfg)
	inst.ReleaseName = release
	inst.Namespace = ns
	inst.CreateNamespace = true
	inst.WaitStrategy = kube.HookOnlyStrategy
	inst.RepoURL = repo
	inst.Version = version
	path, err := inst.LocateChart(chart, settings)
	if err != nil {
		return fmt.Errorf("locate chart %s %s: %w", chart, version, err)
	}
	ch, err := loader.Load(path)
	if err != nil {
		return fmt.Errorf("load chart %s: %w", path, err)
	}
	if _, err := inst.RunWithContext(ctx, ch, vals); err != nil {
		return fmt.Errorf("helm install %s: %w", release, err)
	}
	fmt.Fprintf(os.Stderr, "helm installed %s/%s %s in %s\n", ns, release, version, kubeCtx)
	return nil
}

// merge deep-merges src into dst (maps recurse, everything else replaces).
func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}
