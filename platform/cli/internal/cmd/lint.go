package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/lint"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	var rootDir string
	c := &cobra.Command{
		Use:   "lint [caps...]",
		Short: "Validate capabilities (+ teams/) with the full rules: <file>:<line>:<col>: <message>",
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
			// print what was located before any error that couldn't be (it must not hide them)
			diags, err := lint.All(root, args)
			for _, d := range diags {
				fmt.Fprintln(cmd.OutOrStdout(), d)
			}
			if err != nil {
				return err
			}
			if len(diags) > 0 {
				cmd.SilenceErrors = true // the diagnostics are the output; exit 1 only
				return errors.New("lint failed")
			}
			return nil
		},
	}
	c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
	register(c)
}
