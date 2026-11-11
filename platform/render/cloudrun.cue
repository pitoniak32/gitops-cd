package render

import (
	"regexp"
	"strconv"
	"strings"
	"list"
)

// Cloud Run image apps (ADR 0024): one single-document service.yaml (Knative Service) per (app, target),
// the gha workflow's whole input. Promotion pins the container image in it with yaml-update; the image
// here is the bare repo (Freight pins the digest). Secrets always render: env secret:<p>/<s>[@<v>]
// becomes a Secret Manager secretKeyRef. Cloud Run YAML rules:
//   - label keys: lowercase letters, digits, _ and - only (no / or .), so these differ from the K8S
//     labels; the region is the system label cloud.googleapis.com/location (no location annotation)
//   - a secret in the target's own gcpProject is referenced directly by its name; one from another
//     project needs an alias declared in the run.googleapis.com/secrets template annotation
//     (<alias>:projects/<p>/secrets/<s>, comma-joined, sorted, deduped). Alias = "<p>-<s>"
//     lowercased with _ replaced by -.
//   - under 1 vCPU (cpu "<n>m", n < 1000) Cloud Run requires containerConcurrency 1
cloudRunFiles: {for _, d in deploys if d.kind == "k8s" if d.tgt.platform == "CLOUD_RUN" {
	"apps/\(_cap)/\(d.app)/\(d.stage)/\(d.target)/service.yaml": {
		let labels = {
			"app":              d.app
			"managed-by":       "cue-platform"
			"capability":       _cap
			"stage":            d.stage
			"target":           d.target
			"environment":      d.tgt.environment
			"environment-type": d.tgt.environmentType
			"billing":          d.tgt.billing
		}
		let secretEnv = {for k, v in d.env if strings.HasPrefix(v, "secret:") {
			(k): {
				let m = regexp.FindSubmatch(_secretRe, v)
				project: m[1]
				secret:  m[2]
				key: [if m[3] != "" {m[3]}, "latest"][0]
				alias: strings.Replace(strings.ToLower("\(m[1])-\(m[2])"), "_", "-", -1)
			}
		}}
		let aliases = {for _, e in secretEnv if e.project != d.tgt.gcpProject {(e.alias): "projects/\(e.project)/secrets/\(e.secret)"}}
		let lowCpu = strings.HasSuffix(d.cpu, "m") && strconv.Atoi(strings.TrimSuffix(d.cpu, "m")) < 1000
		apiVersion: "serving.knative.dev/v1"
		kind:       "Service"
		metadata: {
			name:      d.app
			namespace: d.tgt.gcpProject
			"labels": labels & {"cloud.googleapis.com/location": d.tgt.region}
		}
		spec: template: {
			metadata: {
				"labels": labels
				annotations: {
					"autoscaling.knative.dev/minScale": "\(d.replicas)"
					if len(aliases) > 0 {
						"run.googleapis.com/secrets": strings.Join([for a in list.SortStrings([for a, _ in aliases {a}]) {"\(a):\(aliases[a])"}], ",")
					}
				}
			}
			spec: {
				if lowCpu {containerConcurrency: 1}
				containers: [{
					name:  d.app
					image: d.image
					ports: [{containerPort: d.port}]
					resources: limits: {cpu: d.cpu, memory: d.memory}
					if len(d.env) > 0 {
						env: [for k in list.SortStrings([for k, _ in d.env {k}]) {
							name: k
							if secretEnv[k] == _|_ {value: d.env[k]}
							if secretEnv[k] != _|_ {
								let e = secretEnv[k]
								valueFrom: secretKeyRef: {
									name: [if e.project == d.tgt.gcpProject {e.secret}, e.alias][0]
									key: e.key
								}
							}
						}]
					}
				}]
			}
		}
	}
}}
