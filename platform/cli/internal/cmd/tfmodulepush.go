package cmd

import (
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/tfmodule"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "tf-module-push [version]",
			Short: "Push the example terraform module (examples/terraform/bucket) to ghcr; skips an existing tag",
			Long: `Optional demo step (ADR 0021): the Warehouse of payments/infra discovers these tags.
Pushes ` + tfmodule.Repo + `:<version> (default 1.0.0) with the gh CLI token, which needs write:packages
(gh auth refresh -h github.com -s write:packages). An existing tag is never re-pushed.`,
			Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				version := "1.0.0"
				if len(args) == 1 {
					version = args[0]
				}
				dst, err := tfmodule.Remote(cmd.Context(), tfmodule.Repo)
				if err != nil {
					return err
				}
				return tfmodule.Push(cmd.Context(), dst, tfmodule.Repo, version,
					filepath.Join(root, "examples/terraform/bucket"), cmd.OutOrStdout())
			},
		}
	})
}
