package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/render"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"github.com/spf13/cobra"
)

func init() {
	register(func() *cobra.Command {
		var stage, target, base, against string
		var files string
		var asJSON, sources, rendered, diff, stat bool
		c := &cobra.Command{
			Use:   "show [<sys>[/<app>]]",
			Short: "Show a system's effective config per deploy and the file:line each value comes from",
			Long: `Print the merged config of every deploy (app, stage, target) of a system: each value with the
file:line that sets it (or the default it falls back to), plus the effective owners. Edit the file shown.
Narrow with <sys>/<app>, --stage, --target. The system must lint clean. --json prints the same as JSON.

--rendered prints the rendered manifests of the selected deploys instead (apps/<sys>/<app>/<stage>/<target>/*),
each under a "# rendered/<path>" line. --files picks which: app (default; the workload files), platform
(their Argo CD params and Kargo Stage; with no system: every file outside apps/) or all.

--diff shows how the working tree changes the merged config (the plain gcd show output) against --base,
as a unified diff per system (--stat: per-system +added -removed counts); unchanged prints nothing.
--diff --rendered does the same for the rendered manifests (per rendered file; unchanged files omitted).
Both sides use the current gcd, so only input changes show. With a system it covers the selected
deploys; with none, every system whose files under systems/ or teams/ differ from --base (a teams/
change selects all; deleted systems included), all their deploys. --base is a jj revset (exactly one
commit) or git rev, used as given; the default is where this branch left trunk (jj:
fork_point(trunk() | @); git: merge-base of HEAD and origin/main, else main), so the diff is the
branch's. The root must be a jj or git repo.
--diff --rendered --against <rev> instead compares a fresh in-memory render with the rendered/ tree committed
at <rev> (same rev syntax as --base; e.g. the rendered branch; CI uses it). A rev without rendered/ counts as
empty. With a system: its selected deploys; with none: everything gcd render writes under rendered/ that --files
covers (files only at <rev> show as removed). Excludes --base. Nothing is written to rendered/.
--json and --sources exclude --rendered and --diff.`,
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
				if (rendered || diff) && (asJSON || sources) {
					return fmt.Errorf("--json and --sources cannot be combined with --rendered or --diff")
				}
				if cmd.Flags().Changed("files") && !rendered {
					return fmt.Errorf("--files needs --rendered")
				}
				sel, err := render.ParseFiles(files)
				if err != nil {
					return err
				}
				if (stat || base != "" || against != "") && !diff {
					return fmt.Errorf("--base, --against and --stat need --diff")
				}
				if against != "" && !rendered {
					return fmt.Errorf("--against needs --rendered (it compares with a committed rendered/ tree)")
				}
				if base != "" && against != "" {
					return fmt.Errorf("use one of --base, --against")
				}
				if len(args) == 0 {
					if !diff {
						return fmt.Errorf("a system is required (only --diff may omit it)")
					}
					if stage != "" || target != "" {
						return fmt.Errorf("--stage and --target need a system")
					}
					switch {
					case against != "":
						return showAllAgainst(cmd, root, against, sel, stat, pt)
					case rendered:
						return showAllDiff(cmd, root, base, sel, stat, pt)
					}
					return showAllConfigDiff(cmd, root, base, stat, pt)
				}
				sysName, app, _ := strings.Cut(args[0], "/")
				if names := system.Names(root); !contains(names, sysName) {
					return fmt.Errorf("unknown system %s; allowed: %s", sysName, strings.Join(names, ", "))
				}
				res, err := system.Show(system.Default(), root, sysName, nil, system.ShowFilter{App: app, Stage: stage, Target: target})
				if err != nil {
					return reportErr(cmd, err, sysName)
				}
				out := cmd.OutOrStdout()
				if diff && !rendered {
					f := system.ShowFilter{App: app, Stage: stage, Target: target}
					text, err := showConfigDiff(root, args[0], sysName, f, res, base, stat, pt)
					if err != nil {
						return reportErr(cmd, err, sysName)
					}
					_, err = fmt.Fprint(out, text)
					return err
				}
				if rendered {
					text, err := showRendered(root, sysName, res, sel, diff, base, against, stat, pt)
					if err != nil {
						return reportErr(cmd, err, sysName)
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
		c.Flags().BoolVar(&rendered, "rendered", false, "print the rendered manifests of the selected deploys (with --diff: diff them)")
		c.Flags().BoolVar(&diff, "diff", false, "diff the merged config (with --rendered: the rendered manifests) against --base (system optional: then all changed ones)")
		c.Flags().StringVar(&base, "base", "", "with --diff: "+baseHelp)
		c.Flags().StringVar(&against, "against", "", "with --diff --rendered: compare with the rendered/ tree at this rev (e.g. the rendered branch) instead of rendering a base; "+baseHelp)
		c.Flags().BoolVar(&stat, "stat", false, "with --diff: per-file +added -removed line counts instead of the diff")
		c.Flags().StringVar(&files, "files", "app", "with --rendered: app (workload files), platform (Argo CD, Kargo, ...: what app teams need not read) or all")
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

// withBase resolves base (see repo.ResolveBase), exports systems/ and teams/ at it into a temp
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
	if err := repo.ExportBase(root, id, tmp, "systems", "teams"); err != nil {
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

// showAllAgainst prints the diff of everything gcd render writes under rendered/ (that sel covers) against the tree at rev.
func showAllAgainst(cmd *cobra.Command, root, rev string, sel render.Files, stat bool, pt color.P) error {
	return withRendered(root, rev, func(old map[string]string) error {
		now, err := render.AllFiles(root)
		if err != nil {
			return reportErr(cmd, err, "diff")
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), diffText(sel.KeepDiffs(render.SelectedDiffs(old, now)), "rendered/", stat, pt))
		return err
	})
}

// diffText formats d as the unified diff (headers a/<prefix><path>), or the per-file counts with stat; "" when d is empty.
func diffText(d []render.FileDiff, prefix string, stat bool, pt color.P) string {
	switch {
	case len(d) == 0:
		return ""
	case stat:
		return render.ColorStat(render.Stat(d), pt)
	}
	return render.ColorDiff(render.UnifiedIn(d, prefix), pt)
}

// showAllDiff prints the rendered diff (files sel covers) of every system changed since base.
func showAllDiff(cmd *cobra.Command, root, base string, sel render.Files, stat bool, pt color.P) error {
	return withBase(root, base, func(baseRoot string) error {
		systems, err := render.ChangedSystems(baseRoot, root)
		if err != nil || len(systems) == 0 {
			return err
		}
		d, err := render.Diff(system.Default(), baseRoot, root, systems)
		if err != nil {
			return reportErr(cmd, err, "diff")
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), diffText(sel.KeepDiffs(d), "rendered/", stat, pt))
		return err
	})
}

// showRendered renders sysName in memory and returns the selected deploys' files (or, with diff,
// their diff against base, or stat counts; unchanged files omitted). Text is colored for pt.
func showRendered(root, sysName string, res system.ShowResult, sel render.Files, diff bool, base, against string, stat bool, pt color.P) (string, error) {
	picks := make([]render.Pick, 0, len(res.Deploys))
	for _, d := range res.Deploys {
		picks = append(picks, render.Pick{App: d.App, Stage: d.Stage, Target: d.Target})
	}
	cfg := system.Default()
	now, err := render.RenderSystem(cfg, root, sysName)
	if err != nil {
		return "", err
	}
	cur := render.SelectFiles(now, sysName, picks, sel)
	if !diff {
		return render.ColorDiff(render.FormatFiles(cur), pt), nil
	}
	var text string
	if against != "" {
		err = withRendered(root, against, func(old map[string]string) error {
			text = diffText(render.SelectedDiffs(render.SelectFiles(old, sysName, picks, sel), cur), "rendered/", stat, pt)
			return nil
		})
		return text, err
	}
	err = withBase(root, base, func(baseRoot string) error {
		was, err := render.RenderSystem(cfg, baseRoot, sysName)
		if err != nil {
			return fmt.Errorf("base: %w", err)
		}
		text = diffText(render.SelectedDiffs(render.SelectFiles(was, sysName, picks, sel), cur), "rendered/", stat, pt)
		return nil
	})
	return text, err
}

// configText is sysName's merged config at root as gcd show prints it (no sources, no color), filtered
// by f; "" when root lacks the system or the app, stage or target f names.
func configText(root, sysName string, f system.ShowFilter) (string, error) {
	if !contains(system.Names(root), sysName) {
		return "", nil
	}
	res, err := system.Show(system.Default(), root, sysName, nil, f)
	var unknown system.UnknownFilterError
	if errors.As(err, &unknown) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return res.YAML(false, color.P{}), nil
}

// showConfigDiff diffs the merged config of sysName (res: the working side, filtered by f) against base,
// labelled label (the <sys>[/<app>] argument).
func showConfigDiff(root, label, sysName string, f system.ShowFilter, res system.ShowResult, base string, stat bool, pt color.P) (string, error) {
	now := res.YAML(false, color.P{})
	var text string
	err := withBase(root, base, func(baseRoot string) error {
		was, err := configText(baseRoot, sysName, f)
		if err != nil {
			return fmt.Errorf("base: %w", err)
		}
		text = diffText(render.SelectedDiffs(map[string]string{label: was}, map[string]string{label: now}), "", stat, pt)
		return nil
	})
	return text, err
}

// showAllConfigDiff prints the merged config diff of every system changed since base.
func showAllConfigDiff(cmd *cobra.Command, root, base string, stat bool, pt color.P) error {
	return withBase(root, base, func(baseRoot string) error {
		systems, err := render.ChangedSystems(baseRoot, root)
		if err != nil || len(systems) == 0 {
			return err
		}
		was, now := map[string]string{}, map[string]string{}
		for _, sys := range systems {
			if now[sys], err = configText(root, sys, system.ShowFilter{}); err != nil {
				return reportErr(cmd, err, sys)
			}
			if was[sys], err = configText(baseRoot, sys, system.ShowFilter{}); err != nil {
				return reportErr(cmd, fmt.Errorf("base: %w", err), sys)
			}
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), diffText(render.SelectedDiffs(was, now), "", stat, pt))
		return err
	})
}
