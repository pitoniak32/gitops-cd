// Package cmd defines the gcd command tree.
package cmd

import "github.com/spf13/cobra"

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
	for _, f := range subcommands {
		root.AddCommand(f())
	}
	return root
}
