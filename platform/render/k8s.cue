package render

import (
	"regexp"
	"strings"

	apps "cue.dev/x/k8s.io/api/apps/v1"
	core "cue.dev/x/k8s.io/api/core/v1"
)

_secretRe: "^secret:([^/]+)/([^@]+)(?:@(.+))?$"

// k8s objects per (app, K8S target); CLOUD_RUN image apps render service.yaml (cloudrun.cue)
manifests: {for key, d in deploys if d.kind == "k8s" if d.tgt.platform == "K8S" {
	(key): {
		app:    d.app
		stage:  d.stage
		target: d.target

		let appName = d.app
		let podSelector = {"app.kubernetes.io/name": appName}
		let podLabels = podSelector & {
			"app.kubernetes.io/managed-by": "cue-platform"
			"example.com/capability":       _cap
			"example.com/stage":            d.stage
			"example.com/target":           d.target
			"example.com/environment":      d.tgt.environment
			"example.com/environment-type": d.tgt.environmentType
			"example.com/billing":          d.tgt.billing
		}
		let meta = {name: appName, namespace: d.tgt.namespace, labels: podLabels}
		let secretStore = clusters[d.tgt.cluster].secretStore

		// without a secret store, secret-backed env vars are omitted (values still validated)
		let envVars = {for k, v in d.env if secretStore || !strings.HasPrefix(v, "secret:") {(k): v}}
		let secrets = {for k, v in envVars if strings.HasPrefix(v, "secret:") {
			(k): regexp.FindSubmatch(_secretRe, v)
		}}

		objects: [
			core.#ServiceAccount & {
				apiVersion: "v1"
				kind:       "ServiceAccount"
				metadata: meta & {annotations: "iam.gke.io/gcp-service-account": d.tgt.auth.wifEmail}
			},
			apps.#Deployment & {
				apiVersion: "apps/v1"
				kind:       "Deployment"
				metadata:   meta
				spec: {
					replicas: d.replicas
					selector: matchLabels: podSelector
					template: {
						metadata: labels: podLabels
						spec: {
							serviceAccountName: appName
							containers: [{
								name:  appName
								image: d.image
								if d.port != _|_ {
									ports: [{name: "http", containerPort: d.port}]
								}
								if len(envVars) > 0 {
									env: [for k, v in envVars {
										name: k
										if secrets[k] == _|_ {value: v}
										if secrets[k] != _|_ {valueFrom: secretKeyRef: {name: "\(appName)-secrets", key: k}}
									}]
								}
								resources: {
									requests: {cpu: d.cpu, memory: d.memory}
									limits: memory: d.memory
								}
							}]
						}
					}
				}
			},
			if d.port != _|_ {
				core.#Service & {
					apiVersion: "v1"
					kind:       "Service"
					metadata:   meta
					spec: {
						selector: podSelector
						ports: [{name: "http", port: 80, targetPort: "http"}]
					}
				}
			},
			if len(secrets) > 0 {
				// external-secrets.io; one ClusterSecretStore per GCP project: gcp-<project>
				apiVersion: "external-secrets.io/v1"
				kind:       "ExternalSecret"
				metadata: {name: "\(appName)-secrets", namespace: d.tgt.namespace, labels: podLabels}
				spec: {
					refreshInterval: "1h"
					target: name: "\(appName)-secrets"
					data: [for k, m in secrets {
						secretKey: k
						remoteRef: {
							key: m[2]
							version: [if m[3] != "" {m[3]}, "latest"][0]
						}
						sourceRef: storeRef: {kind: "ClusterSecretStore", name: "gcp-\(m[1])"}
					}]
				}
			},
		]
	}
}}
