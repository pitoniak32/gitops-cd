package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// vcs reports which VCS owns root: jj (a .jj dir, colocated or not) wins over git.
func vcs(root string) (string, error) {
	if st, err := os.Stat(filepath.Join(root, ".jj")); err == nil && st.IsDir() {
		return "jj", nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return "git", nil
	}
	return "", fmt.Errorf("%s: not a jj or git repo", root)
}

// DefaultBases are tried in order for the git default base: the remote main, then a local one.
var DefaultBases = []string{"origin/main", "main"}

// forkPoint is the jj revset of where the working copy's branch left trunk.
const forkPoint = "fork_point(trunk() | @)"

// ResolveBase resolves rev to one commit id, used as given: a jj revset (exactly one commit) or a git
// rev. "" is the branch fork point (DefaultBase).
func ResolveBase(root, rev string) (string, error) {
	k, err := vcs(root)
	if err != nil {
		return "", err
	}
	if rev == "" {
		return defaultBase(root, k)
	}
	if k == "jj" {
		return ResolveOne(root, rev)
	}
	out, err := gitOut(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	id := strings.TrimSpace(string(out))
	if err != nil || id == "" {
		return "", fmt.Errorf("unknown git revision %q (not a commit)", rev)
	}
	return id, nil
}

// ResolveForkBase is ResolveBase for "changes of this branch": the fork point of rev and the working
// copy (jj: fork_point(rev | @); git: merge-base rev HEAD). "" is the default base, the same fork
// point of trunk (jj) or origin/main, else main (git).
func ResolveForkBase(root, rev string) (string, error) {
	k, err := vcs(root)
	if err != nil {
		return "", err
	}
	if rev == "" {
		return defaultBase(root, k)
	}
	if k == "jj" {
		return ResolveOne(root, "fork_point(("+rev+") | @)")
	}
	out, err := gitOut(root, "merge-base", rev, "HEAD")
	if err != nil {
		return "", fmt.Errorf("no merge-base with %s: %w", rev, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// defaultBase: the branch fork point. jj: fork_point(trunk() | @). git: merge-base of HEAD and the
// first of DefaultBases that exists.
func defaultBase(root, kind string) (string, error) {
	if kind == "jj" {
		return ResolveOne(root, forkPoint)
	}
	for _, b := range DefaultBases {
		if out, err := gitOut(root, "merge-base", b, "HEAD"); err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}
	return "", fmt.Errorf("no merge-base of HEAD with %s", strings.Join(DefaultBases, " or "))
}

// ChangedFiles lists the files (root-relative, slash-separated, sorted) that differ between commit
// baseID and the working tree, untracked ones included, ignored ones not. jj snapshots the working
// copy first (@ is the tree); git: the working tree against baseID plus untracked files.
func ChangedFiles(root, baseID string) ([]string, error) {
	k, err := vcs(root)
	if err != nil {
		return nil, err
	}
	var out string
	if k == "jj" {
		b, err := jjSnapshot(root, "diff", "--name-only", "--from", baseID, "--to", "@")
		if err != nil {
			return nil, err
		}
		out = string(b)
	} else {
		diff, err := gitOut(root, "diff", "--name-only", "--no-renames", "--relative", baseID, "--", ".")
		if err != nil {
			return nil, err
		}
		untracked, err := gitOut(root, "ls-files", "--others", "--exclude-standard", "--", ".")
		if err != nil {
			return nil, err
		}
		out = string(diff) + "\n" + string(untracked)
	}
	seen := map[string]bool{}
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			files = append(files, filepath.ToSlash(l))
		}
	}
	sort.Strings(files)
	return files, nil
}

// ExportBase writes the files under prefixes at commit id into dst/<path>, with jj or git. Read-only.
func ExportBase(root, id, dst string, prefixes ...string) error {
	k, err := vcs(root)
	if err != nil {
		return err
	}
	if k == "jj" {
		return Export(root, id, dst, prefixes...)
	}
	args := []string{"ls-tree", "-r", "-z", "--name-only", id, "--"}
	args = append(args, prefixes...)
	out, err := gitOut(root, args...)
	if err != nil {
		return err
	}
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		data, err := gitOut(root, "cat-file", "blob", id+":"+p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// gitOut runs read-only git in root and returns raw stdout.
func gitOut(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.quotePath=false"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
