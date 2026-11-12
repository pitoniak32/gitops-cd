package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// jj runs read-only jj in root. --ignore-working-copy: never snapshot or mutate the working copy.
func jj(root string, args ...string) ([]byte, error) {
	full := append([]string{"-R", root, "--ignore-working-copy", "--no-pager", "--color", "never"}, args...)
	cmd := exec.Command("jj", full...)
	cmd.Dir = root // file list prints cwd-relative paths; root is the workspace root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("jj %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// ResolveOne resolves revset to exactly one commit id.
func ResolveOne(root, revset string) (string, error) {
	out, err := jj(root, "log", "-r", revset, "--no-graph", "-T", `commit_id ++ "\n"`)
	if err != nil {
		return "", err
	}
	var ids []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ids = append(ids, l)
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("revset %s resolves to %d commits; want exactly 1", revset, len(ids))
	}
	return ids[0], nil
}

// filesetPath quotes a repo-root-relative path as a jj fileset prefix.
func filesetPath(p string) string {
	p = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filepath.ToSlash(p))
	return `root:"` + p + `"`
}

// Export writes the files under prefixes at commitID into dst/<path>. Prefixes absent at that
// revision yield no files. Read-only.
func Export(root, commitID, dst string, prefixes ...string) error {
	args := []string{"file", "list", "-r", commitID, "-T", `path.display() ++ "\n"`, "--"}
	for _, p := range prefixes {
		args = append(args, filesetPath(p))
	}
	out, err := jj(root, args...)
	if err != nil {
		return err
	}
	for _, p := range strings.Split(string(out), "\n") {
		if p == "" {
			continue
		}
		data, err := jj(root, "file", "show", "-r", commitID, "--", filesetPath(p))
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
