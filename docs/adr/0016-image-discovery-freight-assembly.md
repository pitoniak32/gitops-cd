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
- **Rendering**: each target folder holds `deployment.yaml` (the Deployment alone, image = bare repo) and `manifests.yaml` (everything else).
- **Promotion** (target stage): after `copy`, `yaml-update` sets `spec.template.spec.containers.0.image` in the branch's `deployment.yaml` to `<repo>:<tag>@<digest>` from the Freight (the digest pins; the tag is for readers). The branch stays plain YAML, exactly what Argo CD applies (ADR 0002). The commit message records the config commit and image tag.
- **Access**: `promote-<app>` gets `create` on `freights`, and `gitops-only.yaml` lets kargo-api create Freight as well as Promotions. Freight only references commits and images that the Warehouse already discovered, so config still changes only through git.

## Considered Options

- **Automatic Freight creation**: every new commit or tag would create Freight combining the latest of both. Rejected: the point is to choose which config goes with which image.
- **Keep a pinned `image` as an override**: rejected; one source of truth for the image (discovery).
- **`kustomize-set-image`** with a rendered `kustomization.yaml`: shipped first, then replaced. Argo CD would build every target with kustomize, and the branch no longer shows the applied YAML.
- **`yaml-update` on the multi-document `manifests.yaml`**: doesn't work. Kargo 1.12's `yaml-update` only addresses a file's first document (`pkg/yaml`), which is the ServiceAccount. Putting the Deployment first would rely on that quirk; a separate file doesn't.
- **`argocd-update` image override** (Application spec): nothing in git shows what's deployed.
- **NewestBuild / Digest strategy**: rejected. SemVer gives readable versions and a constraint per app.

## Consequences

- Image tags must be SemVer (`strictSemvers`); `latest` and other non-SemVer tags are ignored.
- RBAC can't limit Freight `create` to one Warehouse (names are content hashes): any promoter in a capability can assemble Freight for any app in it. Promotion rights are still per app.
- Nothing reaches dev until someone assembles Freight, even with `autoPromote` (which still promotes assembled Freight).
- Freight created before this change has no image, and commits rendered before it have no `deployment.yaml`: promoting either fails. Assemble new Freight from a newer commit.
- The rendered `deployment.yaml` alone would deploy `<repo>:latest`; it is only deployed through a target branch, after `yaml-update`.
