package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/certs"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "certs",
			Short: "Browser-trusted TLS for *.localhost UIs: local CA trusted in the macOS keychain + traefik default cert",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				hub, err := kube.ForContext(dns.HubContext)
				if err != nil {
					return err
				}
				return certs.Run(cmd.Context(), root, hub, certs.DefaultCA(), cmd.ErrOrStderr())
			},
		}
	})
}
