#!/usr/bin/env bash
# Golden test: render testdata/root (capabilities payments, search) and diff against testdata/golden. UPDATE=1 to regenerate.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
for cap in payments search; do scripts/render.sh testdata/root "$cap" "$tmp" >/dev/null; done
if [[ "${UPDATE:-}" == 1 ]]; then rm -rf testdata/golden; cp -R "$tmp" testdata/golden; echo updated; exit; fi
diff -ru testdata/golden "$tmp"; echo "golden ok"
# kind: one AppProject per capability; no secret store -> no ExternalSecrets / secret env refs
yq -e 'select(.kind == "AppProject" and .metadata.name == "payments")' "$tmp/argocd/payments.yaml" >/dev/null || { echo "missing AppProject payments" >&2; exit 1; }
if grep -rqE 'ExternalSecret|secretKeyRef' "$tmp/apps/"; then echo "secret refs rendered for a cluster without a secret store" >&2; exit 1; fi
grep -rq "APP_SECRET" testdata/root/capabilities/payments/apps/my-app/app.yml || { echo "fixture lost its secret: env value" >&2; exit 1; }
# Argo CD dry-runs all resources first: the project namespace must be rendered (labeled for Kargo
# to adopt), not left for Kargo to create from the Project
[[ "$(yq 'select(documentIndex == 0) | .kind + "/" + .metadata.name + "/" + .metadata.labels."kargo.akuity.io/project"' "$tmp/kargo/payments.yaml")" == "Namespace/kargo-payments/true" ]] || { echo "kargo/payments.yaml must start with labeled Namespace kargo-payments" >&2; exit 1; }
# Argo CD visibility: owners' groups get read-only access to their capability's apps, nothing else
want='{"name":"viewer","policies":["p, proj:payments:viewer, applications, get, payments/*, allow"],"groups":["team1","team2"]}'
got="$(yq -o=json -I=0 'select(.kind == "AppProject") | .spec.roles[] | {"name": .name, "policies": .policies, "groups": .groups}' "$tmp/argocd/payments.yaml")"
[[ "$got" == "$want" ]] || { printf 'AppProject viewer role\nwant: %s\ngot:  %s\n' "$want" "$got" >&2; exit 1; }
# Kargo visibility: owners read their project via Kargo's per-project kargo-viewer Role, and list
# projects via a global SA (kargo-shared-resources) bound to kargo-user; no global viewers
k="$tmp/kargo/payments.yaml"
[[ "$(yq 'select(.kind == "RoleBinding" and .metadata.name == "owner-team2-viewer") | .roleRef.kind + "/" + .roleRef.name + "/" + .subjects[0].name' "$k")" == "Role/kargo-viewer/owner-team2" ]] || { echo "missing owner-team2-viewer RoleBinding" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "users-payments") | .metadata.namespace + " " + .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == 'kargo-shared-resources {"groups":["team1","team2"]}' ]] || { echo "missing users-payments global SA" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ClusterRoleBinding" and .metadata.name == "kargo-users-payments") | .roleRef.name + "/" + .subjects[0].namespace + "/" + .subjects[0].name' "$k")" == "kargo-user/kargo-shared-resources/users-payments" ]] || { echo "missing kargo-users-payments ClusterRoleBinding" >&2; exit 1; }
# Kargo's promote API needs create on promotions as well as promote on the (named) stages
yq -e 'select(.kind == "Role" and .metadata.name == "owner-team2") | .rules[] | select(.resources[0] == "promotions" and (.verbs[] == "create"))' "$tmp/kargo/payments.yaml" >/dev/null || { echo "owner role can't create promotions" >&2; exit 1; }
echo "kind cluster ok"

# CODEOWNERS: assemble from the fragments into a copy of root
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
scripts/codeowners.sh "$r" "$tmp"
diff -u testdata/golden-codeowners "$r/.github/CODEOWNERS"; echo "codeowners ok"
# a deleted capability drops out on the next full render
rm -rf "${r:?}/capabilities/search"; out2="$(mktemp -d)"
scripts/render.sh "$r" payments "$out2" >/dev/null
scripts/codeowners.sh "$r" "$out2"
if grep -q search "$r/.github/CODEOWNERS"; then echo "deleted capability still in CODEOWNERS" >&2; exit 1; fi
rm -rf "${r:?}" "${out2:?}"; echo "codeowners prune ok"
scripts/lint.sh testdata/root payments; echo "lint ok"

# Lint cases: <file under testdata/root> TAB <yq -i expr> TAB <ERE the lint output must match>
while IFS=$'\t' read -r file expr want; do
  c="$(mktemp -d)"; cp -R testdata/root/. "$c/"
  yq -i "$expr" "$c/$file"
  got="$(scripts/lint.sh "$c" payments || true)"; rm -rf "$c"
  grep -qE "$want" <<<"$got" || { printf 'lint case FAILED: %s %s\nwant: %s\ngot:\n%s\n' "$file" "$expr" "$want" "$got" >&2; exit 1; }
  echo "lint case ok: $expr"
done < testdata/lint-cases.tsv

# Every app overrides owners: the capability-only team gets no RBAC (empty resourceNames = all
# stages) and doesn't approve stages/targets.
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"
printf 'owners: [team1]\n' >"$r/capabilities/search/owners.yml"
printf 'owners: [team2]\n' >"$r/capabilities/search/apps/api/owners.yml"
scripts/render.sh "$r" search "$o" >/dev/null
[[ -f "$o/kargo/search.yaml" ]] || { echo "missing $o/kargo/search.yaml" >&2; exit 1; }
if grep -q 'owner-team1' "$o/kargo/search.yaml"; then echo "capability-only owner got Kargo RBAC" >&2; exit 1; fi
grep -qx '/capabilities/search/ @acme/team2' "$o/codeowners/search" || { echo "stages/targets approvers must be the apps' owners:" >&2; cat "$o/codeowners/search" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "override-all owners ok"
# no Role may ever render an empty resourceNames
ls testdata/golden/kargo/*.yaml >/dev/null || exit 1
if yq -e 'select(.kind == "Role") | .rules[] | select(has("resourceNames") and (.resourceNames | length) == 0)' testdata/golden/kargo/*.yaml >/dev/null 2>&1; then echo "empty resourceNames in golden" >&2; exit 1; fi

# Invalid app/stage/target directory names fail lint (they become k8s/Kargo names)
for d in apps/worker stages/perf targets/on-prem-dev; do
  r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments"
  mv "$c/$d" "$c/$(dirname "$d")/Bad_Name" 2>/dev/null || mv "$c/$d.yml" "$c/$(dirname "$d")/Bad_Name.yml"
  got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
  grep -qE '^capabilities/payments/[a-z]+/Bad_Name(/app)?\.yml:[0-9]+:[0-9]+: .*invalid name' <<<"$got" || { printf 'bad name not rejected for %s:\n%s\n' "$d" "$got" >&2; exit 1; }
  echo "bad name ok: $d"
done
