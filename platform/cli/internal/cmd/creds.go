package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/creds"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/spf13/cobra"
)

func init() {
	register(&cobra.Command{
		Use:   "creds",
		Short: "Kargo git credentials (GitHub App) + GitHub Actions token (PAT) from fnox, shared by all projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hub, err := kube.ForContext(dns.HubContext)
			if err != nil {
				return err
			}
			return creds.Run(cmd.Context(), hub, creds.FnoxGetter, cmd.ErrOrStderr())
		},
	})
}
