# Sourced by mise-tasks/infra and kargo-shards (host-side bootstrap helper, not a mise task).
# wait_app <kube-context> <app>: wait until the Application exists (ApplicationSets create them
# asynchronously), force a refresh, then wait for Synced + Healthy.
wait_app() {
  local ctx=$1 app=$2 i
  for i in $(seq 1 300); do
    kubectl --context "$ctx" -n argocd get "app/$app" >/dev/null 2>&1 && break
    [ "$i" = 300 ] && { echo "app/$app never created in $ctx (check its ApplicationSet status)" >&2; return 1; }
    sleep 2
  done
  # force compare against the current spec, else wait sees a stale Synced status
  kubectl --context "$ctx" -n argocd annotate "app/$app" argocd.argoproj.io/refresh=normal --overwrite >/dev/null
  for i in $(seq 1 60); do
    [ -z "$(kubectl --context "$ctx" -n argocd get "app/$app" -o jsonpath='{.metadata.annotations.argocd\.argoproj\.io/refresh}')" ] && break
    sleep 2
  done
  kubectl --context "$ctx" -n argocd wait "app/$app" --for=jsonpath='{.status.sync.status}'=Synced --timeout=600s
  kubectl --context "$ctx" -n argocd wait "app/$app" --for=jsonpath='{.status.health.status}'=Healthy --timeout=600s
}
