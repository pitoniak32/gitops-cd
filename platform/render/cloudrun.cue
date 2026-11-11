package render

import (
	"regexp"
	"strings"
	"list"
)

// Cloud Run image apps (ADR 0024): one single-document service.yaml (Knative Service) per (app, target),
// the gha workflow's whole input. Promotion pins the container image in it with yaml-update; the image
// here is the bare repo (Freight pins the digest). Secrets always render: env secret:<p>/<s>[@<v>]
// becomes a Secret Manager secretKeyRef.
cloudRunFiles: {for _, d in deploys if d.kind == "k8s" if d.tgt.platform == "CLOUD_RUN" {
	"apps/\(_cap)/\(d.app)/\(d.stage)/\(d.target)/service.yaml": {
		let labels = {
			"app.kubernetes.io/name":       d.app
			"app.kubernetes.io/managed-by": "cue-platform"
			"example.com/capability":       _cap
			"example.com/stage":            d.stage
			"example.com/target":           d.target
			"example.com/environment":      d.tgt.environment
			"example.com/environment-type": d.tgt.environmentType
			"example.com/billing":          d.tgt.billing
		}
		apiVersion: "serving.knative.dev/v1"
		kind:       "Service"
		metadata: {
			name:      d.app
			namespace: d.tgt.gcpProject
			"labels":  labels
			annotations: "run.googleapis.com/location": d.tgt.region
		}
		spec: template: {
			metadata: {
				"labels": labels
				annotations: "autoscaling.knative.dev/minScale": "\(d.replicas)"
			}
			spec: containers: [{
				name:  d.app
				image: d.image
				ports: [{containerPort: d.port}]
				resources: limits: {cpu: d.cpu, memory: d.memory}
				if len(d.env) > 0 {
					env: [for k in list.SortStrings([for k, _ in d.env {k}]) {
						name: k
						if !strings.HasPrefix(d.env[k], "secret:") {value: d.env[k]}
						if strings.HasPrefix(d.env[k], "secret:") {
							let m = regexp.FindSubmatch(_secretRe, d.env[k])
							valueFrom: secretKeyRef: {
								name: "projects/\(m[1])/secrets/\(m[2])"
								key: [if m[3] != "" {m[3]}, "latest"][0]
							}
						}
					}]
				}
			}]
		}
	}
}}
