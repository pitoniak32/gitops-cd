# Image discovery and manual Freight assembly

Status: accepted. Changes ADR 0012's Warehouse; gates and target stages are unchanged.

Before, `app.yml` pinned `image` (tag or digest), the renderer baked it into `rendered/apps/...`, and Freight was one `main` commit. A new image meant a git change per app, and config and image always moved together.

Now Freight carries two artifacts, chosen independently:

- **Config**: a `main` commit of `rendered/apps/<cap>/<app>/` (the config hash).
- **Image**: a SemVer tag of the app's image repo, pinned by digest.

Wiring:

- **`app.yml`**: `image` is the repo only (lint rejects a tag or digest). Optional `imageConstraint` is a SemVer range (`^1`). Per app, no override pin.
- **Warehouse** per app: a git subscription (as before) plus `image: {repoURL, imageSelectionStrategy: SemVer, constraint, discoveryLimit: 10}`, with `freightCreationPolicy: Manual`. It only discovers; it never creates Freight.
- **Assembly**: a promoter opens the Warehouse in the Kargo UI, picks a commit and an image tag, and creates the Freight. The first stage's gate takes it directly.
- **Rendering**: each target folder gets `kustomization.yaml` (`resources: [manifests.yaml]`); the Deployment's image is the bare repo.
- **Promotion** (target stage): after `copy`, `kustomize-set-image` writes `${{ imageFrom(vars.image).Digest }}` into the branch's `kustomization.yaml`. Argo CD builds the folder with kustomize. The commit message records the config commit and image tag.
- **Access**: `promote-<app>` gets `create` on `freights`, and `gitops-only.yaml` lets kargo-api create Freight as well as Promotions. Freight only references commits and images that the Warehouse already discovered, so config still changes only through git.

## Considered Options

- **Automatic Freight creation**: every new commit or tag would create Freight combining the latest of both. Rejected: the point is to choose which config goes with which image.
- **Keep a pinned `image` as an override**: rejected; one source of truth for the image (discovery).
- **`yaml-update` on `manifests.yaml`**: no kustomization needed, but the file is multi-document. `kustomize-set-image` is Kargo's documented path and keeps `manifests.yaml` untouched.
- **NewestBuild / Digest strategy**: rejected. SemVer gives readable versions and a constraint per app.

## Consequences

- Image tags must be SemVer (`strictSemvers`); `latest` and other non-SemVer tags are ignored.
- RBAC can't limit Freight `create` to one Warehouse (names are content hashes): any promoter in a capability can assemble Freight for any app in it. Promotion rights are still per app.
- Nothing reaches dev until someone assembles Freight, even with `autoPromote` (which still promotes assembled Freight).
- Freight created before this change has no image: promoting it fails at `kustomize-set-image` (`imageFrom`). Assemble new Freight.
- The rendered `manifests.yaml` alone deploys `<repo>:latest`; it is only deployed through a target branch, after `kustomize-set-image`.
