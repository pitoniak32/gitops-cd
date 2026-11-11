package render

// Platform pre-deploy checks (ADR 0028): GitHub Actions workflows Kargo dispatches and waits for before a
// target Stage pushes anything. Platform policy, keyed on the target's environmentType; teams configure
// nothing. Each check is dispatched to the app's sourceRepo (app.yml), which must contain `file` on `ref`
// declaring the inputs capability, app, stage, target, commit and image/module. Runs as ClusterPromotionTask
// gha-check (promotiontasks.cue), one `pre-<n>` step per check (delivery.cue).
#PreDeployCheck: {
	// workflow file name under <sourceRepo>/.github/workflows/
	file!: =~"^[A-Za-z0-9_.-]+\\.ya?ml$" | error("expected a .yml or .yaml file name")
	ref:   *"main" | string & !=""
	// how long the run may take, whole minutes; gha-check's wait gives up at 2h regardless
	timeout: *"30m" | =~"^([1-9]|[1-9][0-9]|1[01][0-9]|120)m$" | error("timeout: whole minutes, 1m..120m")
}

preDeploy: [#EnvironmentType]: [...#PreDeployCheck]
preDeploy: prd: [{file: "precheck.yaml"}]
