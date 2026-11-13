package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/ghactions"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/lint"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/progress"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/system"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/termstatus"
	"github.com/spf13/cobra"
)

// getenv reads the environment (GitHub Actions detection); tests replace it.
var getenv = os.Getenv

func init() {
	register(func() *cobra.Command {
		var base, format string
		var changed, everything, noProgress bool
		c := &cobra.Command{
			Use:   "lint [systems...]",
			Short: "Validate systems (+ teams/) with the full rules: <file>:<line>:<col>: <message>",
			Long: `Validate systems in parallel. Default: every system and teams/.

--changed lints only what the changes since the branch fork point can affect (--base: jj revset or git rev
to fork from, via fork_point(base | @) in jj and merge-base in git; default: trunk() in jj, origin/main
else main in git), including uncommitted and untracked files: changed systems; for a changed teams/ file,
teams/ and the systems whose owners.yml names it; everything when platform/cli changed. Names that
would collide across systems (Kargo projects, Applications) are checked over all systems
every time.

--all lints everything and overrides --changed, --base and named systems.

In GitHub Actions (GITHUB_ACTIONS=true), with none of those given: a pull_request lints --changed against
origin/<base branch>, or everything when the PR has the label "` + ghactions.LintAllLabel + `"; any other event
lints everything. Diagnostics are then ::error annotations (--format auto).`,
			RunE: func(cmd *cobra.Command, args []string) error {
				root, err := resolveRoot(cmd)
				if err != nil {
					return err
				}
				gh, err := ghactions.Detect(getenv)
				if err != nil {
					return err
				}
				github, asJSON, err := diagFormat(format, gh != nil)
				if err != nil {
					return err
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

				s := lint.Scope{Teams: true, Systems: args}
				switch {
				case everything:
					if changed || base != "" || len(args) > 0 {
						note("--all: everything (overrides --changed, --base, named systems)")
					}
					s.Systems = system.Names(root)
				case changed && len(args) > 0:
					return errors.New("--changed picks the systems; don't name any")
				case !changed && base != "":
					return errors.New("--base needs --changed")
				case changed:
					id, err := repo.ResolveForkBase(root, base)
					if err != nil {
						return err
					}
					files, err := repo.ChangedFiles(root, id)
					if err != nil {
						return err
					}
					mb := id
					var all bool
					s, all = lint.Changed(root, files)
					what := strings.Join(s.Systems, ", ")
					switch {
					case all:
						what = "everything (platform/cli changed)"
					case len(s.Systems) == 0 && !s.Teams:
						what = "no systems"
					case len(s.Systems) == 0:
						what = "teams/"
					case s.Teams:
						what = "teams/, " + what
					}
					note("changes since %.12s: %s", mb, what)
				case len(args) == 0:
					s.Systems = system.Names(root)
				}
				// print what was located before any error that couldn't be (it must not hide them)
				diags, err := lint.Run(system.Default(), root, s, progress.Auto(cmd.ErrOrStderr(), noProgress, termstatus.From(cmd.Context())))
				if werr := writeDiags(cmd.OutOrStdout(), diags, github, asJSON); werr != nil {
					return werr
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
		c.Flags().BoolVar(&everything, "all", false, "lint everything; overrides --changed, --base and named systems")
		c.Flags().BoolVar(&changed, "changed", false, "lint only what changed since the merge-base with --base")
		c.Flags().StringVar(&base, "base", "", baseHelp)
		c.Flags().StringVar(&format, "format", "auto", "diagnostics as text (file:line:col: message), github (::error annotations) or json (array of diagnostics); auto: github in GitHub Actions")
		c.Flags().BoolVar(&noProgress, "no-progress", false, "no progress bars (drawn on the terminal itself, never in redirected output)")
		return c
	})
}
