// Package localimage builds the platform's own images (static Go binaries on scratch, never pushed) and
// loads them into the hub kind cluster: the github-actions-token minter (ADR 0020) and promotion-log
// (ADR 0032). Each tag is a hash of its sources, pinned in its manifest (TestManifestTags checks it).
package localimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	kind "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
)

// Image is one locally built image.
type Image struct {
	// Dir holds the Go module + Dockerfile, relative to the repo root.
	Dir string
	// Repo is the image name; only ever loaded into kind, never pushed.
	Repo string
	// Manifest pins the tag, relative to Dir.
	Manifest string
}

// Images are every local image, built and loaded by `gcd infra`; `gcd test` runs each Dir's Go checks.
var Images = []Image{
	{Dir: "platform/infrastructure/github-actions-token", Repo: "gitops-cd/github-actions-token", Manifest: "manifests/minter.yaml"},
	{Dir: "platform/infrastructure/promotion-log", Repo: "gitops-cd/promotion-log", Manifest: "manifests/promotion-log.yaml"},
}

// Sources are the files an image is built from (the build context minus .dockerignore), in hash order.
var Sources = []string{".dockerignore", "Dockerfile", "go.mod", "main.go"}

// Ref is the image reference for the current sources: Repo:<first 12 hex of their sha256>.
func (i Image) Ref(root string) (string, error) {
	h := sha256.New()
	for _, f := range Sources {
		b, err := os.ReadFile(filepath.Join(root, i.Dir, f))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(b))
		h.Write(b)
	}
	return i.Repo + ":" + hex.EncodeToString(h.Sum(nil))[:12], nil
}

// Load builds and loads every image. Idempotent.
func Load(ctx context.Context, root, cluster string, log io.Writer) error {
	for _, i := range Images {
		if err := i.Load(ctx, root, cluster, log); err != nil {
			return fmt.Errorf("%s: %w", i.Repo, err)
		}
	}
	return nil
}

// Load builds the image if docker doesn't have it yet and loads it into every node of the kind cluster
// that doesn't have it yet. Idempotent.
func (i Image) Load(ctx context.Context, root, cluster string, log io.Writer) error {
	ref, err := i.Ref(root)
	if err != nil {
		return err
	}
	if _, err := docker(ctx, nil, "image", "inspect", ref); err != nil {
		fmt.Fprintf(log, "building %s\n", ref)
		if _, err := docker(ctx, nil, "build", "-q", "-t", ref, filepath.Join(root, i.Dir)); err != nil {
			return err
		}
	}
	nodes, err := kind.NewProvider(kind.ProviderWithDocker()).ListInternalNodes(cluster)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		// the tag is a content hash: present means current (image IDs differ between docker's
		// containerd store and the node, so they can't be compared)
		if _, err := nodeutils.ImageID(n, ref); err == nil {
			continue
		}
		fmt.Fprintf(log, "loading %s into %s\n", ref, n.String())
		var archive bytes.Buffer
		if _, err := docker(ctx, &archive, "save", ref); err != nil {
			return err
		}
		if err := nodeutils.LoadImageArchive(n, &archive); err != nil {
			return fmt.Errorf("load %s into %s: %w", ref, n.String(), err)
		}
	}
	return nil
}

// docker runs the docker CLI; stdout goes to out, or is returned trimmed when out is nil.
func docker(ctx context.Context, out io.Writer, args ...string) (string, error) {
	var buf, stderr bytes.Buffer
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdout, c.Stderr = &buf, &stderr
	if out != nil {
		c.Stdout = out
	}
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(buf.String()), nil
}
