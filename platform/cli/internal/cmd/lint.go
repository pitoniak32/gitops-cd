package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/capability"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/ghactions"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/lint"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/progress"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/spf13/cobra"
)

// getenv reads the environment (GitHub Actions detection); tests replace it.
var getenv = os.Getenv

func init() {
	register(func() *cobra.Command {
		var rootDir, base, format string
		var changed, everything, noProgress bool
		c := &cobra.Command{
			Use:   "lint [caps...]",
			Short: "Validate capabilities (+ teams/) with the full rules: <file>:<line>:<col>: <message>",
			Long: `Validate capabilities in parallel. Default: every capability and teams/.

--changed lints only what the changes since the merge-base with --base (default origin/main, else main)
can affect, including uncommitted and untracked files: changed capabilities; for a changed teams/ file,
teams/ and the capabilities whose owners.yml names it; everything when platform/cli changed. Names that
would collide across capabilities (Kargo projects, Applications) are checked over all capabilities
every time.

--all lints everything and overrides --changed, --base and named capabilities.

In GitHub Actions (GITHUB_ACTIONS=true), with none of those given: a pull_request lints --changed against
origin/<base branch>, or everything when the PR has the label "` + ghactions.LintAllLabel + `"; any other event
lints everything. Diagnostics are then ::error annotations (--format auto).`,
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
				gh, err := ghactions.Detect(getenv)
				if err != nil {
					return err
				}
				github := false
				switch format {
				case "auto":
					github = gh != nil
				case "github":
					github = true
				case "text":
				default:
					return fmt.Errorf("--format: want auto, text or github, got %q", format)
				}
				note := func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "lint: "+f+"\n", a...) }

				explicit := changed || base != "" || everything || len(args) > 0
				if !explicit && gh.PullRequest() {
					if gh.Labeled(ghactions.LintAllLabel) {
						note("GitHub pull_request labeled %s: everything", ghactions.LintAllLabel)
						everything = true
					} else {
						changed, base = true, "origin/"+gh.BaseRef
					}
				}

				s := lint.Scope{Teams: true, Caps: args}
				switch {
				case everything:
					if changed || base != "" || len(args) > 0 {
						note("--all: everything (overrides --changed, --base, named capabilities)")
					}
					s.Caps = capability.Names(root)
				case changed && len(args) > 0:
					return errors.New("--changed picks the capabilities; don't name any")
				case !changed && base != "":
					return errors.New("--base needs --changed")
				case changed:
					files, mb, err := repo.Changed(root, base)
					if err != nil {
						return err
					}
					var all bool
					s, all = lint.Changed(root, files)
					what := strings.Join(s.Caps, ", ")
					switch {
					case all:
						what = "everything (platform/cli changed)"
					case len(s.Caps) == 0 && !s.Teams:
						what = "no capabilities"
					case len(s.Caps) == 0:
						what = "teams/"
					case s.Teams:
						what = "teams/, " + what
					}
					note("changes since %.12s: %s", mb, what)
				case len(args) == 0:
					s.Caps = capability.Names(root)
				}
				// print what was located before any error that couldn't be (it must not hide them)
				diags, err := lint.Run(capability.Default(), root, s, progress.Auto(stderrFile(cmd), noProgress))
				for _, d := range diags {
					if github {
						msg := d.Msg
						if d.Label != "" {
							msg = d.Label + ": " + msg
						}
						fmt.Fprintln(cmd.OutOrStdout(), ghactions.Error(d.File, d.Line, d.Col, msg))
						continue
					}
					fmt.Fprintln(cmd.OutOrStdout(), d)
				}
				if err != nil {
					return err
				}
				if len(diags) > 0 {
					cmd.SilenceErrors = true // the diagnostics are the output; exit 1 only
					return fmt.Errorf("lint failed: %d problems", len(diags))
				}
				return nil
			},
		}
		c.Flags().StringVar(&rootDir, "root", "", "repo root (default: this repo)")
		c.Flags().BoolVar(&everything, "all", false, "lint everything; overrides --changed, --base and named capabilities")
		c.Flags().BoolVar(&changed, "changed", false, "lint only what changed since the merge-base with --base")
		c.Flags().StringVar(&base, "base", "", "git revision to diff against with --changed (default: origin/main, else main)")
		c.Flags().StringVar(&format, "format", "auto", "diagnostics as text (file:line:col: message) or github (::error annotations); auto: github in GitHub Actions")
		c.Flags().BoolVar(&noProgress, "no-progress", false, "no progress bars (they show only when stderr is a terminal)")
		return c
	})
}
