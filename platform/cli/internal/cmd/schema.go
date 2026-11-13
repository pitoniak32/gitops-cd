package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var asJSON, sources bool
		c := &cobra.Command{
			Use:   "schema [kind]",
			Short: "Describe the system YAML files as annotated YAML skeletons",
			Long: `Print every input file kind (owners, stage, app, target, env-vars) as a YAML skeleton from the
schema lint enforces: placeholders for types and allowed values (a|b), comments for required, pattern
and description. -s/--sources adds a fixture example per field (fixture: ` + system.FixtureRoot + `).
Give a kind to print only that one. --json prints the full schema as JSON.`,
			Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				pt, err := painter(cmd)
				if err != nil {
					return err
				}
				docs := system.Schemas(system.Default())
				if len(args) == 1 {
					var one []system.SchemaDoc
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
				ex, err := system.Examples(filepath.Join(root, filepath.FromSlash(system.FixtureRoot)))
				if err != nil {
					return err
				}
				docs = system.WithExamples(docs, ex)
				out := cmd.OutOrStdout()
				if asJSON {
					b, err := json.MarshalIndent(docs, "", "  ")
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(out, "%s\n", b)
					return err
				}
				_, err = fmt.Fprint(out, system.SchemaYAML(docs, sources, pt))
				return err
			},
		}
		c.Flags().BoolVar(&asJSON, "json", false, "JSON instead of YAML")
		c.Flags().BoolVarP(&sources, "sources", "s", false, "add a fixture example to each field comment")
		return c
	})
}
