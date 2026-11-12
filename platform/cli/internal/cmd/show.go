package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var stage, target, base, against string
		var asJSON, sources, rendered, diff, platform, stat bool
		c := &cobra.Command{
			Use:   "show [<cap>[/<app>]]",
			Short: "Show a capability's effective config per deploy and the file:line each value comes from",
			Long: `Print the merged config of every deploy (app, stage, target) of a capability: each value with the
file:line that sets it (or the default it falls back to), plus the effective owners. Edit the file shown.
Narrow with <cap>/<app>, --stage, --target. The capability must lint clean. --json prints the same as JSON.

--rendered prints the rendered manifests of the selected deploys instead (apps/<cap>/<app>/<stage>/<target>/*),
each under a "# rendered/<path>" line. --include-platform adds their Argo CD params and Kargo Stage.

--diff shows how the working tree changes the rendered output against --base, as a unified diff
(--stat: per-file +added -removed counts); unchanged files print nothing. Both sides are rendered with
the current gcd, so only input changes show. With a capability it covers the selected deploys; with none,
every capability whose files under capabilities/ or teams/ differ from --base (a teams/ change selects
all; deleted capabilities included) with all their rendered files. --base is a jj revset (exactly one
commit) or git rev, used as given; the default is where this branch left trunk (jj:
fork_point(trunk() | @); git: merge-base of HEAD and origin/main, else main), so the diff is the
branch's. The root must be a jj or git repo.
--diff --against <rev> instead compares a fresh in-memory render with the rendered/ tree committed at <rev>
(same rev syntax as --base; e.g. the rendered branch; CI uses it). A rev without rendered/ counts as empty.
With a capability: its selected deploys; with none: everything gcd render writes under rendered/ (files only
at <rev> show as removed). Excludes --base. Nothing is written to rendered/. --json, --sources, --rendered and --diff exclude each other.`,
			Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				pt, err := painter(cmd)
				if err != nil {
					return err
				}
				root, err := resolveRoot(cmd)
				if err != nil {
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
				if (stat || base != "" || against != "") && !diff {
					return fmt.Errorf("--base, --against and --stat need --diff")
				}
				if base != "" && against != "" {
					return fmt.Errorf("use one of --base, --against")
				}
				if len(args) == 0 {
					if !diff {
						return fmt.Errorf("a capability is required (only --diff may omit it)")
					}
					if stage != "" || target != "" {
						return fmt.Errorf("--stage and --target need a capability")
					}
					if against != "" {
						return showAllAgainst(cmd, root, against, stat, pt)
					}
					return showAllDiff(cmd, root, base, stat, pt)
				}
				capName, app, _ := strings.Cut(args[0], "/")
				if names := capability.Names(root); !contains(names, capName) {
					return fmt.Errorf("unknown capability %s; allowed: %s", capName, strings.Join(names, ", "))
				}
				res, err := capability.Show(capability.Default(), root, capName, nil, capability.ShowFilter{App: app, Stage: stage, Target: target})
				if err != nil {
					return reportErr(cmd, err, capName)
				}
				out := cmd.OutOrStdout()
				if rendered || diff {
					text, err := showRendered(root, capName, res, platform, diff, base, against, stat, pt)
					if err != nil {
						return reportErr(cmd, err, capName)
					}
					_, err = fmt.Fprint(out, text)
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
		c.Flags().StringVar(&stage, "stage", "", "only this stage")
		c.Flags().StringVar(&target, "target", "", "only this target")
		c.Flags().BoolVar(&asJSON, "json", false, "JSON instead of YAML")
		c.Flags().BoolVarP(&sources, "sources", "s", false, "comment each value with the file:line (or default) it comes from")
		c.Flags().BoolVar(&rendered, "rendered", false, "print the rendered manifests of the selected deploys")
		c.Flags().BoolVar(&diff, "diff", false, "like --rendered, but changed files as a diff against --base (capability optional: then all changed ones)")
		c.Flags().StringVar(&base, "base", "", "with --diff: "+baseHelp)
		c.Flags().StringVar(&against, "against", "", "with --diff: compare with the rendered/ tree at this rev (e.g. the rendered branch) instead of rendering a base; "+baseHelp)
		c.Flags().BoolVar(&stat, "stat", false, "with --diff: per-file +added -removed line counts instead of the diff")
		c.Flags().BoolVar(&platform, "include-platform", false, "with --rendered/--diff and a capability: also the Argo CD params and Kargo Stage of each deploy")
		return c
	})
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// withBase resolves base (see repo.ResolveBase), exports capabilities/ and teams/ at it into a temp
// directory, calls f with that directory as a config root, and removes it.
func withBase(root, base string, f func(baseRoot string) error) error {
	id, err := repo.ResolveBase(root, base)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "gcd-base-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := repo.ExportBase(root, id, tmp, "capabilities", "teams"); err != nil {
		return err
	}
	return f(tmp)
}

