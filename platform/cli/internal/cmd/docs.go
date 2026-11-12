package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/docs"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var out, format string
		c := &cobra.Command{
			Use:   "docs",
			Short: "Write docs/reference from the schema, then check docs/guide yaml examples",
			Long: `Write one reference page per input file kind (owners, stage, app, target, env-vars) to --out from
the schema lint enforces, each field linked to its first use in the fixture (` + capability.FixtureRoot + `).
Then lint every yaml block in ` + docs.GuideDir + `/*.md: a fence must say path=<file> (lints clean),
path=<file> expect="<text>" (lint reports a matching error) or fragment (not checked). Each block
is written over its own copy of the fixture. Problems print like gcd lint; exit 1 if any.`,
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
				dir := out
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(root, dir)
				}
				ex, err := capability.Examples(filepath.Join(root, filepath.FromSlash(capability.FixtureRoot)))
				if err != nil {
					return err
				}
				pages := capability.SchemaMarkdown(capability.WithExamples(capability.Schemas(capability.Default()), ex))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				names := make([]string, 0, len(pages))
				for n := range pages {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					if err := os.WriteFile(filepath.Join(dir, n), []byte(pages[n]), 0o644); err != nil {
						return err
					}
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "docs: wrote %d reference pages to %s\n", len(names), out)
				diags, err := docs.Check(root)
				if err != nil {
					return err
				}
				if err := writeDiags(cmd.OutOrStdout(), diags, github, asJSON); err != nil {
					return err
				}
				if len(diags) > 0 {
					cmd.SilenceErrors = true // the diagnostics are the output; exit 1 only
					return fmt.Errorf("docs: %d problem(s) in %s", len(diags), docs.GuideDir)
				}
				return nil
			},
		}
		c.Flags().StringVar(&out, "out", "docs/reference", "reference output dir, relative to --root unless absolute")
		c.Flags().StringVar(&format, "format", "auto", "diagnostics as text (file:line:col: message), github (::error annotations) or json (array of diagnostics); auto: github in GitHub Actions")
		return c
	})
}
