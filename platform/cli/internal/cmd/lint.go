package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
		var base, format string
		var changed, everything, noProgress, asJSON bool
		c := &cobra.Command{
			Use:   "lint [caps...]",
			Short: "Validate capabilities (+ teams/) with the full rules: <file>:<line>:<col>: <message>",
			Long: `Validate capabilities in parallel. Default: every capability and teams/.

--changed lints only what the changes since the branch fork point can affect (--base: jj revset or git rev
to fork from, via fork_point(base | @) in jj and merge-base in git; default: trunk() in jj, origin/main
else main in git), including uncommitted and untracked files: changed capabilities; for a changed teams/ file,
teams/ and the capabilities whose owners.yml names it; everything when platform/cli changed. Names that
would collide across capabilities (Kargo projects, Applications) are checked over all capabilities
every time.

--all lints everything and overrides --changed, --base and named capabilities.

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
				github := false
				switch format {
				case "auto":
					github = gh != nil
				case "github":
					github = true
				case "text":
				case "json":
					asJSON = true
				default:
					return fmt.Errorf("--format: want auto, text, github or json, got %q", format)
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
				switch {
				case asJSON:
					if diags == nil {
						diags = []capability.Diag{}
					}
					b, merr := json.MarshalIndent(diags, "", "  ")
					if merr != nil {
						return merr
					}
					fmt.Fprintln(cmd.OutOrStdout(), string(b))
				default:
					printDiags(cmd.OutOrStdout(), diags, github)
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
		c.Flags().BoolVar(&everything, "all", false, "lint everything; overrides --changed, --base and named capabilities")
		c.Flags().BoolVar(&changed, "changed", false, "lint only what changed since the merge-base with --base")
		c.Flags().StringVar(&base, "base", "", baseHelp)
		c.Flags().StringVar(&format, "format", "auto", "diagnostics as text (file:line:col: message), github (::error annotations) or json (array of diagnostics); auto: github in GitHub Actions")
		c.Flags().BoolVar(&noProgress, "no-progress", false, "no progress bars (they show only when stderr is a terminal)")
		return c
	})
}
