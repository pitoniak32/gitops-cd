package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/infra"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(&cobra.Command{
		Use:   "infra",
		Short: "Bootstrap Argo CD + root app of ApplicationSets on hub and regions, wait for add-ons in order (idempotent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := repo.Root()
			if err != nil {
				return err
			}
			return infra.Run(cmd.Context(), root, cmd.ErrOrStderr())
		},
	})
}
