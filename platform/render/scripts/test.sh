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
# Argo CD visibility: promoters + viewers (emails) get read-only access to their capability's apps, nothing else
want='{"name":"viewer","policies":["p, proj:payments:viewer, applications, get, payments/*, allow"],"groups":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}'
got="$(yq -o=json -I=0 'select(.kind == "AppProject") | .spec.roles[] | {"name": .name, "policies": .policies, "groups": .groups}' "$tmp/argocd/payments.yaml")"
[[ "$got" == "$want" ]] || { printf 'AppProject viewer role\nwant: %s\ngot:  %s\n' "$want" "$got" >&2; exit 1; }
# Kargo visibility (email claim): promoters + viewers read their project via Kargo's per-project
# kargo-viewer Role, and list projects via a global SA (kargo-shared-resources) bound to kargo-user
k="$tmp/kargo/payments.yaml"
[[ "$(yq 'select(.kind == "RoleBinding" and .metadata.name == "viewers") | .roleRef.kind + "/" + .roleRef.name + "/" + .subjects[0].name' "$k")" == "Role/kargo-viewer/viewers" ]] || { echo "missing viewers RoleBinding" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "viewers") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}' ]] || { echo "viewers SA must map all promoters + viewers by email" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "users-payments") | .metadata.namespace + " " + .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == 'kargo-shared-resources {"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}' ]] || { echo "missing users-payments global SA" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ClusterRoleBinding" and .metadata.name == "kargo-users-payments") | .roleRef.name + "/" + .subjects[0].namespace + "/" + .subjects[0].name' "$k")" == "kargo-user/kargo-shared-resources/users-payments" ]] || { echo "missing kargo-users-payments ClusterRoleBinding" >&2; exit 1; }
# Kargo's promote API needs create on promotions as well as promote on the (named) stages
yq -e 'select(.kind == "Role" and .metadata.name == "promote-worker") | .rules[] | select(.resources[0] == "promotions" and (.verbs[] == "create"))' "$tmp/kargo/payments.yaml" >/dev/null || { echo "promote role can't create promotions" >&2; exit 1; }
# promoters are per app: worker's app owners.yml replaces promoters; my-app inherits the capability's
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-worker") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["alice@example.com","carol@example.com","doug@example.com"]}' ]] || { echo "promote-worker must use the app's promoters + inherited break-glass" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-my-app") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["carol@example.com","doug@example.com"]}' ]] || { echo "promote-my-app must inherit the capability's promoters + break-glass" >&2; exit 1; }
# break-glass (ADR 0013): every gate and target Stage carries the app's list for the approve guard
[[ "$(yq 'select(.kind == "Stage" and .metadata.labels."example.com/app" == "my-app") | .metadata.annotations."example.com/break-glass"' "$k" | sort -u | grep -v '^---$')" == '["carol@example.com"]' ]] || { echo "every my-app Stage needs the break-glass annotation" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Role" and .metadata.name == "promote-worker") | .rules[0].resourceNames | join(",")' "$k")" == "worker-dev,worker-gcp-us-prod,worker-prod,worker-staging,worker-us-east1-stg,worker-us-east4-dev" ]] || { echo "promote-worker must cover only worker's gate + target stages" >&2; exit 1; }
# Kargo graph: gate <app>-<stage> per gitops stage, target Stage <app>-<target> per target, own env branch
[[ "$(yq 'select(.kind == "Stage" and .metadata.name == "my-app-prod") | .spec.requestedFreight[0].sources | .availabilityStrategy + ":" + (.stages | join(","))' "$k")" == "All:my-app-on-prem-perf,my-app-on-prem-stg,my-app-us-east1-perf,my-app-us-east1-stg" ]] || { echo "prod gate must wait for every upstream target" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Stage" and .metadata.name == "my-app-prod") | .spec | has("promotionTemplate")' "$k")" == "false" ]] || { echo "gates must be control flow stages" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Stage" and .metadata.name == "my-app-gcp-eu-prod") | (.spec.requestedFreight[0].sources.stages | join(",")) + " " + .spec.promotionTemplate.spec.vars[2].value' "$k")" == "my-app-prod env/payments/my-app/gcp-eu-prod" ]] || { echo "target stage must follow its gate and push its own branch" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Application" and .metadata.name == "payments-my-app-gcp-eu-prod") | .spec.source.targetRevision + " " + .spec.source.path + " " + .metadata.annotations."kargo.akuity.io/authorized-stage"' "$tmp/argocd/payments.yaml")" == "env/payments/my-app/gcp-eu-prod . kargo-payments:my-app-gcp-eu-prod" ]] || { echo "Argo CD app must track its target branch, authorized for its target stage" >&2; exit 1; }
if grep -q '"groups"' "$tmp"/kargo/*.yaml; then echo "no Kargo claim may use groups" >&2; exit 1; fi
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

# Every app overrides promoters: the capability-level promoter gets no promote role but still
# views; stages/targets approvers (CODEOWNERS) are the apps' owners.
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"
printf 'owners: [team1]\npromoters: [carol@example.com]\n' >"$r/capabilities/search/owners.yml"
printf 'owners: [team2]\npromoters: [dave@example.com]\n' >"$r/capabilities/search/apps/api/owners.yml"
scripts/render.sh "$r" search "$o" >/dev/null
[[ -f "$o/kargo/search.yaml" ]] || { echo "missing $o/kargo/search.yaml" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-api") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$o/kargo/search.yaml")" == '{"email":["dave@example.com"]}' ]] || { echo "capability-only promoter got promote rights" >&2; exit 1; }
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
  grep -qE '^capabilities/payments/[a-z]+/Bad_Name(/app|/target)?\.yml:[0-9]+:[0-9]+: .*invalid name' <<<"$got" || { printf 'bad name not rejected for %s:\n%s\n' "$d" "$got" >&2; exit 1; }
  echo "bad name ok: $d"
done

# A target named like a stage would collide as a Kargo Stage name (<app>-<name>)
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
cp -R "$r/capabilities/payments/targets/on-prem-dev" "$r/capabilities/payments/targets/dev"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/dev/target\.yml:[0-9]+:[0-9]+: .*also a stage name' <<<"$got" || { printf 'target/stage name collision not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target/stage name collision ok"

# Target layout: old targets/<t>.yml and bundles without target.yml fail lint
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; t="$r/capabilities/payments/targets"
cp "$t/on-prem-dev/target.yml" "$t/old.yml"
mkdir -p "$t/ghost/environment-variables"; echo 'A: b' >"$t/ghost/environment-variables/x.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/old\.yml:1:1: .*move to targets/old/target\.yml' <<<"$got" || { printf 'legacy target file not rejected:\n%s\n' "$got" >&2; exit 1; }
grep -qE '^capabilities/payments/targets/ghost/environment-variables/x\.yml:[0-9]+:[0-9]+: .*without targets/ghost/target\.yml' <<<"$got" || { printf 'bundle without target not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target layout ok"

# envFrom precedence: target env > stage env > app env > environment-variables bundle
for t in us-east4-dev:true on-prem-dev:false; do
  v="$(yq "select(.kind == \"Deployment\") | .spec.template.spec.containers[0].env[] | select(.name == \"APP_ENV\") | .value" testdata/golden/apps/payments/my-app/dev/${t%%:*}/*.yaml)"
  [[ $v == "${t#*:}" ]] || { echo "APP_ENV on ${t%%:*}: got '$v' want '${t#*:}' (bundle must not beat app/target env)" >&2; exit 1; }
done
echo "envFrom precedence ok"

# envFrom KEY clash: most specific layer wins; same layer: equal ok, different = error
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments"
printf 'LD_RELAY_URL: https://other\nLD_SDK_KEY: secret:acme-dev-use4/launchdarkly-sdk-key\n' >"$c/targets/us-east4-dev/environment-variables/flags.yml"
yq -i '.stages.dev.targets.us-east4-dev.envFrom = ["flags"]' "$c/apps/my-app/app.yml"
scripts/render.sh "$r" payments "$o" >/dev/null
v="$(yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].env[] | select(.name == "LD_RELAY_URL") | .value' "$o/apps/payments/my-app/dev/us-east4-dev/manifests.yaml")"
[[ $v == https://other ]] || { echo "target-level envFrom must win: LD_RELAY_URL=$v" >&2; exit 1; }
yq -i '.stages.dev.targets.us-east4-dev.envFrom = ["flags", "launchdarkly"]' "$c/apps/my-app/app.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}" "${o:?}"
grep -qE '^capabilities/payments/apps/my-app/app\.yml:[0-9]+:[0-9]+: targets\.us-east4-dev\.envFrom: .*LD_RELAY_URL differs between environment-variables/\{flags,launchdarkly\}\.yml \(both in target-level' <<<"$got" || { printf 'same-layer clash not rejected:\n%s\n' "$got" >&2; exit 1; }
grep -q LD_SDK_KEY <<<"$got" && { printf 'equal values must not clash:\n%s\n' "$got" >&2; exit 1; }
echo "envFrom clash ok"
