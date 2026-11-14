package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/tokenminter"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		return &cobra.Command{
			Use:   "test",
			Short: "gofmt, go vet and go test for platform/cli and the github-actions-token minter (what CI runs); needs Go (.mise.toml)",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
				if err := goChecks(cmd.Context(), filepath.Join(root, "platform/cli"), true, out, errOut); err != nil {
					return err
				}
				// the token minter's own module (ADR 0020): built into its own image, so it isn't part of gcd
				return goChecks(cmd.Context(), filepath.Join(root, tokenminter.Dir), false, out, errOut)
			},
		}
	})
}

// goChecks runs the Go checks in dir: gofmt (no file may need formatting), vet, every test, and with
// docs the repo docs test uncached (it reads docs/ outside the module, which go test's cache doesn't track).
func goChecks(ctx context.Context, dir string, docs bool, out, errOut io.Writer) error {
	var fmtOut bytes.Buffer
	if err := run(ctx, dir, &fmtOut, errOut, "gofmt", "-l", "."); err != nil {
		return err
	}
	if s := strings.TrimSpace(fmtOut.String()); s != "" {
		return fmt.Errorf("gofmt needed:\n%s", s)
	}
	cmds := [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}}
	if docs {
		cmds = append(cmds, []string{"go", "test", "-count=1", "-run", "TestRepoDocs", "./internal/docs"})
	}
	for _, args := range cmds {
		if err := run(ctx, dir, out, errOut, args...); err != nil {
			return err
		}
	}
	return nil
}

func run(ctx context.Context, dir string, out, errOut io.Writer, args ...string) error {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir, c.Stdout, c.Stderr, c.Env = dir, out, errOut, os.Environ()
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}
