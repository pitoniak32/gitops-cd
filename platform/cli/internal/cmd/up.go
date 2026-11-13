package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cluster"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/infra"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/termstatus"
	"github.com/spf13/cobra"
)

const kargoURL = "https://kargo.localhost"

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "up",
			Short: "Fresh demo cluster: tear down existing, create, install infrastructure + rendered systems",
			Args:  cobra.NoArgs,
			// tab status: phases here, done/error from withStatus (root.go)
			RunE: func(cmd *cobra.Command, _ []string) error { return up(cmd, termstatus.From(cmd.Context())) },
		}
	})
}

func up(cmd *cobra.Command, st *termstatus.Reporter) error {
	ctx := cmd.Context()
	root, err := resolveRoot(cmd)
	if err != nil {
		return err
	}
	rs, err := regions.List(root)
	if err != nil {
		return err
	}
	log := cmd.ErrOrStderr()
	st.Working(fmt.Sprintf("Creating clusters: hub + %d regions", len(rs)))
	if err := cluster.Up(ctx, root, rs, log); err != nil {
		return err
	}
	st.Working("Installing infrastructure")
	if err := infra.Run(ctx, root, log); err != nil {
		return err
	}
	// wait for traefik to pick up the ingressroute
	st.Working("Waiting for the Kargo UI")
	if err := waitServing(ctx, kargoURL+"/", 120*time.Second); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "ready: "+kargoURL)
	return nil
}

// waitServing polls url every 2s until it answers 200 (TLS unverified: mkcert CA may be untrusted here).
func waitServing(ctx context.Context, url string, timeout time.Duration) error {
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // local demo
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s not serving", url)
		case <-time.After(2 * time.Second):
		}
	}
}
