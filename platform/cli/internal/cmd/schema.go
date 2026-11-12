package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var rootDir string
		var asJSON bool
		c := &cobra.Command{
			Use:   "schema [kind]",
			Short: "Describe the capability YAML files: fields, types, rules, and a fixture example of each",
			Long: `Describe every input file kind (owners, stage, app, target, env-vars) from the schema lint enforces:
fields, types, required, allowed values, descriptions, and where the test fixture
(` + capability.FixtureRoot + `) uses each one. Give a kind to print only that one. --json prints the same as JSON.`,
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
				for i, d := range docs {
					if i > 0 {
						fmt.Fprintln(out)
					}
					fmt.Fprintf(out, "%s  %s  — %s\n", d.Kind, d.File, d.Desc)
					for _, f := range d.Fields {
						writeField(out, f, 1)
					}
				}
				return nil
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root, where the fixture examples are read (default: this repo)")
		c.Flags().BoolVar(&asJSON, "json", false, "JSON instead of text")
		return c
	})
}

// writeField prints f and what's under it, 2 spaces per depth.
func writeField(w io.Writer, f capability.FieldDoc, depth int) {
	line := strings.Repeat("  ", depth) + f.Name + "  " + f.Type
	if f.Required {
		line += "  required"
	}
	switch {
	case len(f.Enum) > 0:
		line += "  one of " + strings.Join(f.Enum, "|")
	case f.Pattern != "":
		line += "  /" + f.Pattern + "/"
	}
	if len(f.Platforms) > 0 {
		line += "  platforms " + strings.Join(f.Platforms, ",")
	}
	if f.Desc != "" {
		line += "  — " + f.Desc
	}
	if f.Example != "" {
		line += "  (e.g. " + f.Example + ")"
	}
	fmt.Fprintln(w, line)
	for _, c := range f.Fields {
		writeField(w, c, depth+1)
	}
	if f.Elem != nil {
		e := *f.Elem
		e.Name = "*"
		if f.Type == "list" {
			e.Name = "[]"
		}
		writeField(w, e, depth+1)
	}
}
