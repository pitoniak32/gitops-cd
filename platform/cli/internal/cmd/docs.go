package cmd

import (
	"fmt"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/docs"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var format string
		c := &cobra.Command{
			Use:   "docs",
			Short: "Refresh embedded examples in docs/guide + docs/reference, then check their yaml",
			Long: `Maintain the hand-written pages in ` + docs.GuideDir + `, ` + docs.ReferenceDir + ` and ` + docs.EvaluateDir + `:

1. Rewrite each <!-- embed path=<file> [lines=A-B] --> … <!-- /embed --> block from that file of the
   fixture (` + capability.FixtureRoot + `): a fence of those lines, dedented, plus a source link.
2. Lint the fixture, so every embedded example is valid config.
3. Lint every other yaml block: a fence must say path=<file> (lints clean), path=<file> expect="<text>"
   (lint reports a matching error) or fragment (not checked). Each is written over its own fixture copy.

Problems print like gcd lint; exit 1 if any.`,
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				gh, err := inGitHubActions()
				if err != nil {
					return err
				}
				github, asJSON, err := diagFormat(format, gh)
				if err != nil {
					return err
				}
				changed, diags, err := docs.Run(root, true)
				if err != nil {
					return err
				}
				if len(changed) > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "docs: updated %d page(s)\n", len(changed))
				}
				if err := writeDiags(cmd.OutOrStdout(), diags, github, asJSON); err != nil {
					return err
				}
				if len(diags) > 0 {
					cmd.SilenceErrors = true // the diagnostics are the output; exit 1 only
					return fmt.Errorf("docs: %d problem(s)", len(diags))
				}
				return nil
			},
		}
		c.Flags().StringVar(&format, "format", "auto", "diagnostics as text (file:line:col: message), github (::error annotations) or json (array of diagnostics); auto: github in GitHub Actions")
		return c
	})
}
