package repo

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// DefaultBases are tried in order when no base is given: the remote main, then a local one.
var DefaultBases = []string{"origin/main", "main"}

// Changed lists files under root (relative to it, slash-separated) that differ between the merge-base
// of base and HEAD and the working tree, plus untracked files. Renames list both paths. With jj
// (colocated), HEAD is @- and the working tree is @, so @'s changes count too. base "" tries
// DefaultBases. Read-only git; returns the merge-base it compared against.
func Changed(root, base string) (files []string, mergeBase string, err error) {
	bases := DefaultBases
	if base != "" {
		bases = []string{base}
	}
	for _, b := range bases {
		if mergeBase, err = git(root, "merge-base", b, "HEAD"); err == nil {
			break
		}
	}
	if err != nil {
		return nil, "", fmt.Errorf("changed files: no merge-base with %s: %w", strings.Join(bases, " or "), err)
	}
	diff, err := git(root, "diff", "--name-only", "--no-renames", "--relative", mergeBase, "--", ".")
	if err != nil {
		return nil, "", err
	}
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard", "--", ".")
	if err != nil {
		return nil, "", err
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(diff+"\n"+untracked, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			seen[l] = true
		}
	}
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, mergeBase, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.quotePath=false"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
