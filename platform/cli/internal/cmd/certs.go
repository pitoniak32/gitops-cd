package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/certs"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(&cobra.Command{
		Use:   "certs",
		Short: "Browser-trusted TLS for *.localhost UIs: mkcert CA in macOS keychain + traefik default cert",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repo.Root()
			if err != nil {
				return err
			}
			hub, err := kube.ForContext(dns.HubContext)
			if err != nil {
				return err
			}
			return certs.Run(cmd.Context(), root, hub, certs.ExecRunner, cmd.ErrOrStderr())
		},
	})
}
