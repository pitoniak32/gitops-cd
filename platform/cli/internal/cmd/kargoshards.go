package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/dns"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kargo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "kargo-shards",
			Short: "Kargo controller shard per region: hub identity, token handoff, regional controller (idempotent)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
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
				return kargo.Shards(cmd.Context(), root, hub, regs, cmd.ErrOrStderr())
			},
		}
	})
}
