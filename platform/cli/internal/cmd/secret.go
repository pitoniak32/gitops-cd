package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/secrets"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func init() {
	register(func() *cobra.Command {
		c := &cobra.Command{
			Use:   "secret",
			Short: "Local secrets in the macOS keychain (GitHub App) that gcd creds installs",
		}
		c.AddCommand(&cobra.Command{
			Use:   "set NAME",
			Short: "Store NAME: typed (hidden) on a terminal, else read from stdin (e.g. < key.pem)",
			Long:  "Store a secret in the keychain (service " + secrets.Service + "). Names gcd uses: " + strings.Join(secrets.Names, ", ") + ".",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				v, err := readSecret(cmd, args[0])
				if err != nil {
					return err
				}
				if v == "" {
					return errors.New("empty value; nothing stored")
				}
				if err := secrets.Default().Set(cmd.Context(), args[0], v); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s stored\n", args[0])
				return nil
			},
		})
		c.AddCommand(&cobra.Command{
			Use:   "list",
			Short: "Which secrets gcd uses are set (never prints values)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				missing := 0
				for _, n := range secrets.Names {
					state := "set"
					if _, err := secrets.Default().Get(cmd.Context(), n); errors.Is(err, secrets.ErrNotFound) {
						state, missing = "missing", missing+1
					} else if err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%-28s %s\n", n, state)
				}
				if missing > 0 {
					return fmt.Errorf("%d missing: gcd secret set NAME", missing)
				}
				return nil
			},
		})
		return c
	})
}

// readSecret prompts without echo when stdin is a terminal (one line), else reads all of stdin
// (multi-line values such as a PEM key), trimming one trailing newline.
func readSecret(cmd *cobra.Command, name string) (string, error) {
	if f, ok := cmd.InOrStdin().(*os.File); ok && color.IsTerminal(f) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		return string(b), err
	}
	b, err := io.ReadAll(cmd.InOrStdin())
	return strings.TrimSuffix(string(b), "\n"), err
}
