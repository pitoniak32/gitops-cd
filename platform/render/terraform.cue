package render

import "list"

// Terraform apps (ADR 0021): one terraform.yaml per (app, target), the workflow's whole input.
// Promotion pins `module` to <repo>:<tag>@<digest> on the target branch; env is exported as-is.
terraformFiles: {for _, d in deploys if d.kind == "terraform" if d.tgt.platform == "TERRAFORM" {
	"apps/\(_cap)/\(d.app)/\(d.stage)/\(d.target)/terraform.yaml": {
		capability:  _cap
		app:         d.app
		stage:       d.stage
		target:      d.target
		environment: d.tgt.environment
		gcpProject:  d.tgt.terraform.gcpProject
		wifEmail:    d.tgt.auth.wifEmail
		backend:     d.backend
		module:      d.module
		env: {for k in list.SortStrings([for k, _ in d.env {k}]) {(k): d.env[k]}}
	}
}}
