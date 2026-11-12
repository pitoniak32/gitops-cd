package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	"github.com/spf13/cobra"
)

// openURLs lists every UI URL, in display order.
func openURLs(rs []string) []string {
	urls := []string{"https://kargo.localhost", "https://argocd.localhost/applications"}
	for _, r := range rs {
		urls = append(urls, "https://argocd-"+r+".localhost/applications")
	}
	return append(urls, "http://keycloak.localhost/admin/master/console/")
}

func init() {
	register(func() *cobra.Command {
		var printOnly bool
		c := &cobra.Command{
			Use:   "open",
			Short: "Open every UI in the browser",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				rs, err := regions.List(root)
				if err != nil {
					return err
				}
				for _, u := range openURLs(rs) {
					fmt.Fprintln(cmd.OutOrStdout(), u)
					if !printOnly {
						oc := exec.Command("open", u)
						oc.Stderr = os.Stderr
						if err := oc.Run(); err != nil {
							return fmt.Errorf("open %s: %w", u, err)
						}
					}
				}
				return nil
			},
		}
		c.Flags().BoolVar(&printOnly, "print", false, "only print the URLs")
		return c
	})
}
