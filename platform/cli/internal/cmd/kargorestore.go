package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kargo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "kargo-restore [cap...]",
			Short: "Restore Kargo's record of what runs where after a cluster rebuild, from the env/* branches (idempotent)",
			RunE: func(cmd *cobra.Command, caps []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				hub, err := kube.ForContext(dns.HubContext)
				if err != nil {
					return err
				}
				return kargo.Restore(cmd.Context(), root, hub, caps, kargo.GHReader{}, cmd.OutOrStdout())
			},
		}
	})
}
