// Package ghactions reads the GitHub Actions run gcd is in (event, base branch, PR labels) and writes
// workflow-command annotations, so CI calls `gcd lint` without picking flags or rewriting output.
package ghactions

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LintAllLabel on a pull request makes CI lint everything instead of only what changed.
const LintAllLabel = "lint-all"

// Run is the GitHub Actions run gcd is in.
type Run struct {
	Event   string   // GITHUB_EVENT_NAME: pull_request, push, workflow_dispatch, ...
	BaseRef string   // GITHUB_BASE_REF: the PR's target branch
	Labels  []string // the PR's labels, from the event payload
}

// Detect returns the run when GITHUB_ACTIONS is "true", else nil. getenv is os.Getenv in production.
func Detect(getenv func(string) string) (*Run, error) {
	if getenv("GITHUB_ACTIONS") != "true" {
		return nil, nil
	}
	r := &Run{Event: getenv("GITHUB_EVENT_NAME"), BaseRef: getenv("GITHUB_BASE_REF")}
	if r.Event != "pull_request" {
		return r, nil
	}
	p := getenv("GITHUB_EVENT_PATH")
	if p == "" {
		return r, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("github event payload: %w", err)
	}
	var ev struct {
		PullRequest struct {
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(b, &ev); err != nil {
		return nil, fmt.Errorf("github event payload %s: %w", p, err)
	}
	for _, l := range ev.PullRequest.Labels {
		r.Labels = append(r.Labels, l.Name)
	}
	return r, nil
}

// PullRequest: a pull_request run with a base branch.
func (r *Run) PullRequest() bool { return r != nil && r.Event == "pull_request" && r.BaseRef != "" }

// Labeled: the PR carries label.
func (r *Run) Labeled(label string) bool {
	if r == nil {
		return false
	}
	for _, l := range r.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// Error is an ::error workflow command for file:line:col (GitHub shows it on the PR diff).
func Error(file string, line, col int, msg string) string {
	return fmt.Sprintf("::error file=%s,line=%d,col=%d::%s", prop(file), line, col, data(msg))
}

// escaping per GitHub's workflow commands (toolkit/core command.ts)
func data(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func prop(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
