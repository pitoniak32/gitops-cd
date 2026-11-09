// Package cmd defines the gcd command tree.
package cmd

import "github.com/spf13/cobra"

var subcommands []*cobra.Command

// register adds a subcommand to the root (called from init in each command file).
func register(c *cobra.Command) { subcommands = append(subcommands, c) }

// NewRoot builds the gcd root command.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "gcd",
		Short:         "gitops-cd cluster setup CLI",
		Long:          "gcd: gitops-cd cluster setup CLI (replaces bash mise tasks).",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(subcommands...)
	return root
}
