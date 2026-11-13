# Image discovery and manual Freight assembly

Freight carries two artifacts, chosen independently: a **config** commit of `rendered/apps/<sys>/<app>/` (on branch `rendered`, ADR 0002) and an **image** SemVer tag, pinned by digest. A new image needs no git change, and a promoter chooses which config goes with which image.

- **`app.yml`**: `image` is the repo only (lint rejects a tag or digest). Optional `imageConstraint` is a SemVer range (`^1`).
- **Warehouse** `<app>`: a git subscription plus `image: {repoURL, imageSelectionStrategy: SemVer, constraint, discoveryLimit: 10}`, with `freightCreationPolicy: Manual`. It only discovers.
- **Assembly**: a promoter picks a commit and a tag in the Kargo UI and creates the Freight. `promote-<stage>` has `create` on `freights`, and `gitops-only.yaml` lets kargo-api create Freight and Promotions. Freight only references what the Warehouse discovered, so config still changes only through git. Each app has its own project (ADR 0029), so a promoter can assemble Freight only for apps they promote.
- **Rendering**: each K8S target folder holds `deployment.yaml` (the Deployment alone, with image = bare repo) and `manifests.yaml` (everything else).
- **Promotion**: after the copy, `yaml-update` sets the container image in the branch's `deployment.yaml` to `<repo>:<tag>@<digest>` (the digest pins, the tag is for readers). The branch stays plain YAML, exactly what Argo CD applies. Terraform modules (ADR 0021) and Cloud Run services (ADR 0024) are pinned the same way.

## Considered Options

- **Automatic Freight creation**: it combines the latest of both, which defeats choosing.
- **A pinned `image` override**: two sources of truth.
- **`kustomize-set-image`**: Argo CD would build every target with kustomize, and the branch wouldn't show the applied YAML.
- **`yaml-update` on the multi-document `manifests.yaml`**: Kargo's `yaml-update` only addresses a file's first document.
- **An `argocd-update` image override**: nothing in git would show what's deployed.
- **NewestBuild or Digest strategy**: SemVer gives readable versions and a constraint per app.

## Consequences

- Image tags must be SemVer (`strictSemvers`). `latest` and other non-SemVer tags are ignored.
- Nothing reaches dev until someone assembles Freight, even with `autoPromote`.
- The rendered `deployment.yaml` on its own would deploy `<repo>:latest`. It is only ever deployed through a target branch, after `yaml-update`.
