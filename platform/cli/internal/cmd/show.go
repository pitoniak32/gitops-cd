package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var rootDir, stage, target string
		var asJSON bool
		c := &cobra.Command{
			Use:   "show <cap>[/<app>]",
			Short: "Show a capability's effective config per deploy and the file:line each value comes from",
			Long: `Print the merged config of every deploy (app, stage, target) of a capability: each value with the
file:line that sets it (or the default it falls back to), plus the effective owners. Edit the file shown.
Narrow with <cap>/<app>, --stage, --target. The capability must lint clean. --json prints the same as JSON.`,
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				capName, app, _ := strings.Cut(args[0], "/")
				root := rootDir
				var err error
				if root == "" {
					if root, err = repo.Root(); err != nil {
						return err
					}
				} else if root, err = filepath.Abs(root); err != nil {
					return err
				}
				res, err := capability.Show(capability.Default(), root, capName, nil, capability.ShowFilter{App: app, Stage: stage, Target: target})
				if err != nil {
					var de capability.Error
					if errors.As(err, &de) {
						for _, d := range de {
							fmt.Fprintln(cmd.ErrOrStderr(), d.String())
						}
						return fmt.Errorf("%s: %d problem(s); fix them first (gcd lint %s)", capName, len(de), capName)
					}
					return err
				}
				out := cmd.OutOrStdout()
				if asJSON {
					b, err := json.MarshalIndent(res, "", "  ")
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(out, "%s\n", b)
					return err
				}
				_, err = fmt.Fprint(out, res.Text())
				return err
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
		c.Flags().StringVar(&stage, "stage", "", "only this stage")
		c.Flags().StringVar(&target, "target", "", "only this target")
		c.Flags().BoolVar(&asJSON, "json", false, "JSON instead of text")
		return c
	})
}
