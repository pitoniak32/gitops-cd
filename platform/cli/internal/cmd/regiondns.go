package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(&cobra.Command{
		Use:   "region-dns",
		Short: "Regions: resolve keycloak.localhost to the hub traefik (CoreDNS rewrite + selectorless Service, idempotent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repo.Root()
			if err != nil {
				return err
			}
			return dns.Regions(cmd.Context(), root, cmd.ErrOrStderr())
		},
	})
}
