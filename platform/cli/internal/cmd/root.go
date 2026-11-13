// Package cmd defines the gcd command tree.
package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/termstatus"
	"github.com/spf13/cobra"
)

var subcommands []func() *cobra.Command

// register adds a subcommand constructor (called from init in each command file). NewRoot calls it, so
// every root gets fresh commands and flag variables (tests execute several roots in one process).
func register(f func() *cobra.Command) { subcommands = append(subcommands, f) }

// NewRoot builds the gcd root command.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "gcd",
		Short:         "gitops-cd cluster setup CLI",
		Long:          "gcd: gitops-cd cluster setup, rendering and checks. Build it with `mise run build` (.bin/gcd, on PATH via mise).",
		SilenceUsage:  true,
		SilenceErrors: false,
		// every command uses the project-local kubeconfig (never ~/.kube/config or a $KUBECONFIG of
		// other clusters); `gcd env` prints it for kubectl
		PersistentPreRun: func(*cobra.Command, []string) {
			if r, err := repo.Root(); err == nil {
				os.Setenv("KUBECONFIG", kubeconfigFor(r))
			}
		},
	}
	root.PersistentFlags().String("color", "auto", "color human output: auto (terminal and no NO_COLOR), always, never")
	root.PersistentFlags().String("root", "", "repo root (default: this repo); sandbox: platform/render/testdata/root")
	for _, f := range subcommands {
		c := f()
		if done, ok := statusCommands[c.Name()]; ok {
			withStatus(c, done)
		}
		root.AddCommand(c)
	}
	return root
}

// statusCommands report to the terminal tab (OSC 7501, e.g. Rex) while they run: command -> message
// when done. The long ones: validation, rendering, cluster setup.
var statusCommands = map[string]string{
	"lint": "Lint clean", "render": "Rendered", "docs": "Docs up to date",
	"up": "Ready: " + kargoURL, "down": "Clusters deleted", "infra": "Infrastructure ready",
	"certs": "Certificates ready", "creds": "Credentials applied", "kargo-restore": "Kargo state restored",
	"kargo-shards": "Shard tokens handed over", "region-dns": "Region DNS ready",
}

// withStatus wraps c's RunE: report working while it runs (phases come from progress trackers and
// termstatus.From(ctx)), then done, error, or idle on Ctrl+C. App: gcd-<name>.
func withStatus(c *cobra.Command, done string) {
	run := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		st := termstatus.Open(cmd.ErrOrStderr(), "gcd-"+cmd.Name())
		defer st.Close()
		st.Working(strings.TrimSpace("gcd " + cmd.Name() + " " + strings.Join(args, " ")))
		ctx := cmd.Context()
		cmd.SetContext(termstatus.With(ctx, st))
		err := run(cmd, args)
		st.End(ctx, err, done)
		return err
	}
}

// painter resolves the root --color flag for cmd's stdout.
func painter(cmd *cobra.Command) (color.P, error) {
	mode, _ := cmd.Flags().GetString("color")
	return color.Detect(mode, cmd.OutOrStdout())
}

// resolveRoot is the root --root flag as an absolute path, else the repo root.
func resolveRoot(cmd *cobra.Command) (string, error) {
	if r, _ := cmd.Flags().GetString("root"); r != "" {
		return filepath.Abs(r)
	}
	return repo.Root()
}

// baseHelp describes --base (lint --changed, show --diff): the default is the branch fork point.
const baseHelp = "jj revset (exactly one commit) or git rev; default: where this branch left trunk (jj: fork_point(trunk() | @); git: merge-base of HEAD and origin/main, else main)"
