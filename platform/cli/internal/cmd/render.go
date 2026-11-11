package cmd

import (
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	var rootDir string
	var caps []string
	var withPlatform bool
	c := &cobra.Command{
		Use:   "render [out]",
		Short: "Render every capability into rendered/ (or out) and regenerate .github/CODEOWNERS",
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
			out := "rendered"
			if len(args) == 1 {
				out = args[0]
			}
			if out, err = filepath.Abs(out); err != nil {
				return err
			}
			if len(caps) == 0 && !withPlatform {
				return render.All(root, out, cmd.OutOrStdout())
			}
			// selected capabilities only: no wipe of others, no platform files/CODEOWNERS
			if err := render.Selected(root, caps, out, cmd.OutOrStdout()); err != nil {
				return err
			}
			if withPlatform {
				return render.Wrap(render.Platform(root, out))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&withPlatform, "platform", false, "also write the platform-level files: regions, promotion tasks (alone: only those)")
	c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
	c.Flags().StringArrayVar(&caps, "capability", nil, "render only this capability (repeatable; skips platform files/CODEOWNERS)")
	register(c)
}
