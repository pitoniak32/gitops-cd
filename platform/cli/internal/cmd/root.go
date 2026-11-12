// Package cmd defines the gcd command tree.
package cmd

import (
	"path/filepath"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
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
		Long:          "gcd: gitops-cd cluster setup CLI (replaces bash mise tasks).",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().String("color", "auto", "color human output: auto (terminal and no NO_COLOR), always, never")
	root.PersistentFlags().String("root", "", "repo root (default: this repo); sandbox: platform/render/testdata/root")
	for _, f := range subcommands {
		root.AddCommand(f())
	}
	return root
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
