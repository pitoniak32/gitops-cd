package argocd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	releaseutil "helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// HelmInstallIfMissing installs the addon's chart (repo/chart/version from its AppSet) unless the release
// exists. No wait (hook-only strategy): the root app only needs the CRDs. Values: valueFiles merged, then
// extra, then (region != "") the AppSet valuesObject with {{.values.region}} substituted.
func HelmInstallIfMissing(ctx context.Context, kubeCtx, ns, release string, root string, addon string, valueFiles []string, extra map[string]any, region string, log io.Writer) error {
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

	exists, status := true, ""
	rel, err := action.NewStatus(cfg).Run(release)
	switch {
	case errors.Is(err, driver.ErrReleaseNotFound):
		exists = false
	case err != nil:
		return fmt.Errorf("helm status %s: %w", release, err)
	default:
		acc, aerr := releaseutil.NewAccessor(rel)
		if aerr != nil {
			return fmt.Errorf("helm status %s: %w", release, aerr)
		}
		status = acc.Status()
	}
	switch decideInstall(exists, status) {
	case actSkip:
		return nil
	case actReinstall:
		// a failed hook or interrupted install leaves a release with no resources; a skip would hang later
		fmt.Fprintf(log, "helm release %s/%s is %s, uninstalling before reinstall\n", ns, release, status)
		un := action.NewUninstall(cfg)
		un.WaitStrategy = kube.HookOnlyStrategy
		un.Timeout = HelmTimeout
		if _, err := un.Run(release); err != nil {
			return fmt.Errorf("helm uninstall %s: %w", release, err)
		}
	}

	vals, err := (&values.Options{ValueFiles: valueFiles}).MergeValues(getter.All(settings))
	if err != nil {
		return fmt.Errorf("helm values: %w", err)
	}
	merge(vals, extra)
	if region != "" {
		merge(vals, RegionValues(valuesObject, region))
	}

	inst := newInstall(cfg, ns, release, repo, version)
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
	fmt.Fprintf(log, "helm installed %s/%s %s in %s\n", ns, release, version, kubeCtx)
	return nil
}

type installAction int

const (
	actInstall   installAction = iota // no release
	actSkip                           // deployed
	actReinstall                      // present but not deployed (failed, pending, ...)
)

func decideInstall(exists bool, status string) installAction {
	switch {
	case !exists:
		return actInstall
	case status == "deployed":
		return actSkip
	default:
		return actReinstall
	}
}

const defaultHelmTimeout = 5 * time.Minute

// HelmTimeout bounds the install, including pre-install hooks (argo-cd's redis-secret-init Job) which
// HookOnlyStrategy still waits for. Same as the helm CLI's --timeout default.
var HelmTimeout = defaultHelmTimeout

func newInstall(cfg *action.Configuration, ns, release, repo, version string) *action.Install {
	inst := action.NewInstall(cfg)
	inst.ReleaseName = release
	inst.Namespace = ns
	inst.CreateNamespace = true
	inst.WaitStrategy = kube.HookOnlyStrategy
	inst.Timeout = HelmTimeout
	inst.RepoURL = repo
	inst.Version = version
	return inst
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
