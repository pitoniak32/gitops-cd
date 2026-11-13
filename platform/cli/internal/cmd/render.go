package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/progress"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/termstatus"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var out string
		var withPlatform, everything, noProgress bool
		c := &cobra.Command{
			Use:   "render [sys...]",
			Short: "Render every system (or the named ones) into rendered/ (or --out) and regenerate .github/CODEOWNERS",
			Long: `Render every system, the platform files and CODEOWNERS into --out (default rendered).

Named systems render alone: no wipe of the others, no platform files, no CODEOWNERS; add --platform
for the platform files. --all renders everything and overrides named systems and --platform.
Validation errors print like gcd lint.`,
			RunE: func(cmd *cobra.Command, systems []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				if out, err = filepath.Abs(out); err != nil {
					return err
				}
				prog := progress.Auto(cmd.ErrOrStderr(), noProgress, termstatus.From(cmd.Context()))
				if everything && (len(systems) > 0 || withPlatform) {
					fmt.Fprintln(cmd.ErrOrStderr(), "render: --all: everything (overrides named systems, --platform)")
				}
				if everything || len(systems) == 0 && !withPlatform {
					return reportErr(cmd, render.All(root, out, cmd.OutOrStdout(), prog), "render")
				}
				// selected systems only: no wipe of others, no platform files/CODEOWNERS
				if err := render.Selected(root, systems, out, cmd.OutOrStdout(), prog); err != nil {
					return reportErr(cmd, err, "render")
				}
				if withPlatform {
					return render.Wrap(render.Platform(root, out))
				}
				return nil
			},
		}
		c.Flags().StringVar(&out, "out", "rendered", "output directory")
		c.Flags().BoolVar(&withPlatform, "platform", false, "also write the platform-level files: regions, promotion tasks (alone: only those)")
		c.Flags().BoolVar(&everything, "all", false, "render everything (the default); overrides named systems and --platform")
		c.Flags().BoolVar(&noProgress, "no-progress", false, "no progress bars (drawn on the terminal itself, never in redirected output)")
		return c
	})
}
