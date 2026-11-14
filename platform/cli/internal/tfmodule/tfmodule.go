// Package tfmodule pushes the example terraform module (examples/terraform/bucket) to ghcr as an OCI
// artifact (ADR 0021): the Warehouse of payments/infra discovers its tags.
//
// Shape Kargo needs: a single manifest (its image discovery rejects an index) with the empty JSON config
// (a non-JSON config fails discovery), like `oras push`'s default. The ghcr package must be public: ghcr
// makes new ones private and Kargo has no registry credential (docs/gotchas.md).
package tfmodule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	Repo         = "ghcr.io/pitoniak32/tf-bucket"
	SourceRepo   = "https://github.com/pitoniak32/gitops-cd"
	ArtifactType = "application/vnd.example.tf.module.v1"
	LayerType    = "application/vnd.example.tf.module.v1.tar+gzip"
)

// Target is where the module goes: a registry repository (or an in-memory store in tests).
type Target interface {
	oras.Target
}

// Push pushes dir as repo:tag unless the tag exists. Idempotent: an existing tag is never re-pushed
// (the tarball isn't reproducible; a re-push would move the tag).
func Push(ctx context.Context, dst Target, repo, tag, dir string, log io.Writer) error {
	ref := repo + ":" + tag
	_, err := dst.Resolve(ctx, tag)
	switch {
	case err == nil:
		fmt.Fprintf(log, "%s exists, skipped\n", ref)
		return nil
	case !errors.Is(err, errdef.ErrNotFound):
		return fmt.Errorf("checking %s: %w", ref, err)
	}
	tgz, err := tarGz(dir)
	if err != nil {
		return err
	}
	layer := content.NewDescriptorFromBytes(LayerType, tgz)
	layer.Annotations = map[string]string{ocispec.AnnotationTitle: "module.tgz"} // `oras pull` file name
	if err := dst.Push(ctx, layer, bytes.NewReader(tgz)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return fmt.Errorf("push layer: %w", err)
	}
	// the source annotation links the package to the repo (ghcr: the workflow GITHUB_TOKEN can then pull it)
	m, err := oras.PackManifest(ctx, dst, oras.PackManifestVersion1_1, ArtifactType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{ocispec.AnnotationSource: SourceRepo},
	})
	if err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}
	if err := dst.Tag(ctx, m, tag); err != nil {
		return fmt.Errorf("tag %s: %w", ref, err)
	}
	fmt.Fprintf(log, "pushed %s\n", ref)
	return nil
}

// Remote is repo on ghcr, authenticated with the gh CLI token. ghcr needs a classic token, not the
// GitHub App; the gh token needs write:packages
// (`gh auth refresh -h github.com -s write:packages`).
func Remote(ctx context.Context, repo string) (Target, error) {
	r, err := remote.NewRepository(repo)
	if err != nil {
		return nil, err
	}
	token, err := gh(ctx, "auth", "token")
	if err != nil {
		return nil, err
	}
	login, err := gh(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return nil, err
	}
	r.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(r.Reference.Registry, auth.Credential{Username: login, Password: token}),
	}
	return r, nil
}

func gh(ctx context.Context, args ...string) (string, error) {
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "gh", args...)
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// tarGz is dir as a gzipped tar with ./-relative names (what `tar -C dir -czf - .` writes).
func tarGz(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = "./" + filepath.ToSlash(rel)
		if d.IsDir() {
			h.Name = strings.TrimSuffix(h.Name, "/.") + "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			_, err = tw.Write(b)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
