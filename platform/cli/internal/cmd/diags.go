package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/ghactions"
	"github.com/spf13/cobra"
)

// printDiags writes diags one per line: text (file:line:col: message) or GitHub ::error annotations.
func printDiags(w io.Writer, diags []capability.Diag, github bool) {
	for _, d := range diags {
		if github {
			msg := d.Msg
			if d.Label != "" {
				msg = d.Label + ": " + msg
			}
			fmt.Fprintln(w, ghactions.Error(d.File, d.Line, d.Col, msg))
			continue
		}
		fmt.Fprintln(w, d)
	}
}

// inGitHubActions reports whether diagnostics should be annotations by default.
func inGitHubActions() (bool, error) {
	gh, err := ghactions.Detect(getenv)
	return gh != nil, err
}

// reportErr prints err's diagnostics to stderr exactly as lint does (annotations in GitHub Actions)
// and returns a short error that points at lint. An err without diagnostics is returned as is.
// what names the command's subject ("payments", "diff") in the short error.
func reportErr(cmd *cobra.Command, err error, what string) error {
	diags := capability.DiagsOf(err)
	if len(diags) == 0 {
		return err
	}
	gh, derr := inGitHubActions()
	if derr != nil {
		return derr
	}
	if strings.HasPrefix(err.Error(), "base: ") {
		fmt.Fprintln(cmd.ErrOrStderr(), "base revision:")
	}
	printDiags(cmd.ErrOrStderr(), diags, gh)
	return fmt.Errorf("%s: %d problem(s); fix them first (gcd lint)", what, len(diags))
}
