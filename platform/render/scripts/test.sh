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
[[ "$(cat "$tmp/infrastructure/regions/us-east4.json")" == '{"region":"us-east4"}' ]] || { echo "infrastructure/regions/us-east4.json wrong" >&2; exit 1; }
[[ "$(ls "$tmp/infrastructure/regions" | paste -sd' ' -)" == "dc1.json us-east4.json" ]] || { echo "one regions JSON per clusters.cue region" >&2; exit 1; }
[[ -f "$tmp/argocd/projects/dc1/.keep" ]] || { echo "argocd/projects/<r>/.keep missing" >&2; exit 1; }
echo "regions ok"
# kind: one AppProject per capability; no secret store -> no ExternalSecrets / secret env refs
yq -e 'select(.kind == "AppProject" and .metadata.name == "payments")' "$tmp/argocd/projects/us-east4/payments.yaml" >/dev/null || { echo "missing AppProject payments" >&2; exit 1; }
if grep -rqE 'ExternalSecret|secretKeyRef' "$tmp/apps/"; then echo "secret refs rendered for a cluster without a secret store" >&2; exit 1; fi
grep -rq "APP_SECRET" testdata/root/capabilities/payments/apps/my-app/app.yml || { echo "fixture lost its secret: env value" >&2; exit 1; }
# Argo CD dry-runs all resources first: the project namespace must be rendered (labeled for Kargo
# to adopt), not left for Kargo to create from the Project
[[ "$(yq 'select(documentIndex == 0) | .kind + "/" + .metadata.name + "/" + .metadata.labels."kargo.akuity.io/project"' "$tmp/kargo/payments.yaml")" == "Namespace/kargo-payments/true" ]] || { echo "kargo/payments.yaml must start with labeled Namespace kargo-payments" >&2; exit 1; }
# Argo CD visibility: promoters + viewers (emails) get read-only access to their capability's apps, nothing else
want='{"name":"viewer","policies":["p, proj:payments:viewer, applications, get, payments/*, allow"],"groups":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}'
got="$(yq -o=json -I=0 'select(.kind == "AppProject") | .spec.roles[] | {"name": .name, "policies": .policies, "groups": .groups}' "$tmp/argocd/projects/us-east4/payments.yaml")"
[[ "$got" == "$want" ]] || { printf 'AppProject viewer role\nwant: %s\ngot:  %s\n' "$want" "$got" >&2; exit 1; }
# Kargo visibility (email claim): promoters + viewers read their project via Kargo's per-project
# kargo-viewer Role, and list projects via a global SA (kargo-shared-resources) bound to kargo-user
k="$tmp/kargo/payments.yaml"
[[ "$(yq 'select(.kind == "RoleBinding" and .metadata.name == "viewers") | .roleRef.kind + "/" + .roleRef.name + "/" + .subjects[0].name' "$k")" == "Role/kargo-viewer/viewers" ]] || { echo "missing viewers RoleBinding" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "viewers") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}' ]] || { echo "viewers SA must map all promoters + viewers by email" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "users-payments") | .metadata.namespace + " " + .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == 'kargo-shared-resources {"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}' ]] || { echo "missing users-payments global SA" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ClusterRoleBinding" and .metadata.name == "kargo-users-payments") | .roleRef.name + "/" + .subjects[0].namespace + "/" + .subjects[0].name' "$k")" == "kargo-user/kargo-shared-resources/users-payments" ]] || { echo "missing kargo-users-payments ClusterRoleBinding" >&2; exit 1; }
# Kargo's promote API needs create on promotions as well as promote on the (named) stages
for role in $(yq 'select(.kind == "Role" and (.metadata.name | test("^promote-"))) | .metadata.name' "$tmp/kargo/payments.yaml" | grep -v -- "^---"); do
  yq -e "select(.kind == \"Role\" and .metadata.name == \"$role\") | .rules[] | select(.resources[0] == \"promotions\" and (.verbs[] == \"create\"))" "$tmp/kargo/payments.yaml" >/dev/null || { echo "$role can't create promotions" >&2; exit 1; }