// withRendered resolves rev (see repo.ResolveBase), reads the rendered/ tree committed there (paths
// relative to rendered/; none: empty) and calls f with it.
func withRendered(root, rev string, f func(old map[string]string) error) error {
	id, err := repo.ResolveBase(root, rev)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "gcd-rendered-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := repo.ExportBase(root, id, tmp, "rendered"); err != nil {
		return err
	}
	old, err := render.ReadFiles(tmp, "rendered")
	if err != nil {
		return err
	}
	rel := make(map[string]string, len(old))
	for p, c := range old {
		rel[strings.TrimPrefix(p, "rendered/")] = c
	}
	return f(rel)
}

// showAllAgainst prints the diff of everything gcd render writes under rendered/ against the tree at rev.
func showAllAgainst(cmd *cobra.Command, root, rev string, stat bool, pt color.P) error {
	return withRendered(root, rev, func(old map[string]string) error {
		now, err := render.AllFiles(root)
		if err != nil {
			return reportErr(cmd, err, "diff")
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), diffText(render.SelectedDiffs(old, now), stat, pt))
		return err
	})
}

// diffText formats d as the unified diff, or the per-file counts with stat; "" when d is empty.
func diffText(d []render.FileDiff, stat bool, pt color.P) string {
	switch {
	case len(d) == 0:
		return ""
	case stat:
		return render.ColorStat(render.Stat(d), pt)
	}
	return render.ColorDiff(render.Unified(d), pt)
}

// showAllDiff prints the rendered diff of every capability changed since base.
func showAllDiff(cmd *cobra.Command, root, base string, stat bool, pt color.P) error {
	return withBase(root, base, func(baseRoot string) error {
		caps, err := render.ChangedCaps(baseRoot, root)
		if err != nil || len(caps) == 0 {
			return err
		}
		d, err := render.Diff(capability.Default(), baseRoot, root, caps)
		if err != nil {
			return reportErr(cmd, err, "diff")
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), diffText(d, stat, pt))
		return err
	})
}

// showRendered renders capName in memory and returns the selected deploys' files (or, with diff,
// their diff against base, or stat counts; unchanged files omitted). Text is colored for pt.
func showRendered(root, capName string, res capability.ShowResult, platform, diff bool, base, against string, stat bool, pt color.P) (string, error) {
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
		return render.ColorDiff(render.FormatFiles(cur), pt), nil
	}
	var text string
	if against != "" {
		err = withRendered(root, against, func(old map[string]string) error {
			text = diffText(render.SelectedDiffs(render.SelectFiles(old, capName, picks, platform), cur), stat, pt)
			return nil
		})
		return text, err
	}
	err = withBase(root, base, func(baseRoot string) error {
		was, err := render.RenderCap(cfg, baseRoot, capName)
		if err != nil {
			return fmt.Errorf("base: %w", err)
		}
		text = diffText(render.SelectedDiffs(render.SelectFiles(was, capName, picks, platform), cur), stat, pt)
		return nil
	})
	return text, err
}
