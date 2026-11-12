package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cluster"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "down",
			Short: "Delete demo clusters (hub + regions)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := repo.Root()
				if err != nil {
					return err
				}
				rs, err := regions.List(root)
				if err != nil {
					return err
				}
				return cluster.Down(cmd.Context(), root, rs, cmd.ErrOrStderr())
			},
		}
	})
}
