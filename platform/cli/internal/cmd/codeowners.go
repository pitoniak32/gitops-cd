package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var rootDir, outDir string
		c := &cobra.Command{
			Use:   "codeowners",
			Short: "Regenerate .github/CODEOWNERS from owners files + CODEOWNERS.tmpl",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := rootDir, error(nil)
				if root == "" {
					if root, err = repo.Root(); err != nil {
						return err
					}
				}
				if outDir != "" {
					// fragments already rendered under outDir/codeowners
					if err := render.CodeOwners(root, outDir); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "wrote .github/CODEOWNERS")
					return nil
				}
				tmp, err := os.MkdirTemp("", "gcd-codeowners")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmp)
				// owner fragments render to a temp dir; rendered/ is untouched (ADR 0002)
				if err := render.Fragments(root, tmp, io.Discard, nil); err != nil {
					return render.Wrap(err)
				}
				if err := render.CodeOwners(root, tmp); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "wrote .github/CODEOWNERS")
				return nil
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
		c.Flags().StringVar(&outDir, "out", "", "use fragments already rendered under <out>/codeowners (default: render to a temp dir)")
		return c
	})
}