done
# promoters are per app: worker's app owners.yml replaces promoters; my-app inherits the capability's
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-worker-dev") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["alice@example.com","carol@example.com","doug@example.com"]}' ]] || { echo "promote-worker-dev claims" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-worker-staging") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["carol@example.com","doug@example.com"]}' ]] || { echo "promote-worker-staging must inherit" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-worker-prod") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$k")" == '{"email":["alice@example.com","carol@example.com"]}' ]] || { echo "promote-worker-prod claims" >&2; exit 1; }
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
[[ "$(stages_of my-app "$k")" == "my-app-dc1-dev-k8s my-app-dc1-npr-k8s my-app-dc1-prd-k8s my-app-dev my-app-dev-done my-app-prod my-app-staging my-app-us-east4-dev-k8s my-app-us-east4-npr-k8s my-app-us-east4-npr-k8s-perf my-app-us-east4-prd-k8s" ]] || { echo "my-app Stage set: $(stages_of my-app "$k")" >&2; exit 1; }
want my-app-dev "true::" "$k"; want my-app-us-east4-dev-k8s "false::my-app-dev" "$k"; want my-app-dc1-dev-k8s "false::my-app-dev" "$k"
want my-app-dev-done "false:All:my-app-dc1-dev-k8s,my-app-us-east4-dev-k8s" "$k"
want my-app-staging "false::my-app-dev-done" "$k"; want my-app-us-east4-npr-k8s "false::my-app-staging" "$k"; want my-app-dc1-npr-k8s "false::my-app-staging" "$k"
want my-app-us-east4-npr-k8s-perf "false::my-app-dev-done" "$k"
want my-app-prod "false:All:my-app-dc1-npr-k8s,my-app-us-east4-npr-k8s,my-app-us-east4-npr-k8s-perf" "$k"
want my-app-us-east4-prd-k8s "false::my-app-prod" "$k"; want my-app-dc1-prd-k8s "false::my-app-prod" "$k"
[[ "$(stages_of worker "$k")" == "worker-us-east4-dev-k8s worker-us-east4-npr-k8s worker-us-east4-prd-k8s" ]] || { echo "worker must have no control flow: $(stages_of worker "$k")" >&2; exit 1; }
want worker-us-east4-dev-k8s "true::" "$k"; want worker-us-east4-npr-k8s "false::worker-us-east4-dev-k8s" "$k"; want worker-us-east4-prd-k8s "false::worker-us-east4-npr-k8s" "$k"
s="$tmp/kargo/search.yaml"
[[ "$(stages_of api "$s")" == "api-us-east4-dev-k8s" ]] || { echo "search api must be one target Stage" >&2; exit 1; }
want api-us-east4-dev-k8s "true::" "$s"
for n in my-app-dev my-app-dev-done my-app-prod my-app-staging; do
  [[ "$(yq "select(.kind==\"Stage\" and .metadata.name==\"$n\") | .metadata.labels.\"example.com/kind\" + \" \" + (.spec | has(\"promotionTemplate\") | tostring) + \" \" + (.spec.shard // \"-\")" "$k")" == "control-flow false -" ]] || { echo "$n must be control-flow: no steps, no shard" >&2; exit 1; }
done
[[ "$(yq 'select(.kind=="Stage" and .metadata.name=="my-app-dev-done") | .metadata.labels."example.com/stage"' "$k") $(yq 'select(.kind=="Stage" and .metadata.name=="my-app-staging") | .metadata.labels."example.com/stage"' "$k")" == "dev staging" ]] || { echo "control-flow stage labels" >&2; exit 1; }
[[ "$(yq 'select(.kind=="ProjectConfig") | .spec.promotionPolicies[] | .stageSelector.name' "$k" | sort | paste -sd' ' -)" == "infra-us-east4-dev-tf my-app-dc1-dev-k8s my-app-dev my-app-dev-done my-app-prod my-app-staging my-app-us-east4-dev-k8s payments-us-east4-dev-k8s worker-us-east4-dev-k8s" ]] || { echo "auto-promotion: every control-flow Stage + targets of autoPromote stages only" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Role" and .metadata.name == "promote-worker-dev") | .rules[0].resourceNames | join(",")' "$k")" == "worker-us-east4-dev-k8s" ]] || { echo "promote-worker-dev resourceNames" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Role" and .metadata.name == "promote-worker-staging") | .rules[0].resourceNames | join(",")' "$k")" == "worker-us-east4-npr-k8s" ]] || { echo "promote-worker-staging resourceNames" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Role" and .metadata.name == "promote-worker-prod") | .rules[0].resourceNames | join(",")' "$k")" == "worker-us-east4-prd-k8s" ]] || { echo "promote-worker-prod resourceNames" >&2; exit 1; }
[[ -z "$(yq 'select(.kind == "Role" and (.metadata.name == "promote-worker-perf" or .metadata.name == "promote-worker" or .metadata.name == "promote-my-app")) | .metadata.name' "$k")" ]] || { echo "unexpected promote role" >&2; exit 1; }
for st in staging perf; do
  yq -e "select(.kind == \"Role\" and .metadata.name == \"promote-my-app-$st\") | .rules[0].resourceNames[] | select(. == \"my-app-dev-done\")" "$k" >/dev/null || { echo "promote-my-app-$st must include shared my-app-dev-done" >&2; exit 1; }
