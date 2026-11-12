package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var rootDir, base string
		var stat bool
		var caps []string
		c := &cobra.Command{
			Use:   "diff",
			Short: "Show how the working tree changes the rendered output against a base revision (read-only)",
			Long: `Renders the changed capabilities from the working tree and from --base (default @-) in memory and prints
the unified diff of rendered/ files. Changed = capabilities/ or teams/ files differing from --base (a teams/
change selects every capability). Nothing is written to rendered/. Prints nothing when nothing changed.`,
			Args: cobra.NoArgs,
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
				id, err := repo.ResolveOne(root, base)
				if err != nil {
					return err
				}
				tmp, err := os.MkdirTemp("", "gcd-diff-")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmp)
				if err := repo.Export(root, id, tmp, "capabilities", "teams"); err != nil {
					return err
				}
				if len(caps) > 0 {
					all := map[string]bool{}
					for _, r := range []string{tmp, root} {
						for _, n := range capability.Names(r) {
							all[n] = true
						}
					}
					for _, c := range caps {
						if !all[c] {
							names := make([]string, 0, len(all))
							for n := range all {
								names = append(names, n)
							}
							sort.Strings(names)
							return fmt.Errorf("unknown capability %s; allowed: %s", c, strings.Join(names, ", "))
						}
					}
				}
				if len(caps) == 0 {
					if caps, err = render.ChangedCaps(tmp, root); err != nil {
						return err
					}
				}
				if len(caps) == 0 {
					return nil
				}
				d, err := render.Diff(capability.Default(), tmp, root, caps)
				if err != nil {
					cmd.SilenceErrors = true // diagnostics are the output
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					return fmt.Errorf("diff failed")
				}
				if len(d) == 0 {
					return nil
				}
				if stat {
					fmt.Fprint(cmd.OutOrStdout(), render.Stat(d))
				} else {
					fmt.Fprint(cmd.OutOrStdout(), render.Unified(d))
				}
				return nil
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
		c.Flags().StringVar(&base, "base", "@-", "jj revset of the base revision (exactly one commit)")
		c.Flags().BoolVar(&stat, "stat", false, "per-file +added -removed line counts instead of the diff")
		c.Flags().StringArrayVar(&caps, "capability", nil, "diff only this capability (repeatable; overrides change detection)")
		return c
	})
}
