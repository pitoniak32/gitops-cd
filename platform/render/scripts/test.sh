#!/usr/bin/env bash
# Golden test: render testdata/root (capabilities payments, search) and diff against testdata/golden. UPDATE=1 to regenerate.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
for cap in payments search; do scripts/render.sh testdata/root "$cap" "$tmp" >/dev/null; done
scripts/regions.sh "$tmp" >/dev/null  # platform-level outputs from clusters.cue
if [[ "${UPDATE:-}" == 1 ]]; then rm -rf testdata/golden; cp -R "$tmp" testdata/golden; echo updated; exit; fi
diff -ru testdata/golden "$tmp"; echo "golden ok"
# regions registry (clusters.cue): one JSON per region for hub-side AppSets; projects dir exists per region
[[ "$(cat "$tmp/infrastructure/regions/us.json")" == '{"region":"us"}' ]] || { echo "infrastructure/regions/us.json wrong" >&2; exit 1; }
[[ "$(ls "$tmp/infrastructure/regions" | paste -sd' ' -)" == "onprem.json us.json" ]] || { echo "one regions JSON per clusters.cue region" >&2; exit 1; }
[[ -f "$tmp/argocd/projects/onprem/.keep" ]] || { echo "argocd/projects/<r>/.keep missing" >&2; exit 1; }
echo "regions ok"
# kind: one AppProject per capability; no secret store -> no ExternalSecrets / secret env refs
yq -e 'select(.kind == "AppProject" and .metadata.name == "payments")' "$tmp/argocd/projects/us/payments.yaml" >/dev/null || { echo "missing AppProject payments" >&2; exit 1; }
if grep -rqE 'ExternalSecret|secretKeyRef' "$tmp/apps/"; then echo "secret refs rendered for a cluster without a secret store" >&2; exit 1; fi
grep -rq "APP_SECRET" testdata/root/capabilities/payments/apps/my-app/app.yml || { echo "fixture lost its secret: env value" >&2; exit 1; }
# Argo CD dry-runs all resources first: the project namespace must be rendered (labeled for Kargo
# to adopt), not left for Kargo to create from the Project
[[ "$(yq 'select(documentIndex == 0) | .kind + "/" + .metadata.name + "/" + .metadata.labels."kargo.akuity.io/project"' "$tmp/kargo/payments.yaml")" == "Namespace/kargo-payments/true" ]] || { echo "kargo/payments.yaml must start with labeled Namespace kargo-payments" >&2; exit 1; }
# Argo CD visibility: promoters + viewers (emails) get read-only access to their capability's apps, nothing else
want='{"name":"viewer","policies":["p, proj:payments:viewer, applications, get, payments/*, allow"],"groups":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}'
got="$(yq -o=json -I=0 'select(.kind == "AppProject") | .spec.roles[] | {"name": .name, "policies": .policies, "groups": .groups}' "$tmp/argocd/projects/us/payments.yaml")"
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
# break-glass (ADR 0013): every control-flow and target Stage carries the app's list for the approve guard
[[ "$(yq 'select(.kind == "Stage" and .metadata.labels."example.com/app" == "my-app") | .metadata.annotations."example.com/break-glass"' "$k" | sort -u | grep -v '^---$')" == '["carol@example.com"]' ]] || { echo "every my-app Stage needs the break-glass annotation" >&2; exit 1; }
# Kargo graph (ADR 0019): control-flow Stages only for fan-in/out and one-click stage release; targets <app>-<target>
src() { yq "select(.kind==\"Stage\" and .metadata.name==\"$1\") | .spec.requestedFreight[0].sources | ((.direct // false) | tostring) + \":\" + (.availabilityStrategy // \"\") + \":\" + ((.stages // []) | join(\",\"))" "$2"; }
want() { [[ "$(src "$1" "$3")" == "$2" ]] || { echo "Stage $1 sources: want '$2' got '$(src "$1" "$3")'" >&2; exit 1; }; }
stages_of() { yq "select(.kind==\"Stage\" and .metadata.labels.\"example.com/app\"==\"$1\") | .metadata.name" "$2" | grep -v '^---$' | sort | paste -sd' ' -; }
oneclick() { # <kargo yaml>
  local f="$1" app st tg ok n ch
  for app in $(yq 'select(.kind=="Stage") | .metadata.labels."example.com/app"' "$f" | grep -v '^---$' | sort -u); do
    for st in $(yq "select(.kind==\"Stage\" and .metadata.labels.\"example.com/app\"==\"$app\" and .metadata.labels.\"example.com/kind\"==\"target\") | .metadata.labels.\"example.com/stage\"" "$f" | grep -v '^---$' | sort | uniq -d); do
      tg="$(yq "select(.kind==\"Stage\" and .metadata.labels.\"example.com/app\"==\"$app\" and .metadata.labels.\"example.com/kind\"==\"target\" and .metadata.labels.\"example.com/stage\"==\"$st\") | .metadata.name" "$f" | grep -v '^---$' | sort | paste -sd, -)"
      ok=""; for n in $(stages_of "$app" "$f"); do
        ch="$(yq "select(.kind==\"Stage\" and ((.spec.requestedFreight[0].sources.stages // []) | any_c(. == \"$n\"))) | .metadata.name" "$f" | { grep -v '^---$' || true; } | sort | paste -sd, -)"
        [[ "$ch" == "$tg" ]] && ok=1
      done
      [[ -n "$ok" ]] || { echo "e) $app stage $st ($tg) has no one-click node in $f" >&2; exit 1; }
    done
  done
}
[[ "$(stages_of my-app "$k")" == "my-app-dev my-app-dev-done my-app-onprem-dev my-app-onprem-prod my-app-onprem-stg my-app-prod my-app-staging my-app-us-dev my-app-us-perf my-app-us-prod my-app-us-stg" ]] || { echo "my-app Stage set: $(stages_of my-app "$k")" >&2; exit 1; }
want my-app-dev "true::" "$k"; want my-app-us-dev "false::my-app-dev" "$k"; want my-app-onprem-dev "false::my-app-dev" "$k"
want my-app-dev-done "false:All:my-app-onprem-dev,my-app-us-dev" "$k"
want my-app-staging "false::my-app-dev-done" "$k"; want my-app-us-stg "false::my-app-staging" "$k"; want my-app-onprem-stg "false::my-app-staging" "$k"
want my-app-us-perf "false::my-app-dev-done" "$k"
want my-app-prod "false:All:my-app-onprem-stg,my-app-us-perf,my-app-us-stg" "$k"
want my-app-us-prod "false::my-app-prod" "$k"; want my-app-onprem-prod "false::my-app-prod" "$k"
[[ "$(stages_of worker "$k")" == "worker-us-dev worker-us-prod worker-us-stg" ]] || { echo "worker must have no control flow: $(stages_of worker "$k")" >&2; exit 1; }
want worker-us-dev "true::" "$k"; want worker-us-stg "false::worker-us-dev" "$k"; want worker-us-prod "false::worker-us-stg" "$k"
s="$tmp/kargo/search.yaml"
[[ "$(stages_of api "$s")" == "api-us-dev" ]] || { echo "search api must be one target Stage" >&2; exit 1; }
want api-us-dev "true::" "$s"
for n in my-app-dev my-app-dev-done my-app-prod my-app-staging; do
  [[ "$(yq "select(.kind==\"Stage\" and .metadata.name==\"$n\") | .metadata.labels.\"example.com/kind\" + \" \" + (.spec | has(\"promotionTemplate\") | tostring) + \" \" + (.spec.shard // \"-\")" "$k")" == "control-flow false -" ]] || { echo "$n must be control-flow: no steps, no shard" >&2; exit 1; }
done
[[ "$(yq 'select(.kind=="Stage" and .metadata.name=="my-app-dev-done") | .metadata.labels."example.com/stage"' "$k") $(yq 'select(.kind=="Stage" and .metadata.name=="my-app-staging") | .metadata.labels."example.com/stage"' "$k")" == "dev staging" ]] || { echo "control-flow stage labels" >&2; exit 1; }
[[ "$(yq 'select(.kind=="ProjectConfig") | .spec.promotionPolicies[] | .stageSelector.name' "$k" | sort | paste -sd' ' -)" == "infra-tf-dev my-app-dev my-app-dev-done my-app-onprem-dev my-app-prod my-app-staging my-app-us-dev payments-us-dev worker-us-dev" ]] || { echo "auto-promotion: every control-flow Stage + targets of autoPromote stages only" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Role" and .metadata.name == "promote-worker") | .rules[0].resourceNames | join(",")' "$k")" == "worker-us-dev,worker-us-prod,worker-us-stg" ]] || { echo "promote-worker must cover exactly worker's Stages" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Role" and .metadata.name == "promote-my-app") | .rules[0].resourceNames | join(",")' "$k")" == "my-app-dev,my-app-dev-done,my-app-onprem-dev,my-app-onprem-prod,my-app-onprem-stg,my-app-prod,my-app-staging,my-app-us-dev,my-app-us-perf,my-app-us-prod,my-app-us-stg" ]] || { echo "promote-my-app must cover targets + control flow" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Stage" and .metadata.name == "my-app-onprem-prod") | .spec.promotionTemplate.spec.vars[2].value' "$k")" == "env/payments/my-app/onprem-prod" ]] || { echo "target stage must push its own branch" >&2; exit 1; }
d="$tmp/argocd/deploys/onprem/payments/my-app-onprem-prod.json"
[[ "$(jq -r '.appName + " " + .branch + " " + .authorizedStage + " " + .destination + "/" + .namespace' "$d")" == "payments-my-app-onprem-prod env/payments/my-app/onprem-prod kargo-payments:my-app-onprem-prod in-cluster/payments-onprem-prod" ]] || { echo "deploy params must track the target branch, authorized for its target stage" >&2; exit 1; }
# workloads AppSet (platform/appsets/workloads.yaml) reads exactly these keys; git files gen reserves path/values
[[ "$(jq -r 'keys | join(",")' "$d")" == "app,appName,authorizedStage,branch,capability,destination,namespace,stage,target" ]] || { echo "deploy params keys changed: $(jq -c keys "$d")" >&2; exit 1; }
# regions: target Stages run on their cluster's region shard; control-flow Stages and Warehouses stay on the hub controller
[[ "$(yq 'select(.kind == "Stage") | .metadata.name + "=" + (.spec.shard // "-")' "$k" | grep '^my-app-' | sort | paste -sd' ' -)" == "my-app-dev-done=- my-app-dev=- my-app-onprem-dev=onprem my-app-onprem-prod=onprem my-app-onprem-stg=onprem my-app-prod=- my-app-staging=- my-app-us-dev=us my-app-us-perf=us my-app-us-prod=us my-app-us-stg=us" ]] || { echo "shard must be set on target Stages only, from the target's cluster region" >&2; exit 1; }
if yq -e 'select(.kind == "Warehouse") | .spec.shard' "$k" >/dev/null 2>&1; then echo "Warehouses must not be sharded" >&2; exit 1; fi
# Argo CD output is per region; destination is the regional Argo CD's own cluster
[[ "$(cd "$tmp/argocd" && find . -type f ! -name .keep | sort | paste -sd' ' -)" == "./deploys/onprem/payments/my-app-onprem-dev.json ./deploys/onprem/payments/my-app-onprem-prod.json ./deploys/us/payments/my-app-us-dev.json ./deploys/us/payments/my-app-us-perf.json ./deploys/us/payments/my-app-us-prod.json ./deploys/us/payments/my-app-us-stg.json ./deploys/us/payments/payments-us-dev.json ./deploys/us/payments/payments-us-prod.json ./deploys/us/payments/payments-us-stg.json ./deploys/us/payments/worker-us-prod.json ./deploys/us/payments/worker-us-stg.json ./deploys/us/search/api-us-dev.json ./projects/onprem/payments.yaml ./projects/us/payments.yaml ./projects/us/search.yaml" ]] || { echo "argocd output must be per region: deploys/<r>/<cap>/ + projects/<r>/<cap>.yaml" >&2; exit 1; }
if grep -rq 'kind: Application$' "$tmp/argocd"; then echo "renderer must not emit Applications (workloads AppSet does)" >&2; exit 1; fi
[[ "$(yq 'select(.kind == "AppProject") | .spec.destinations[] | .name + "/" + .namespace' "$tmp/argocd/projects/onprem/payments.yaml" | paste -sd' ' -)" == "in-cluster/payments-onprem-dev in-cluster/payments-onprem-prod" ]] || { echo "onprem AppProject must list only onprem destinations" >&2; exit 1; }
if grep -l onprem "$tmp"/argocd/deploys/us/*/*.json >/dev/null 2>&1; then echo "us deploys contain onprem targets" >&2; exit 1; fi
if grep -q '"groups"' "$tmp"/kargo/*.yaml; then echo "no Kargo claim may use groups" >&2; exit 1; fi
echo "kind cluster ok"
# GHA targets (deployWith: gha): env branch written, then dispatch + wait instead of argocd-update
st() { yq "select(.kind==\"Stage\" and .metadata.name==\"$1\") | $2" "$k" | sed '/^$/d;/^---$/d'; }  # yq prints blank lines / separators for unselected documents
[[ "$(st worker-us-dev '[.spec.promotionTemplate.spec.steps[].uses] | join(",")')" == "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" ]] || { echo "worker-us-dev steps" >&2; exit 1; }
[[ "$(st worker-us-stg '.spec.promotionTemplate.spec.steps[-1].uses')" == "argocd-update" ]] || { echo "worker-us-stg must stay argocd" >&2; exit 1; }
[[ "$(st worker-us-dev '.metadata.labels."example.com/deploy-with"') $(st worker-us-stg '.metadata.labels."example.com/deploy-with"')" == "gha argocd" ]] || { echo "deploy-with label" >&2; exit 1; }
[[ "$(st worker-us-dev '[.spec.promotionTemplate.spec.vars[].name] | join(",")')" == "repo,src,branch,image,workflowRepo,workflowFile,workflowRef" ]] || { echo "vars order (kargo-restore reads by name)" >&2; exit 1; }
[[ "$(st worker-us-stg '[.spec.promotionTemplate.spec.vars[].name] | join(",")')" == "repo,src,branch,image" ]] || { echo "argocd Stage keeps its four vars" >&2; exit 1; }
[[ "$(st worker-us-dev '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "pitoniak32/gitops-cd deploy.yaml main" ]] || { echo "default workflow" >&2; exit 1; }
[[ "$(st my-app-onprem-stg '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "acme/deploys onprem.yml v1" ]] || { echo "target workflow override" >&2; exit 1; }
d1='.spec.promotionTemplate.spec.steps[] | select(.as == "dispatch")'
[[ "$(st worker-us-dev "$d1 | .config.method + \" \" + .config.url")" == 'POST https://api.github.com/repos/${{ vars.workflowRepo }}/actions/workflows/${{ vars.workflowFile }}/dispatches' ]] || { echo "dispatch url" >&2; exit 1; }
[[ "$(st worker-us-dev "$d1 | .config.headers[] | select(.name == \"Authorization\") | .value")" == "Bearer \${{ sharedSecret('github-actions').token }}" ]] || { echo "auth header" >&2; exit 1; }
[[ "$(st worker-us-dev "$d1 | .config.successExpression + \" | \" + .config.failureExpression + \" | \" + .config.errorExpression")" == "response.status == 200 | response.status != 200 | response.body.message" ]] || { echo "dispatch expressions" >&2; exit 1; }
[[ "$(st worker-us-dev "$d1 | [.config.outputs[] | .name + \"=\" + .fromExpression] | join(\",\")")" == "runID=response.body.workflow_run_id,runURL=response.body.html_url" ]] || { echo "dispatch outputs" >&2; exit 1; }
for f in "'ref': vars.workflowRef" "'return_run_details': true" "'commit': outputs.commit.commit" "'target': 'us-dev'" "imageFrom(vars.image).Digest"; do
  st worker-us-dev "$d1 | .config.body" | grep -qF "$f" || { echo "dispatch body missing $f" >&2; exit 1; }
done
w1='.spec.promotionTemplate.spec.steps[] | select(.as == "wait")'
[[ "$(st worker-us-dev "$w1 | .retry.timeout + \" \" + .config.url")" == '30m https://api.github.com/repos/${{ vars.workflowRepo }}/actions/runs/${{ outputs.dispatch.runID }}' ]] || { echo "wait step" >&2; exit 1; }
[[ "$(st worker-us-dev "$w1 | .config.successExpression")" == "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'" ]] || { echo "wait success" >&2; exit 1; }
[[ "$(st worker-us-dev "$w1 | .config.failureExpression")" == "response.status != 200 || (response.body.status == 'completed' && response.body.conclusion != 'success')" ]] || { echo "wait failure" >&2; exit 1; }
[[ "$(st worker-us-dev "$w1 | .config.errorExpression")" == "response.status != 200 ? response.body.message : 'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url" ]] || { echo "wait error message (non-200 has no run fields)" >&2; exit 1; }
# Kargo ends a ${{ }} expression at the first "}}": a nested map literal must not close with "}}"
if grep -nE '[^ ]\}\}' "$k" testdata/golden/kargo/*.yaml; then echo 'Kargo expression cut short: "}}" not preceded by a space' >&2; exit 1; fi
echo "gha targets ok"
# terraform apps (ADR 0021): one terraform.yaml per target, no k8s manifests, no Argo CD params
tf="$tmp/apps/payments/infra"
[[ "$(ls "$tf/dev/tf-dev" | paste -sd' ' -)" == "terraform.yaml" ]] || { echo "terraform target renders only terraform.yaml: $(ls "$tf/dev/tf-dev")" >&2; exit 1; }
want='{"capability":"payments","app":"infra","stage":"dev","target":"tf-dev","environment":"dev","gcpProject":"team1-dev","wifEmail":"team1-dev@team1-dev.iam.gserviceaccount.com","backend":{"bucket":"tfstate-team1-dev","prefix":"payments/infra/tf-dev"},"module":"ghcr.io/pitoniak32/tf-bucket","env":{"TF_VAR_bucket_name":"payments-dev","TF_VAR_region":"us-central1"}}'
got="$(yq -o=json -I=0 '.' "$tf/dev/tf-dev/terraform.yaml")"
[[ "$got" == "$want" ]] || { printf 'tf-dev terraform.yaml\nwant: %s\ngot:  %s\n' "$want" "$got" >&2; exit 1; }
[[ "$(yq '.backend.prefix + " " + .env.TF_VAR_region' "$tf/prod/tf-prod/terraform.yaml")" == "payments/custom us-east1" ]] || { echo "tf-prod prefix override / target env" >&2; exit 1; }
if ls "$tmp"/argocd/deploys/*/payments/infra-* >/dev/null 2>&1; then echo "terraform targets must not get Argo CD params" >&2; exit 1; fi
echo "terraform render ok"
# deployWith/workflow resolve per field, target > stage > app > default
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments/apps/my-app/app.yml"
yq -i '.workflow.ref = "release" | .stages.dev.deployWith = "gha" | .stages.dev.targets.onprem-dev = {"deployWith": "argocd", "workflow": {"file": "x.yml"}} | .stages.dev.targets.us-dev.workflow.repo = "acme/deploys"' "$c"
scripts/lint.sh "$r" payments >/dev/null || { echo "app-level workflow with argocd targets must lint clean" >&2; exit 1; }
scripts/render.sh "$r" payments "$o" >/dev/null
pt() { jq -r ".\"my-app\".stages.$1.targets.\"$2\".deployWith" "$o/pipelines/payments.json"; }
[[ "$(pt dev us-dev) $(pt dev onprem-dev) $(pt prod us-prod)" == "gha argocd argocd" ]] || { echo "deployWith resolution: $(pt dev us-dev) $(pt dev onprem-dev) $(pt prod us-prod)" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "deployWith resolution ok"

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
# env keys: any C identifier (some apps need lowercase, terraform TF_VAR_<name>)
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
yq -i '.env.TF_VAR_lower_ok = "x"' "$r/capabilities/payments/apps/my-app/app.yml"
scripts/lint.sh "$r" payments >/dev/null || { echo "lowercase env key must lint clean" >&2; exit 1; }
rm -rf "${r:?}"; echo "lowercase env ok"

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
for d in apps/worker stages/perf targets/onprem-dev; do
  r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments"
  mv "$c/$d" "$c/$(dirname "$d")/Bad_Name" 2>/dev/null || mv "$c/$d.yml" "$c/$(dirname "$d")/Bad_Name.yml"
  got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
  grep -qE '^capabilities/payments/[a-z]+/Bad_Name(/app|/target)?\.yml:[0-9]+:[0-9]+: .*invalid name' <<<"$got" || { printf 'bad name not rejected for %s:\n%s\n' "$d" "$got" >&2; exit 1; }
  echo "bad name ok: $d"
done

# A target named like a stage would collide as a Kargo Stage name (<app>-<name>)
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
cp -R "$r/capabilities/payments/targets/onprem-dev" "$r/capabilities/payments/targets/dev"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/dev/target\.yml:[0-9]+:[0-9]+: .*also a stage name' <<<"$got" || { printf 'target/stage name collision not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target/stage name collision ok"
# ...nor like <stage>-done (fan-out control-flow Stage <app>-<stage>-done, ADR 0019)
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
cp -R "$r/capabilities/payments/targets/onprem-dev" "$r/capabilities/payments/targets/dev-done"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/dev-done/target\.yml:[0-9]+:[0-9]+: .*target name dev-done is <stage>-done; Kargo control-flow stage names <app>-dev-done would collide' <<<"$got" || { printf 'target named <stage>-done not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target <stage>-done collision ok"

# Target layout: old targets/<t>.yml and bundles without target.yml fail lint
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; t="$r/capabilities/payments/targets"
cp "$t/onprem-dev/target.yml" "$t/old.yml"
mkdir -p "$t/ghost/environment-variables"; echo 'A: b' >"$t/ghost/environment-variables/x.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/old\.yml:1:1: .*move to targets/old/target\.yml' <<<"$got" || { printf 'legacy target file not rejected:\n%s\n' "$got" >&2; exit 1; }
grep -qE '^capabilities/payments/targets/ghost/environment-variables/x\.yml:[0-9]+:[0-9]+: .*without targets/ghost/target\.yml' <<<"$got" || { printf 'bundle without target not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target layout ok"

# envFrom precedence: target env > stage env > app env > environment-variables bundle
for t in us-dev:true onprem-dev:false; do
  v="$(yq "select(.kind == \"Deployment\") | .spec.template.spec.containers[0].env[] | select(.name == \"APP_ENV\") | .value" testdata/golden/apps/payments/my-app/dev/${t%%:*}/*.yaml)"
  [[ $v == "${t#*:}" ]] || { echo "APP_ENV on ${t%%:*}: got '$v' want '${t#*:}' (bundle must not beat app/target env)" >&2; exit 1; }
done
echo "envFrom precedence ok"

# envFrom KEY clash: most specific layer wins; same layer: equal ok, different = error
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments"
printf 'LD_RELAY_URL: https://other\nLD_SDK_KEY: secret:acme-dev-us/launchdarkly-sdk-key\n' >"$c/targets/us-dev/environment-variables/flags.yml"
yq -i '.stages.dev.targets.us-dev.envFrom = ["flags"]' "$c/apps/my-app/app.yml"
scripts/render.sh "$r" payments "$o" >/dev/null
v="$(yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].env[] | select(.name == "LD_RELAY_URL") | .value' "$o/apps/payments/my-app/dev/us-dev/deployment.yaml")"
[[ $v == https://other ]] || { echo "target-level envFrom must win: LD_RELAY_URL=$v" >&2; exit 1; }
yq -i '.stages.dev.targets.us-dev.envFrom = ["flags", "launchdarkly"]' "$c/apps/my-app/app.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}" "${o:?}"
grep -qE '^capabilities/payments/apps/my-app/app\.yml:[0-9]+:[0-9]+: targets\.us-dev\.envFrom: .*LD_RELAY_URL differs between environment-variables/\{flags,launchdarkly\}\.yml \(both in target-level' <<<"$got" || { printf 'same-layer clash not rejected:\n%s\n' "$got" >&2; exit 1; }
grep -q LD_SDK_KEY <<<"$got" && { printf 'equal values must not clash:\n%s\n' "$got" >&2; exit 1; }
echo "envFrom clash ok"

# capability names that prefix each other (pay vs payments): re-rendering one must not wipe the other's deploys
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; cp -R "$r/capabilities/payments" "$r/capabilities/pay"
scripts/render.sh "$r" payments "$o" >/dev/null; scripts/render.sh "$r" pay "$o" >/dev/null; scripts/render.sh "$r" pay "$o" >/dev/null
[[ -f "$o/argocd/deploys/us/payments/my-app-us-dev.json" && -f "$o/argocd/deploys/us/pay/my-app-us-dev.json" ]] || { echo "re-render of pay wiped payments deploys" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "prefix caps ok"

# workload Application names are global per region Argo CD: a duplicate breaks the whole workloads AppSet
o="$(mktemp -d)"; scripts/render.sh testdata/root payments "$o" >/dev/null
scripts/unique-apps.sh "$o" >/dev/null || { echo "unique-apps rejected a clean render" >&2; exit 1; }
mkdir -p "$o/argocd/deploys/us/payments-my"; cp "$o/argocd/deploys/us/payments/my-app-us-dev.json" "$o/argocd/deploys/us/payments-my/app-us-dev.json"
got="$(scripts/unique-apps.sh "$o" 2>&1 || true)"; rm -rf "${o:?}"
grep -q 'duplicate workload Application name payments-my-app-us-dev' <<<"$got" || { printf 'duplicate appName not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "unique app names ok"

# Kargo graph rules on synthetic variants (ADR 0019): skipped stage, 1->N, shared upstream target, multi-upstream naming
synth() { # <cap> <setup cmds...>: copy testdata/root to $r, run setup in it, render <cap> into $o
  r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"
  (cd "$r/capabilities" && eval "$2")
  scripts/render.sh "$r" "$1" "$o" >/dev/null; kk="$o/kargo/$1.yaml"
}
# a) app skips perf: dev fans into staging alone (fan-in node named for staging), staging fans into prod
synth payments 'yq -i "del(.stages.perf)" payments/apps/my-app/app.yml'
[[ -z "$(yq 'select(.kind=="Stage" and .metadata.name=="my-app-dev-done") | .metadata.name' "$kk")" ]] || { echo "a) no dev-done without fan-out" >&2; exit 1; }
want my-app-staging "false:All:my-app-onprem-dev,my-app-us-dev" "$kk"; want my-app-prod "false:All:my-app-onprem-stg,my-app-us-stg" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# b) 1->N: single dev target feeds both prod targets directly
synth search 'printf "dependsOn: [dev]\n" > search/stages/prod.yml
  for t in us-prod onprem-prod; do cp -R payments/targets/$t search/targets/$t; yq -i ".config.namespace = \"search-$t\"" search/targets/$t/target.yml; yq -i ".stages.prod.targets.\"$t\" = {}" search/apps/api/app.yml; done'
[[ "$(stages_of api "$kk")" == "api-onprem-prod api-us-dev api-us-prod" ]] || { echo "b) 1->N needs no control flow: $(stages_of api "$kk")" >&2; exit 1; }
want api-us-prod "false::api-us-dev" "$kk"; want api-onprem-prod "false::api-us-dev" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# c) worker-us-dev feeds staging (2 targets) and perf: staging needs its own entry; prod joins 3 sources directly (M->1)
synth payments 'yq -i ".stages.perf.targets.us-perf = {} | .stages.staging.targets.onprem-stg = {}" payments/apps/worker/app.yml'
want worker-staging "false::worker-us-dev" "$kk"; want worker-us-stg "false::worker-staging" "$kk"; want worker-us-perf "false::worker-us-dev" "$kk"
want worker-us-prod "false:All:worker-onprem-stg,worker-us-perf,worker-us-stg" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# d) qa and prod both depend on staging+perf: one shared node named after both upstream stages
synth payments 'printf "dependsOn: [staging, perf]\n" > payments/stages/qa.yml
  for t in us onprem; do cp -R payments/targets/$t-stg payments/targets/$t-qa; yq -i ".config.namespace = \"payments-$t-qa\"" payments/targets/$t-qa/target.yml; yq -i ".stages.qa.targets.\"$t-qa\" = {}" payments/apps/my-app/app.yml; done'
want my-app-perf-staging-done "false:All:my-app-onprem-stg,my-app-us-perf,my-app-us-stg" "$kk"
want my-app-prod "false::my-app-perf-staging-done" "$kk"; want my-app-qa "false::my-app-perf-staging-done" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# e) one-click: every stage with >1 target is exactly the downstream set of one Stage (any app in the file)
oneclick "$k"
# f) a single upstream target that also feeds an M->1 join: the multi-target stage it feeds still needs an entry
synth payments 'yq -i "del(.stages.staging.targets.onprem-stg) | del(.stages.prod.targets.onprem-prod)" payments/apps/my-app/app.yml
  printf "dependsOn: [staging]\n" > payments/stages/qa.yml
  for t in us onprem; do cp -R payments/targets/$t-stg payments/targets/$t-qa; yq -i ".config.namespace = \"payments-$t-qa\"" payments/targets/$t-qa/target.yml; yq -i ".stages.qa.targets.\"$t-qa\" = {}" payments/apps/my-app/app.yml; done'
want my-app-qa "false::my-app-us-stg" "$kk"; want my-app-us-qa "false::my-app-qa" "$kk"
want my-app-us-prod "false:All:my-app-us-perf,my-app-us-stg" "$kk"
oneclick "$kk"; rm -rf "${r:?}" "${o:?}"
echo "control-flow graph ok"
