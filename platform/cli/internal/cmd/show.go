package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var rootDir, stage, target, base string
		var asJSON, sources, rendered, diff, platform bool
		c := &cobra.Command{
			Use:   "show <cap>[/<app>]",
			Short: "Show a capability's effective config per deploy and the file:line each value comes from",
			Long: `Print the merged config of every deploy (app, stage, target) of a capability: each value with the
file:line that sets it (or the default it falls back to), plus the effective owners. Edit the file shown.
Narrow with <cap>/<app>, --stage, --target. The capability must lint clean. --json prints the same as JSON.

--rendered prints the rendered manifests of the selected deploys instead (apps/<cap>/<app>/<stage>/<target>/*),
each under a "# rendered/<path>" line. --include-platform adds their Argo CD params and Kargo Stage.
--diff does the same against --base (jj revset, default @-; git rev, default HEAD): changed files as a
unified diff; unchanged files print nothing. --diff needs --root (default this repo) to be a jj or git repo;
selection is validated against the working tree, so a deploy removed there is not shown.
Nothing is written to rendered/. --json, --sources, --rendered and --diff exclude each other.`,
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				capName, app, _ := strings.Cut(args[0], "/")
				pt, err := painter(cmd)
				if err != nil {
					return err
				}
				root := rootDir
				if root == "" {
					if root, err = repo.Root(); err != nil {
						return err
					}
				} else if root, err = filepath.Abs(root); err != nil {
					return err
				}
				if rendered && diff {
					return fmt.Errorf("use one of --rendered, --diff")
				}
				if (rendered || diff) && (asJSON || sources) {
					return fmt.Errorf("--json and --sources cannot be combined with --rendered or --diff")
				}
				if platform && !rendered && !diff {
					return fmt.Errorf("--include-platform needs --rendered or --diff")
				}
				res, err := capability.Show(capability.Default(), root, capName, nil, capability.ShowFilter{App: app, Stage: stage, Target: target})
				if err != nil {
					var de capability.Error
					if errors.As(err, &de) {
						for _, d := range de {
							fmt.Fprintln(cmd.ErrOrStderr(), d.String())
						}
						return fmt.Errorf("%s: %d problem(s); fix them first (gcd lint %s)", capName, len(de), capName)
					}
					return err
				}
				out := cmd.OutOrStdout()
				if rendered || diff {
					text, err := showRendered(root, capName, res, platform, diff, base)
					if err != nil {
						var de capability.Error
						if errors.As(err, &de) {
							for _, d := range de {
								fmt.Fprintln(cmd.ErrOrStderr(), d.String())
							}
							return fmt.Errorf("%s: %d problem(s); fix them first (gcd lint %s)", capName, len(de), capName)
						}
						return err
					}
					_, err = fmt.Fprint(out, render.ColorDiff(text, pt))
					return err
				}
				if asJSON {
					b, err := json.MarshalIndent(res, "", "  ")
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(out, "%s\n", b)
					return err
				}
				_, err = fmt.Fprint(out, res.YAML(sources, pt))
				return err
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
		c.Flags().StringVar(&stage, "stage", "", "only this stage")
		c.Flags().StringVar(&target, "target", "", "only this target")
		c.Flags().BoolVar(&asJSON, "json", false, "JSON instead of YAML")
		c.Flags().BoolVarP(&sources, "sources", "s", false, "comment each value with the file:line (or default) it comes from")
		c.Flags().BoolVar(&rendered, "rendered", false, "print the rendered manifests of the selected deploys")
		c.Flags().BoolVar(&diff, "diff", false, "like --rendered, but changed files as a diff against --base (root must be a jj/git repo)")
		c.Flags().StringVar(&base, "base", "", "with --diff: jj revset (default @-) or git rev (default HEAD)")
		c.Flags().BoolVar(&platform, "include-platform", false, "with --rendered/--diff: also the Argo CD params and Kargo Stage of each deploy")
		return c
	})
}

// showRendered renders capName in memory and returns the selected deploys' files (or, with diff,
// their diff against base; unchanged files omitted).
func showRendered(root, capName string, res capability.ShowResult, platform, diff bool, base string) (string, error) {
	picks := make([]render.Pick, 0, len(res.Deploys))
	for _, d := range res.Deploys {
		picks = append(picks, render.Pick{App: d.App, Stage: d.Stage, Target: d.Target})
	}
	cfg := capability.Default()
	now, err := render.RenderCap(cfg, root, capName)
	if err != nil {
		return "", err
	}
	cur := render.SelectFiles(now, capName, picks, platform)
	if !diff {
		return render.FormatFiles(cur), nil
	}
	id, err := repo.ResolveBase(root, base)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "gcd-show-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := repo.ExportBase(root, id, tmp, "capabilities", "teams"); err != nil {
		return "", err
	}
	was, err := render.RenderCap(cfg, tmp, capName)
	if err != nil {
		return "", fmt.Errorf("base: %w", err)
	}
	return render.FormatSelectedDiff(render.SelectFiles(was, capName, picks, platform), cur), nil
}
