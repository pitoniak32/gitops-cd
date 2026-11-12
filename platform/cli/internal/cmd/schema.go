package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var rootDir string
		var asJSON, sources bool
		c := &cobra.Command{
			Use:   "schema [kind]",
			Short: "Describe the capability YAML files as annotated YAML skeletons",
			Long: `Print every input file kind (owners, stage, app, target, env-vars) as a YAML skeleton from the
schema lint enforces: placeholders for types and allowed values (a|b), comments for required, pattern
and description. -s/--sources adds a fixture example per field (fixture: ` + capability.FixtureRoot + `).
Give a kind to print only that one. --json prints the full schema as JSON.`,
			Args: cobra.MaximumNArgs(1),
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
				pt, err := painter(cmd)
				if err != nil {
					return err
				}
				docs := capability.Schemas(capability.Default())
				if len(args) == 1 {
					var one []capability.SchemaDoc
					for _, d := range docs {
						if d.Kind == args[0] {
							one = append(one, d)
						}
					}
					if len(one) == 0 {
						var kinds []string
						for _, d := range docs {
							kinds = append(kinds, d.Kind)
						}
						return fmt.Errorf("unknown kind %q: want one of %s", args[0], strings.Join(kinds, ", "))
					}
					docs = one
				}
				ex, err := capability.Examples(filepath.Join(root, filepath.FromSlash(capability.FixtureRoot)))
				if err != nil {
					return err
				}
				docs = capability.WithExamples(docs, ex)
				out := cmd.OutOrStdout()
				if asJSON {
					b, err := json.MarshalIndent(docs, "", "  ")
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(out, "%s\n", b)
					return err
				}
				_, err = fmt.Fprint(out, capability.SchemaYAML(docs, sources, pt))
				return err
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root, where the fixture examples are read (default: this repo)")
		c.Flags().BoolVar(&asJSON, "json", false, "JSON instead of YAML")
		c.Flags().BoolVarP(&sources, "sources", "s", false, "add a fixture example to each field comment")
		return c
	})
}
