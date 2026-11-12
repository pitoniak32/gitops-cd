package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// ResolveBase resolves rev to one commit id: a jj revset (default @-) or a git rev (default HEAD).
func ResolveBase(root, rev string) (string, error) {
	k, err := vcs(root)
	if err != nil {
		return "", err
	}
	if k == "jj" {
		if rev == "" {
			rev = "@-"
		}
		return ResolveOne(root, rev)
	}
	if rev == "" {
		rev = "HEAD"
	}
	out, err := gitOut(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	id := strings.TrimSpace(string(out))
	if err != nil || id == "" {
		return "", fmt.Errorf("unknown git revision %q (not a commit)", rev)
	}
	return id, nil
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
