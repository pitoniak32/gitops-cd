package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "region-dns",
			Short: "Regions: resolve keycloak.localhost + otlp.localhost to the hub traefik (CoreDNS rewrite + selectorless Service, idempotent)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				return dns.Regions(cmd.Context(), root, cmd.ErrOrStderr())
			},
		}
	})
}
