package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "env",
			Short: "Print shell exports for this repo's clusters: eval \"$(gcd env)\" (kubectl, k9s use .kubeconfig)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "export KUBECONFIG=%q\n", kubeconfigFor(root))
				return nil
			},
		}
	})
}

// kubeconfigFor is the project-local kubeconfig: gcd never touches ~/.kube/config.
func kubeconfigFor(root string) string { return filepath.Join(root, ".kubeconfig") }
