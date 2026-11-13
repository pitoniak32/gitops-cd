package tfmodule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

// the example module, as `gcd tf-module-push` pushes it
func exampleDir(t *testing.T) string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "../../../../examples/terraform/bucket")
}

func TestPushMissingTag(t *testing.T) {
	ctx := context.Background()
	dst := memory.New()
	var log bytes.Buffer
	if err := Push(ctx, dst, "ghcr.io/x/tf-bucket", "1.0.0", exampleDir(t), &log); err != nil {
		t.Fatal(err)
	}
	desc, err := dst.Resolve(ctx, "1.0.0")
	if err != nil {
		t.Fatalf("tag not pushed: %v", err)
	}
	var m ocispec.Manifest
	b, err := content.FetchAll(ctx, dst, desc)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// single manifest (Kargo's discovery rejects an index), empty JSON config, our artifact type
	if desc.MediaType != ocispec.MediaTypeImageManifest || m.ArtifactType != ArtifactType || m.Config.MediaType != ocispec.MediaTypeEmptyJSON {
		t.Fatalf("manifest: media %s artifactType %s config %s", desc.MediaType, m.ArtifactType, m.Config.MediaType)
	}
	// the source annotation links the ghcr package to the repo
	if m.Annotations[ocispec.AnnotationSource] != SourceRepo {
		t.Fatalf("annotations %v", m.Annotations)
	}
	if len(m.Layers) != 1 || m.Layers[0].MediaType != LayerType || m.Layers[0].Annotations[ocispec.AnnotationTitle] != "module.tgz" {
		t.Fatalf("layers %+v", m.Layers)
	}
	files := untar(t, dst, m.Layers[0])
	if !files["./main.tf"] || !files["./variables.tf"] {
		t.Fatalf("layer files %v", files)
	}
}

// An existing tag is never re-pushed: the tarball isn't reproducible, a re-push would move the tag.
func TestPushExistingTagSkipped(t *testing.T) {
	ctx := context.Background()
	dst := memory.New()
	if err := Push(ctx, dst, "r", "1.0.0", exampleDir(t), io.Discard); err != nil {
		t.Fatal(err)
	}
	before, _ := dst.Resolve(ctx, "1.0.0")
	var log bytes.Buffer
	if err := Push(ctx, dst, "r", "1.0.0", exampleDir(t), &log); err != nil {
		t.Fatal(err)
	}
	after, _ := dst.Resolve(ctx, "1.0.0")
	if before.Digest != after.Digest || !bytes.Contains(log.Bytes(), []byte("exists, skipped")) {
		t.Fatalf("re-pushed: %s -> %s, log %q", before.Digest, after.Digest, log.String())
	}
}

type failing struct{ *memory.Store }

func (failing) Resolve(context.Context, string) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errors.New("response status code 401: unauthorized")
}

// A registry or auth error fails without pushing.
func TestPushRegistryError(t *testing.T) {
	ctx := context.Background()
	dst := failing{memory.New()}
	if err := Push(ctx, dst, "r", "1.0.0", exampleDir(t), io.Discard); err == nil {
		t.Fatal("want error")
	}
	if _, err := dst.Store.Resolve(ctx, "1.0.0"); err == nil {
		t.Fatal("must not push")
	}
}

func untar(t *testing.T, s content.Fetcher, d ocispec.Descriptor) map[string]bool {
	t.Helper()
	b, err := content.FetchAll(context.Background(), s, d)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	out := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = true
	}
}
