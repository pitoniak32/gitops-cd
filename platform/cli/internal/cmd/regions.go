package cmd

import (
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "regions",
			Short: "Regenerate region-derived committed config from the cluster registry",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
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