done
[[ "$(yq 'select(.kind == "Role" and (.metadata.name | test("^promote-my-app-"))) | .rules[0].resourceNames[]' "$k" | grep -v -- "^---" | sort -u | paste -sd, -)" == "my-app-dc1-dev-k8s,my-app-dc1-npr-k8s,my-app-dc1-prd-k8s,my-app-dev,my-app-dev-done,my-app-prod,my-app-staging,my-app-us-east4-dev-k8s,my-app-us-east4-npr-k8s,my-app-us-east4-npr-k8s-perf,my-app-us-east4-prd-k8s" ]] || { echo "promote-my-app-* union must cover targets + control flow" >&2; exit 1; }
[[ "$(yq 'select(.kind == "Stage" and .metadata.name == "my-app-dc1-prd-k8s") | .spec.promotionTemplate.spec.vars[2].value' "$k")" == "env/payments/my-app/dc1-prd-k8s" ]] || { echo "target stage must push its own branch" >&2; exit 1; }
d="$tmp/argocd/deploys/dc1/payments/my-app-dc1-prd-k8s.json"
[[ "$(jq -r '.appName + " " + .branch + " " + .authorizedStage + " " + .destination + "/" + .namespace' "$d")" == "payments-my-app-dc1-prd-k8s env/payments/my-app/dc1-prd-k8s kargo-payments:my-app-dc1-prd-k8s in-cluster/payments-onprem-prod" ]] || { echo "deploy params must track the target branch, authorized for its target stage" >&2; exit 1; }
# workloads AppSet (platform/appsets/workloads.yaml) reads exactly these keys; git files gen reserves path/values
[[ "$(jq -r 'keys | join(",")' "$d")" == "app,appName,authorizedStage,branch,capability,destination,namespace,stage,target" ]] || { echo "deploy params keys changed: $(jq -c keys "$d")" >&2; exit 1; }
# regions: target Stages run on their cluster's region shard; control-flow Stages and Warehouses stay on the hub controller
[[ "$(yq 'select(.kind == "Stage") | .metadata.name + "=" + (.spec.shard // "-")' "$k" | grep '^my-app-' | sort | paste -sd' ' -)" == "my-app-dc1-dev-k8s=dc1 my-app-dc1-npr-k8s=dc1 my-app-dc1-prd-k8s=dc1 my-app-dev-done=- my-app-dev=- my-app-prod=- my-app-staging=- my-app-us-east4-dev-k8s=us-east4 my-app-us-east4-npr-k8s-perf=us-east4 my-app-us-east4-npr-k8s=us-east4 my-app-us-east4-prd-k8s=us-east4" ]] || { echo "shard must be set on target Stages only, from the target's cluster region" >&2; exit 1; }
if yq -e 'select(.kind == "Warehouse") | .spec.shard' "$k" >/dev/null 2>&1; then echo "Warehouses must not be sharded" >&2; exit 1; fi
# Argo CD output is per region; destination is the regional Argo CD's own cluster
[[ "$(cd "$tmp/argocd" && find . -type f ! -name .keep | sort | paste -sd' ' -)" == "./deploys/dc1/payments/my-app-dc1-dev-k8s.json ./deploys/dc1/payments/my-app-dc1-prd-k8s.json ./deploys/us-east4/payments/my-app-us-east4-dev-k8s.json ./deploys/us-east4/payments/my-app-us-east4-npr-k8s-perf.json ./deploys/us-east4/payments/my-app-us-east4-npr-k8s.json ./deploys/us-east4/payments/my-app-us-east4-prd-k8s.json ./deploys/us-east4/payments/payments-us-east4-dev-k8s.json ./deploys/us-east4/payments/payments-us-east4-npr-k8s.json ./deploys/us-east4/payments/payments-us-east4-prd-k8s.json ./deploys/us-east4/payments/worker-us-east4-npr-k8s.json ./deploys/us-east4/payments/worker-us-east4-prd-k8s.json ./deploys/us-east4/search/api-us-east4-dev-k8s.json ./projects/dc1/payments.yaml ./projects/us-east4/payments.yaml ./projects/us-east4/search.yaml" ]] || { echo "argocd output must be per region: deploys/<r>/<cap>/ + projects/<r>/<cap>.yaml" >&2; exit 1; }
if grep -rq 'kind: Application$' "$tmp/argocd"; then echo "renderer must not emit Applications (workloads AppSet does)" >&2; exit 1; fi
[[ "$(yq 'select(.kind == "AppProject") | .spec.destinations[] | .name + "/" + .namespace' "$tmp/argocd/projects/dc1/payments.yaml" | paste -sd' ' -)" == "in-cluster/payments-onprem-dev in-cluster/payments-onprem-prod" ]] || { echo "onprem AppProject must list only onprem destinations" >&2; exit 1; }
if grep -l dc1 "$tmp"/argocd/deploys/us-east4/*/*.json >/dev/null 2>&1; then echo "us-east4 deploys contain dc1 targets" >&2; exit 1; fi
if grep -q '"groups"' "$tmp"/kargo/*.yaml; then echo "no Kargo claim may use groups" >&2; exit 1; fi
echo "kind cluster ok"
# GHA targets (deployWith: gha): env branch written, then dispatch + wait instead of argocd-update
st() { yq "select(.kind==\"Stage\" and .metadata.name==\"$1\") | $2" "$k" | sed '/^$/d;/^---$/d'; }  # yq prints blank lines / separators for unselected documents
[[ "$(st worker-us-east4-dev-k8s '[.spec.promotionTemplate.spec.steps[].uses] | join(",")')" == "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" ]] || { echo "worker-us-east4-dev-k8s steps" >&2; exit 1; }
[[ "$(st worker-us-east4-npr-k8s '.spec.promotionTemplate.spec.steps[-1].uses')" == "argocd-update" ]] || { echo "worker-us-east4-npr-k8s must stay argocd" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s '.metadata.labels."example.com/deploy-with"') $(st worker-us-east4-npr-k8s '.metadata.labels."example.com/deploy-with"')" == "gha argocd" ]] || { echo "deploy-with label" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s '[.spec.promotionTemplate.spec.vars[].name] | join(",")')" == "repo,src,branch,image,workflowRepo,workflowFile,workflowRef" ]] || { echo "vars order (kargo-restore reads by name)" >&2; exit 1; }
[[ "$(st worker-us-east4-npr-k8s '[.spec.promotionTemplate.spec.vars[].name] | join(",")')" == "repo,src,branch,image" ]] || { echo "argocd Stage keeps its four vars" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "pitoniak32/gitops-cd deploy.yaml main" ]] || { echo "default workflow" >&2; exit 1; }
[[ "$(st my-app-dc1-npr-k8s '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "acme/deploys onprem.yml v1" ]] || { echo "target workflow override" >&2; exit 1; }
d1='.spec.promotionTemplate.spec.steps[] | select(.as == "dispatch")'
[[ "$(st worker-us-east4-dev-k8s "$d1 | .config.method + \" \" + .config.url")" == 'POST https://api.github.com/repos/${{ vars.workflowRepo }}/actions/workflows/${{ vars.workflowFile }}/dispatches' ]] || { echo "dispatch url" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s "$d1 | .config.headers[] | select(.name == \"Authorization\") | .value")" == "Bearer \${{ sharedSecret('github-actions').token }}" ]] || { echo "auth header" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s "$d1 | .config.successExpression + \" | \" + .config.failureExpression + \" | \" + .config.errorExpression")" == "response.status == 200 | response.status != 200 | response.body.message" ]] || { echo "dispatch expressions" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s "$d1 | [.config.outputs[] | .name + \"=\" + .fromExpression] | join(\",\")")" == "runID=response.body.workflow_run_id,runURL=response.body.html_url" ]] || { echo "dispatch outputs" >&2; exit 1; }
for f in "'ref': vars.workflowRef" "'return_run_details': true" "'commit': outputs.commit.commit" "'target': 'us-east4-dev-k8s'" "imageFrom(vars.image).Digest"; do
  st worker-us-east4-dev-k8s "$d1 | .config.body" | grep -qF "$f" || { echo "dispatch body missing $f" >&2; exit 1; }
done
w1='.spec.promotionTemplate.spec.steps[] | select(.as == "wait")'
[[ "$(st worker-us-east4-dev-k8s "$w1 | .retry.timeout + \" \" + .config.url")" == '30m https://api.github.com/repos/${{ vars.workflowRepo }}/actions/runs/${{ outputs.dispatch.runID }}' ]] || { echo "wait step" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s "$w1 | .config.successExpression")" == "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'" ]] || { echo "wait success" >&2; exit 1; }
# 404: the dispatched run isn't readable yet (GitHub returns its id before the API serves it): keep polling
[[ "$(st worker-us-east4-dev-k8s "$w1 | .config.failureExpression")" == "(response.status != 200 && response.status != 404) || (response.status == 200 && response.body.status == 'completed' && response.body.conclusion != 'success')" ]] || { echo "wait failure (404 must keep polling)" >&2; exit 1; }
[[ "$(st worker-us-east4-dev-k8s "$w1 | .config.errorExpression")" == "response.status != 200 ? response.body.message : 'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url" ]] || { echo "wait error message (non-200 has no run fields)" >&2; exit 1; }
# Kargo ends a ${{ }} expression at the first "}}": a nested map literal must not close with "}}"
if grep -nE '[^ ]\}\}' "$k" testdata/golden/kargo/*.yaml; then echo 'Kargo expression cut short: "}}" not preceded by a space' >&2; exit 1; fi
echo "gha targets ok"
# terraform apps (ADR 0021): one terraform.yaml per target, no k8s manifests, no Argo CD params
tf="$tmp/apps/payments/infra"
[[ "$(ls "$tf/dev/us-east4-dev-tf" | paste -sd' ' -)" == "terraform.yaml" ]] || { echo "terraform target renders only terraform.yaml: $(ls "$tf/dev/us-east4-dev-tf")" >&2; exit 1; }
want='{"capability":"payments","app":"infra","stage":"dev","target":"us-east4-dev-tf","environment":"dev","gcpProject":"team1-dev","region":"us-east4","wifEmail":"team1-dev@team1-dev.iam.gserviceaccount.com","backend":{"bucket":"tfstate-team1-dev","prefix":"payments/infra/us-east4-dev-tf"},"module":"ghcr.io/pitoniak32/tf-bucket","env":{"TF_VAR_bucket_name":"payments-dev","TF_VAR_region":"us-central1"}}'
got="$(yq -o=json -I=0 '.' "$tf/dev/us-east4-dev-tf/terraform.yaml")"
[[ "$got" == "$want" ]] || { printf 'us-east4-dev-tf terraform.yaml\nwant: %s\ngot:  %s\n' "$want" "$got" >&2; exit 1; }
[[ "$(yq '.backend.prefix + " " + .env.TF_VAR_region' "$tf/prod/us-east4-prd-tf/terraform.yaml")" == "payments/custom us-east1" ]] || { echo "us-east4-prd-tf prefix override / target env" >&2; exit 1; }
if ls "$tmp"/argocd/deploys/*/payments/infra-* >/dev/null 2>&1; then echo "terraform targets must not get Argo CD params" >&2; exit 1; fi
echo "terraform render ok"
# terraform Kargo: Warehouse watches the module as an image (no platform), Stage runs on the hub
# controller (no shard), pins module in terraform.yaml, dispatches with `module`, waits 60m
[[ "$(yq 'select(.kind=="Warehouse" and .metadata.name=="infra") | .spec.subscriptions[1].image | .repoURL + " " + .imageSelectionStrategy + " " + .constraint + " " + (.discoveryLimit|tostring) + " " + (has("platform")|tostring)' "$k")" == "ghcr.io/pitoniak32/tf-bucket SemVer ^1 10 false" ]] || { echo "infra Warehouse module subscription" >&2; exit 1; }
[[ "$(st infra-us-east4-dev-tf '(.spec.shard // "-") + " " + .metadata.labels."example.com/platform" + " " + .metadata.labels."example.com/deploy-with"')" == "- terraform gha" ]] || { echo "infra-us-east4-dev-tf: no shard, platform/deploy-with labels" >&2; exit 1; }
[[ "$(st my-app-us-east4-dev-k8s '.metadata.labels."example.com/platform"')" == "k8s" ]] || { echo "k8s platform label" >&2; exit 1; }
[[ "$(st infra-us-east4-dev-tf '[.spec.promotionTemplate.spec.steps[].uses] | join(",")')" == "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" ]] || { echo "infra-us-east4-dev-tf steps" >&2; exit 1; }
[[ "$(st infra-us-east4-dev-tf '.spec.promotionTemplate.spec.steps[] | select(.uses == "yaml-update") | .config.path + " " + .config.updates[0].key + " " + .config.updates[0].value')" == './out/terraform.yaml module ${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}' ]] || { echo "pin module in terraform.yaml" >&2; exit 1; }
[[ "$(st infra-us-east4-dev-tf '[.spec.promotionTemplate.spec.vars[].name] | join(",")')" == "repo,src,branch,image,workflowRepo,workflowFile,workflowRef" ]] || { echo "terraform vars (kargo-restore reads image by name)" >&2; exit 1; }
[[ "$(st infra-us-east4-dev-tf '.spec.promotionTemplate.spec.vars[4:] | map(.value) | join(" ")')" == "pitoniak32/gitops-cd terraform.yaml v1" ]] || { echo "terraform workflow defaults/override" >&2; exit 1; }
st infra-us-east4-dev-tf "$d1 | .config.body" | grep -qF "'module': vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest" || { echo "dispatch must send module" >&2; exit 1; }
if st infra-us-east4-dev-tf "$d1 | .config.body" | grep -qF "'image':"; then echo "terraform dispatch must not send image" >&2; exit 1; fi
st infra-us-east4-dev-tf '.spec.promotionTemplate.spec.steps[] | select(.uses == "git-commit") | .config.message' | grep -qF ', module ${{ imageFrom(vars.image).Tag }}' || { echo "commit message names module tag (kargo-restore parses it)" >&2; exit 1; }
[[ "$(st infra-us-east4-dev-tf "$w1 | .retry.timeout") $(st worker-us-east4-dev-k8s "$w1 | .retry.timeout")" == "60m 30m" ]] || { echo "wait timeout terraform 60m, k8s 30m" >&2; exit 1; }
echo "terraform kargo ok"
# ADR 0022: Warehouses watch the rendered branch, not main
[[ "$(yq 'select(.kind=="Warehouse") | .spec.subscriptions[0].git.branch' "$k" "$s" | grep -v '^---$' | sort -u)" == "rendered" ]] || { echo "Warehouse git branch must be rendered" >&2; exit 1; }
echo "rendered branch ok"
# editor schema (generic export) accepts both platforms: nothing platform-specific is required
[[ "$(jq -c '.properties.config.required' schemas/target.json)" == '["auth","billing","environment","environmentType"]' ]] || { echo "target.json config.required: $(jq -c '.properties.config.required' schemas/target.json)" >&2; exit 1; }
[[ "$(jq -c '.properties.config.properties | [has("gcpProject"), has("backend"), has("terraform")]' schemas/target.json)" == '[true,true,false]' ]] || { echo "target.json must describe flat gcpProject/backend" >&2; exit 1; }
echo "target schema ok"
# deployWith/workflow resolve per field, target > stage > app > default
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments/apps/my-app/app.yml"
yq -i '.workflow.ref = "release" | .stages.dev.deployWith = "gha" | .stages.dev.targets.dc1-dev-k8s = {"deployWith": "argocd", "workflow": {"file": "x.yml"}} | .stages.dev.targets.us-east4-dev-k8s.workflow.repo = "acme/deploys"' "$c"
scripts/lint.sh "$r" payments >/dev/null || { echo "app-level workflow with argocd targets must lint clean" >&2; exit 1; }
scripts/render.sh "$r" payments "$o" >/dev/null
pt() { jq -r ".\"my-app\".stages.$1.targets.\"$2\".deployWith" "$o/pipelines/payments.json"; }
[[ "$(pt dev us-east4-dev-k8s) $(pt dev dc1-dev-k8s) $(pt prod us-east4-prd-k8s)" == "gha argocd argocd" ]] || { echo "deployWith resolution: $(pt dev us-east4-dev-k8s) $(pt dev dc1-dev-k8s) $(pt prod us-east4-prd-k8s)" >&2; exit 1; }
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

