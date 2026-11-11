package cmd

import (
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/schemas"
	"github.com/spf13/cobra"
)

func init() {
	var rootDir string
	c := &cobra.Command{
		Use:   "schemas [out-dir]",
		Short: "Regenerate editor JSON Schemas (default platform/render/schemas)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootDir
			var err error
			if root == "" {
				if root, err = repo.Root(); err != nil {
					return err
				}
			} else if root, err = filepath.Abs(root); err != nil {
				return err
			}
			out := ""
			if len(args) == 1 {
				if out, err = filepath.Abs(args[0]); err != nil {
					return err
				}
			}
			return schemas.Generate(root, out, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
	register(c)
}
