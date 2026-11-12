package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "regions",
			Short: "Regenerate region-derived committed config from the cluster registry",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				root, err := repo.Root()
				if err != nil {
					return err
				}
				cfg, err := regions.Load(root)
				if err != nil {
					return err
				}
				return regions.Write(root, cfg)
			},
		}
	})
}
