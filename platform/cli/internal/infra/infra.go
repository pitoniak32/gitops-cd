// Package infra bootstraps Argo CD on the hub and every region and waits for the add-ons in order.
package infra

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/argocd"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/certs"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/creds"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kargo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/logx"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"golang.org/x/sync/errgroup"
)

// HubWaitRest are the hub Applications waited for after kargo + creds, in order.
var HubWaitRest = []string{"cert-manager", "argo-rollouts", "keycloak", "kargo-config", "break-glass", "argocd"}

// Run bootstraps hub + regions. Regions need nothing from the hub to bootstrap their Argo CD (OIDC DNS only
// needs the hub container IP), so they run in parallel with the hub add-ons; only kargo-shards needs both.
// The first failure (hub or region) cancels the rest.
func Run(ctx context.Context, root string, out io.Writer) error {
	rs, err := regions.List(root)
	if err != nil {
		return err
	}
	hub, err := kube.ForContext(dns.HubContext)
	if err != nil {
		return err
	}
	regs := map[string]*kube.Clients{}
	for _, r := range rs {
		if regs[r], err = kube.ForContext("kind-region-" + r); err != nil {
			return err
		}
	}

	// keycloak.localhost -> traefik inside each cluster, so kargo-api reaches the same issuer URL as the browser
	if err := dns.Hub(ctx, hub, out); err != nil {
		return fmt.Errorf("hub dns: %w", err)
	}
	if err := dns.Regions(ctx, root, out); err != nil {
		return fmt.Errorf("region dns: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := runRegions(ctx, rs, func(ctx context.Context, r string) error {
			return bootstrapRegion(ctx, root, regs[r], r, logx.Prefix(out, "[region-"+r+"] "))
		})
		if err != nil {
			cancel()
		}
		done <- err
	}()

	hubErr := bootstrapHub(ctx, root, hub, logx.Prefix(out, "[hub] "))
	if hubErr != nil {
		cancel() // stop region jobs too
	}
	if err := bootstrapErr(<-done, hubErr); err != nil {
		return err
	}

	for _, r := range rs {
		// hub IngressRoute for https://argocd-<r>.localhost
		if err := argocd.WaitApp(ctx, hub, "argocd-ui-"+r, out); err != nil {
			return err
		}
	}
	// Kargo controller shard per region (hub identity, token handoff, regional controller)
	if err := kargo.Shards(ctx, root, hub, regs, out); err != nil {
		return fmt.Errorf("kargo shards: %w", err)
	}
	return nil
}

// bootstrapErr combines the region and hub results. Either side's failure cancels the other, whose error
// (often a cancel or timeout) then only follows the real cause: region first, then hub.
func bootstrapErr(regErr, hubErr error) error {
	if regErr != nil {
		regErr = fmt.Errorf("region bootstrap: %w", regErr)
	}
	if hubErr != nil {
		hubErr = fmt.Errorf("hub bootstrap: %w", hubErr)
	}
	return errors.Join(regErr, hubErr)
}

// runRegions runs fn for every region in parallel; the first error cancels ctx for the others and is returned.
func runRegions(ctx context.Context, rs []string, fn func(ctx context.Context, region string) error) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, r := range rs {
		g.Go(func() error {
			if err := fn(gctx, r); err != nil {
				return fmt.Errorf("region %s: %w", r, err)
			}
			return nil
		})
	}
	return g.Wait()
}

// bootstrapRegion: minimal Argo CD from the host (hub holds no region creds), then self-managed via root.
// Same values as the argocd-region AppSet: values.yaml + its valuesObject for this region.
func bootstrapRegion(ctx context.Context, root string, c *kube.Clients, r string, log io.Writer) error {
	if err := argocd.HelmInstallIfMissing(ctx, c.Ctx, argocd.Namespace, "argocd", root, "argocd-region",
		[]string{"platform/infrastructure/argocd-region/values.yaml"}, nil, r, log); err != nil {
		return err
	}
	if err := argocd.ApplyRoot(ctx, c, root, log); err != nil {
		return err
	}
	return argocd.WaitApp(ctx, c, "argocd", log)
}

// bootstrapHub installs Argo CD once (afterwards the `argocd` ApplicationSet manages it, ADR 0017), applies
// the root app and waits only for what host steps need, then checks the rest.
func bootstrapHub(ctx context.Context, root string, hub *kube.Clients, log io.Writer) error {
	// extraObjects: IngressRoute needs traefik CRDs; added later by the argocd app
	if err := argocd.HelmInstallIfMissing(ctx, hub.Ctx, argocd.Namespace, "argocd", root, "argocd",
		[]string{"platform/infrastructure/argocd/values.yaml"}, map[string]any{"extraObjects": []any{}}, "", log); err != nil {
		return err
	}
	if err := argocd.ApplyRoot(ctx, hub, root, log); err != nil {
		return err
	}
	if err := argocd.WaitApp(ctx, hub, "traefik", log); err != nil {
		return err
	}
	if err := certs.Run(ctx, root, hub, certs.DefaultCA(), log); err != nil { // browser-trusted TLS
		return fmt.Errorf("certs: %w", err)
	}
	if err := kargo.KickCertPods(ctx, hub, log); err != nil {
		return err
	}
	if err := argocd.WaitApp(ctx, hub, "kargo", log); err != nil {
		return err
	}
	if err := creds.Run(ctx, hub, creds.FnoxGetter, log); err != nil { // git creds for Kargo
		return fmt.Errorf("creds: %w", err)
	}
	for _, app := range HubWaitRest {
		if err := argocd.WaitApp(ctx, hub, app, log); err != nil {
			return err
		}
	}
	return nil
}