# A forbidden field is reported by its "not allowed" line only: first line, no spurious pattern lines
# (<file> <expr> <first-line ERE> <ERE that must NOT appear>)
nospur() {
  c="$(mktemp -d)"; cp -R testdata/root/. "$c/"
  yq -i "$2" "$c/$1"
  got="$(scripts/lint.sh "$c" payments || true)"; rm -rf "$c"
  [[ "$(head -n1 <<<"$got")" =~ $3 ]] || { printf 'first lint line wrong: %s\nwant: %s\ngot:\n%s\n' "$2" "$3" "$got" >&2; exit 1; }
  ! grep -qE 'expected a GCP project id|unknown region|unknown cluster' <<<"$got" || { printf 'spurious pattern line: %s\ngot:\n%s\n' "$2" "$got" >&2; exit 1; }
  echo "no spurious lines: $2"
}
tg=capabilities/payments/targets
nospur $tg/us-east4-dev-k8s/target.yml '.config.gcpProject = "acme-payments-dev"' 'gcpProject not allowed on a K8S target'
nospur $tg/us-east4-dev-k8s/target.yml '.config.region = "us-east4"' 'region not allowed on a K8S target'
nospur $tg/us-east4-npr-cr/target.yml '.config.cluster = "us-east4"' 'cluster not allowed on a CLOUD_RUN target'
nospur $tg/us-east4-npr-cr/target.yml '.config.namespace = "payments"' 'namespace not allowed on a CLOUD_RUN target'
nospur $tg/us-east4-dev-tf/target.yml '.config.cluster = "us-east4"' 'cluster not allowed on a TERRAFORM target'
nospur $tg/us-east4-dev-tf/target.yml '.config.namespace = "payments"' 'namespace not allowed on a TERRAFORM target'

# Every app overrides promoters: the capability-level promoter gets no promote role but still
# views; stages/targets approvers (CODEOWNERS) are the apps' owners.
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"
printf 'codeOwners: [team1]\npromoters: {dev: [carol@example.com]}\napps: {api: {codeOwners: [team2], promoters: {dev: [dave@example.com]}}}\n' >"$r/capabilities/search/owners.yml"
scripts/render.sh "$r" search "$o" >/dev/null
[[ -f "$o/kargo/search.yaml" ]] || { echo "missing $o/kargo/search.yaml" >&2; exit 1; }
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "promote-api-dev") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$o/kargo/search.yaml")" == '{"email":["dave@example.com"]}' ]] || { echo "capability-only promoter got promote rights" >&2; exit 1; }
grep -qx '/capabilities/search/ @acme/team2' "$o/codeowners/search" || { echo "stages/targets approvers must be the apps' owners:" >&2; cat "$o/codeowners/search" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "override-all owners ok"

# App viewers (apps.<a>.viewers) are included in the viewers SA claims
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"
yq -i '.apps.worker.viewers = ["erin@example.com"]' "$r/capabilities/payments/owners.yml"
scripts/render.sh "$r" payments "$o" >/dev/null
[[ "$(yq 'select(.kind == "ServiceAccount" and .metadata.name == "viewers") | .metadata.annotations."rbac.kargo.akuity.io/claims"' "$o/kargo/payments.yaml")" == *erin@example.com* ]] || { echo "app viewer missing from viewers SA" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "app viewers ok"

# Leftover apps/<a>/owners.yml (pre-ADR 0023 layout) fails loudly
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
printf 'owners: [team1]\n' >"$r/capabilities/payments/apps/worker/owners.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE 'apps/worker/owners\.yml:[0-9]+:[0-9]+: .*move into capabilities/payments/owners\.yml apps\.worker' <<<"$got" || { printf 'legacy app owners.yml not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "legacy app owners ok"
# no Role may ever render an empty resourceNames
ls testdata/golden/kargo/*.yaml >/dev/null || exit 1
if yq -e 'select(.kind == "Role") | .rules[] | select(has("resourceNames") and (.resourceNames | length) == 0)' testdata/golden/kargo/*.yaml >/dev/null 2>&1; then echo "empty resourceNames in golden" >&2; exit 1; fi

# Invalid app/stage/target directory names fail lint (they become k8s/Kargo names)
for d in apps/worker stages/perf targets/dc1-dev-k8s; do
  r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments"
  mv "$c/$d" "$c/$(dirname "$d")/Bad_Name" 2>/dev/null || mv "$c/$d.yml" "$c/$(dirname "$d")/Bad_Name.yml"
  got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
  grep -qE '^capabilities/payments/[a-z]+/Bad_Name(/app|/target)?\.yml:[0-9]+:[0-9]+: .*invalid name' <<<"$got" || { printf 'bad name not rejected for %s:\n%s\n' "$d" "$got" >&2; exit 1; }
  echo "bad name ok: $d"
done

# A target named like a stage would collide as a Kargo Stage name (<app>-<name>)
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
cp -R "$r/capabilities/payments/targets/dc1-dev-k8s" "$r/capabilities/payments/targets/dev"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/dev/target\.yml:[0-9]+:[0-9]+: .*also a stage name' <<<"$got" || { printf 'target/stage name collision not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target/stage name collision ok"
# ...nor like <stage>-done (fan-out control-flow Stage <app>-<stage>-done, ADR 0019)
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"
cp -R "$r/capabilities/payments/targets/dc1-dev-k8s" "$r/capabilities/payments/targets/dev-done"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/dev-done/target\.yml:[0-9]+:[0-9]+: .*target name dev-done is <stage>-done; Kargo control-flow stage names <app>-dev-done would collide' <<<"$got" || { printf 'target named <stage>-done not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target <stage>-done collision ok"

# Target layout: old targets/<t>.yml and bundles without target.yml fail lint
r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; t="$r/capabilities/payments/targets"
cp "$t/dc1-dev-k8s/target.yml" "$t/old.yml"
mkdir -p "$t/ghost/environment-variables"; echo 'A: b' >"$t/ghost/environment-variables/x.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
grep -qE '^capabilities/payments/targets/old\.yml:1:1: .*move to targets/old/target\.yml' <<<"$got" || { printf 'legacy target file not rejected:\n%s\n' "$got" >&2; exit 1; }
grep -qE '^capabilities/payments/targets/ghost/environment-variables/x\.yml:[0-9]+:[0-9]+: .*without targets/ghost/target\.yml' <<<"$got" || { printf 'bundle without target not rejected:\n%s\n' "$got" >&2; exit 1; }
echo "target layout ok"

# envFrom precedence: target env > stage env > app env > environment-variables bundle
for t in us-east4-dev-k8s:true dc1-dev-k8s:false; do
  v="$(yq "select(.kind == \"Deployment\") | .spec.template.spec.containers[0].env[] | select(.name == \"APP_ENV\") | .value" testdata/golden/apps/payments/my-app/dev/${t%%:*}/*.yaml)"
  [[ $v == "${t#*:}" ]] || { echo "APP_ENV on ${t%%:*}: got '$v' want '${t#*:}' (bundle must not beat app/target env)" >&2; exit 1; }
done
echo "envFrom precedence ok"

# envFrom KEY clash: most specific layer wins; same layer: equal ok, different = error
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments"
printf 'LD_RELAY_URL: https://other\nLD_SDK_KEY: secret:acme-dev-us/launchdarkly-sdk-key\n' >"$c/targets/us-east4-dev-k8s/environment-variables/flags.yml"
yq -i '.stages.dev.targets.us-east4-dev-k8s.envFrom = ["flags"]' "$c/apps/my-app/app.yml"
scripts/render.sh "$r" payments "$o" >/dev/null
v="$(yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].env[] | select(.name == "LD_RELAY_URL") | .value' "$o/apps/payments/my-app/dev/us-east4-dev-k8s/deployment.yaml")"
[[ $v == https://other ]] || { echo "target-level envFrom must win: LD_RELAY_URL=$v" >&2; exit 1; }
yq -i '.stages.dev.targets.us-east4-dev-k8s.envFrom = ["flags", "launchdarkly"]' "$c/apps/my-app/app.yml"
got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}" "${o:?}"
grep -qE '^capabilities/payments/apps/my-app/app\.yml:[0-9]+:[0-9]+: targets\.us-east4-dev-k8s\.envFrom: .*LD_RELAY_URL differs between environment-variables/\{flags,launchdarkly\}\.yml \(both in target-level' <<<"$got" || { printf 'same-layer clash not rejected:\n%s\n' "$got" >&2; exit 1; }
grep -q LD_SDK_KEY <<<"$got" && { printf 'equal values must not clash:\n%s\n' "$got" >&2; exit 1; }
echo "envFrom clash ok"

# capability names that prefix each other (pay vs payments): re-rendering one must not wipe the other's deploys
r="$(mktemp -d)"; o="$(mktemp -d)"; cp -R testdata/root/. "$r/"; cp -R "$r/capabilities/payments" "$r/capabilities/pay"
scripts/render.sh "$r" payments "$o" >/dev/null; scripts/render.sh "$r" pay "$o" >/dev/null; scripts/render.sh "$r" pay "$o" >/dev/null
[[ -f "$o/argocd/deploys/us-east4/payments/my-app-us-east4-dev-k8s.json" && -f "$o/argocd/deploys/us-east4/pay/my-app-us-east4-dev-k8s.json" ]] || { echo "re-render of pay wiped payments deploys" >&2; exit 1; }
rm -rf "${r:?}" "${o:?}"; echo "prefix caps ok"

# workload Application names are global per region Argo CD: a duplicate breaks the whole workloads AppSet
o="$(mktemp -d)"; scripts/render.sh testdata/root payments "$o" >/dev/null
scripts/unique-apps.sh "$o" >/dev/null || { echo "unique-apps rejected a clean render" >&2; exit 1; }
mkdir -p "$o/argocd/deploys/us-east4/payments-my"; cp "$o/argocd/deploys/us-east4/payments/my-app-us-east4-dev-k8s.json" "$o/argocd/deploys/us-east4/payments-my/app-us-east4-dev-k8s.json"
got="$(scripts/unique-apps.sh "$o" 2>&1 || true)"; rm -rf "${o:?}"
grep -q 'duplicate workload Application name payments-my-app-us-east4-dev-k8s' <<<"$got" || { printf 'duplicate appName not rejected:\n%s\n' "$got" >&2; exit 1; }
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
want my-app-staging "false:All:my-app-dc1-dev-k8s,my-app-us-east4-dev-k8s" "$kk"; want my-app-prod "false:All:my-app-dc1-npr-k8s,my-app-us-east4-npr-k8s" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# b) 1->N: single dev target feeds both prod targets directly
synth search 'printf "dependsOn: [dev]\n" > search/stages/prod.yml
  yq -i ".promoters.prod = [\"alice@example.com\"]" search/owners.yml
  for t in us-east4-prd-k8s dc1-prd-k8s; do cp -R payments/targets/$t search/targets/$t; yq -i ".config.namespace = \"search-$t\"" search/targets/$t/target.yml; yq -i ".stages.prod.targets.\"$t\" = {}" search/apps/api/app.yml; done'
[[ "$(stages_of api "$kk")" == "api-dc1-prd-k8s api-us-east4-dev-k8s api-us-east4-prd-k8s" ]] || { echo "b) 1->N needs no control flow: $(stages_of api "$kk")" >&2; exit 1; }
want api-us-east4-prd-k8s "false::api-us-east4-dev-k8s" "$kk"; want api-dc1-prd-k8s "false::api-us-east4-dev-k8s" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# c) worker-us-east4-dev-k8s feeds staging (2 targets) and perf: staging needs its own entry; prod joins 3 sources directly (M->1)
synth payments 'yq -i ".stages.perf.targets.us-east4-npr-k8s-perf = {} | .stages.staging.targets.dc1-npr-k8s = {}" payments/apps/worker/app.yml'
want worker-staging "false::worker-us-east4-dev-k8s" "$kk"; want worker-us-east4-npr-k8s "false::worker-staging" "$kk"; want worker-us-east4-npr-k8s-perf "false::worker-us-east4-dev-k8s" "$kk"
want worker-us-east4-prd-k8s "false:All:worker-dc1-npr-k8s,worker-us-east4-npr-k8s,worker-us-east4-npr-k8s-perf" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# d) qa and prod both depend on staging+perf: one shared node named after both upstream stages
synth payments 'printf "dependsOn: [staging, perf]\n" > payments/stages/qa.yml
  yq -i ".promoters.qa = [\"doug@example.com\"]" payments/owners.yml
  for t in us-east4 dc1; do cp -R payments/targets/$t-npr-k8s payments/targets/$t-npr-k8s-qa; yq -i ".config.namespace = \"payments-$t-qa\"" payments/targets/$t-npr-k8s-qa/target.yml; yq -i ".stages.qa.targets.\"$t-npr-k8s-qa\" = {}" payments/apps/my-app/app.yml; done'
want my-app-perf-staging-done "false:All:my-app-dc1-npr-k8s,my-app-us-east4-npr-k8s,my-app-us-east4-npr-k8s-perf" "$kk"
want my-app-prod "false::my-app-perf-staging-done" "$kk"; want my-app-qa "false::my-app-perf-staging-done" "$kk"
oneclick "$kk"
rm -rf "${r:?}" "${o:?}"
# e) one-click: every stage with >1 target is exactly the downstream set of one Stage (any app in the file)
oneclick "$k"
# f) a single upstream target that also feeds an M->1 join: the multi-target stage it feeds still needs an entry
synth payments 'yq -i "del(.stages.staging.targets.dc1-npr-k8s) | del(.stages.prod.targets.dc1-prd-k8s)" payments/apps/my-app/app.yml
  printf "dependsOn: [staging]\n" > payments/stages/qa.yml
  yq -i ".promoters.qa = [\"doug@example.com\"]" payments/owners.yml
  for t in us-east4 dc1; do cp -R payments/targets/$t-npr-k8s payments/targets/$t-npr-k8s-qa; yq -i ".config.namespace = \"payments-$t-qa\"" payments/targets/$t-npr-k8s-qa/target.yml; yq -i ".stages.qa.targets.\"$t-npr-k8s-qa\" = {}" payments/apps/my-app/app.yml; done'
want my-app-qa "false::my-app-us-east4-npr-k8s" "$kk"; want my-app-us-east4-npr-k8s-qa "false::my-app-qa" "$kk"
want my-app-us-east4-prd-k8s "false:All:my-app-us-east4-npr-k8s,my-app-us-east4-npr-k8s-perf" "$kk"
oneclick "$kk"; rm -rf "${r:?}" "${o:?}"
echo "control-flow graph ok"

# A cluster that narrows its environmentTypes rejects other types, naming what it accepts
# (copy of the renderer: clusters.cue is platform-owned, so the case can't be a lint-cases.tsv edit)
p="$(mktemp -d)"; r="$(mktemp -d)"; cp -R ../cue.mod "$p/"; cp -R . "$p/render"; cp -R testdata/root/. "$r/"
sed -i.bak 's/dc1: {region: "dc1", kind: "kind", environmentTypes: \["dev", "npr", "prd"\]/dc1: {region: "dc1", kind: "kind", environmentTypes: ["dev"]/' "$p/render/clusters.cue"
grep -q 'environmentTypes: \["dev"\]' "$p/render/clusters.cue" || { echo "cluster-types case: sed did not narrow dc1" >&2; exit 1; }
got="$("$p/render/scripts/lint.sh" "$r" payments || true)"; rm -rf "${p:?}" "${r:?}"
grep -qE '^capabilities/payments/targets/dc1-prd-k8s/target\.yml:[0-9]+:[0-9]+: config\.environmentType: cluster dc1 accepts environment types: dev$' <<<"$got" || { printf 'cluster environment-type check failed:\n%s\n' "$got" >&2; exit 1; }
echo "cluster types ok"

# Target name must be <region>-<env>-<platform>[-<qualifier>] (ADR 0025); dir renames, so not lint-cases.tsv edits
name_case() { # <from> <to> <want: ok | grep -E pattern>
  r="$(mktemp -d)"; cp -R testdata/root/. "$r/"; c="$r/capabilities/payments/targets"
  mv "$c/$1" "$c/$2"; perl -pi -e "s/(?<![\\w-])$1(?![\\w-])/$2/g" "$r"/capabilities/payments/apps/*/app.yml
  got="$(scripts/lint.sh "$r" payments || true)"; rm -rf "${r:?}"
  if [[ $3 == ok ]]; then
    [[ -z "$got" ]] || { printf 'name %s should pass:\n%s\n' "$2" "$got" >&2; exit 1; }
  else
    ! grep -qE 'expected K8S, TERRAFORM or CLOUD_RUN|expected K8S or TERRAFORM|empty disjunction|invalid interpolation' <<<"$got" || { printf 'name %s: noisy output:\n%s\n' "$2" "$got" >&2; exit 1; }
    grep -qE "$3" <<<"$got" || { printf 'name %s not rejected as expected:\n%s\n' "$2" "$got" >&2; exit 1; }
  fi
  echo "target name ok: $2"
}
name_case us-east4-npr-k8s dc1-npr-k8s-x '^capabilities/payments/targets/dc1-npr-k8s-x/target\.yml:[0-9]+:[0-9]+: .*name must be <region>-<env>-<platform>\[-<qualifier>\]: expected us-east4-npr-k8s\[-<qualifier>\]'
name_case us-east4-npr-k8s us-east4-npr-k8sperf '^capabilities/payments/targets/us-east4-npr-k8sperf/target\.yml:[0-9]+:[0-9]+: .*expected us-east4-npr-k8s\[-<qualifier>\]'
name_case us-east4-npr-k8s us-east4-npr-k8s- '^capabilities/payments/targets/us-east4-npr-k8s-/target\.yml:[0-9]+:[0-9]+: '
name_case us-east4-npr-k8s us-east4-npr-k8s-blue ok
name_case us-east4-npr-cr us-east4-npr-k8s-x '^capabilities/payments/targets/us-east4-npr-k8s-x/target\.yml:[0-9]+:[0-9]+: .*name must be <region>-<env>-<platform>\[-<qualifier>\]: expected us-east4-npr-cr\[-<qualifier>\]'
name_case us-east4-npr-cr dc1-npr-cr '^capabilities/payments/targets/dc1-npr-cr/target\.yml:[0-9]+:[0-9]+: .*expected us-east4-npr-cr\[-<qualifier>\]'
name_case us-east4-npr-cr us-east4-npr-cr-blue ok
