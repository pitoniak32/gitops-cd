// Package dns wires keycloak.localhost (OIDC issuer) and otlp.localhost (telemetry ingest, ADR 0031) to the hub
// traefik inside each cluster via CoreDNS rewrites.
package dns

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/regions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	hubTarget     = "traefik.traefik.svc.cluster.local"
	regionTarget  = "hub-traefik.kube-system.svc.cluster.local"
	hubCtx        = "kind-kargo-demo"
	rolloutWait   = 120 * time.Second
	restartedAt   = "kubectl.kubernetes.io/restartedAt"
	hubContainer  = "kargo-demo-control-plane"
	hubInspectFmt = `{{(index .NetworkSettings.Networks "kargo-demo").IPAddress}}`
)

// inClusterHosts resolve to the hub traefik in every cluster.
var inClusterHosts = []string{"keycloak.localhost", "otlp.localhost"}

var readyLine = regexp.MustCompile(`(?m)^([ \t]*)ready$`)

// RewriteCorefile inserts "rewrite name <host> <target>" after the ready line (same indentation).
// It returns false, unchanged, when host already has a rewrite or there is no ready line.
func RewriteCorefile(corefile, host, target string) (string, bool) {
	if strings.Contains(corefile, host) {
		return corefile, false
	}
	loc := readyLine.FindStringSubmatchIndex(corefile)
	if loc == nil {
		return corefile, false
	}
	indent := corefile[loc[2]:loc[3]]
	return corefile[:loc[1]] + "\n" + indent + "rewrite name " + host + " " + target + corefile[loc[1]:], true
}

// waitRollout waits for the coredns Deployment rollout (var for tests).
var waitRollout = func(ctx context.Context, c *kube.Clients) error {
	ctx, cancel := context.WithTimeout(ctx, rolloutWait)
	defer cancel()
	for {
		d, err := c.Core.AppsV1().Deployments("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
		if err == nil {
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			if d.Status.ObservedGeneration >= d.Generation && d.Status.UpdatedReplicas == want &&
				d.Status.ReadyReplicas == want && d.Status.Replicas == want {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("coredns rollout in %s: %w", c.Ctx, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// ensureRewrite adds a rewrite per host to the coredns ConfigMap; restarts and waits once, only when it changed.
func ensureRewrite(ctx context.Context, c *kube.Clients, hosts []string, target string, log io.Writer) error {
	cms := c.Core.CoreV1().ConfigMaps("kube-system")
	cm, err := cms.Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get coredns configmap in %s: %w", c.Ctx, err)
	}
	out, added := cm.Data["Corefile"], []string{}
	for _, h := range hosts {
		var changed bool
		if out, changed = RewriteCorefile(out, h, target); changed {
			added = append(added, h)
		}
	}
	if len(added) == 0 {
		fmt.Fprintf(log, "coredns in %s already rewrites %s\n", c.Ctx, strings.Join(hosts, ", "))
		return nil
	}
	cm.Data["Corefile"] = out
	if _, err := cms.Update(ctx, cm, metav1.UpdateOptions{FieldManager: kube.FieldManager}); err != nil {
		return fmt.Errorf("update coredns configmap in %s: %w", c.Ctx, err)
	}
	deps := c.Core.AppsV1().Deployments("kube-system")
	d, err := deps.Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get coredns deployment in %s: %w", c.Ctx, err)
	}
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations[restartedAt] = time.Now().Format(time.RFC3339)
	if _, err := deps.Update(ctx, d, metav1.UpdateOptions{FieldManager: kube.FieldManager}); err != nil {
		return fmt.Errorf("restart coredns in %s: %w", c.Ctx, err)
	}
	fmt.Fprintf(log, "coredns in %s: added rewrite %s -> %s, restarting\n", c.Ctx, strings.Join(added, ", "), target)
	return waitRollout(ctx, c)
}

// Hub makes inClusterHosts resolve to the hub traefik inside the hub cluster.
func Hub(ctx context.Context, c *kube.Clients, log io.Writer) error {
	return ensureRewrite(ctx, c, inClusterHosts, hubTarget, log)
}

// hubIP resolves the hub container's docker-network IP (var for tests).
var hubIP = func(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", hubContainer, "-f", hubInspectFmt).Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect %s: %w", hubContainer, err)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("no hub container IP")
	}
	return ip, nil
}

var forContext = kube.ForContext

func hubService(ip string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Service
metadata: {name: hub-traefik, namespace: kube-system}
spec:
  ports: [{name: web, port: 80, targetPort: 30080}]
---
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: hub-traefik
  namespace: kube-system
  labels: {kubernetes.io/service-name: hub-traefik}
addressType: IPv4
ports: [{name: web, port: 30080, protocol: TCP}]
endpoints: [{addresses: ["%s"]}]
`, ip)
}

// Regions points inClusterHosts at the hub traefik (hub container IP:30080) in every region.
// The IP is re-resolved each run (it changes on docker restart).
func Regions(ctx context.Context, root string, log io.Writer) error {
	ip, err := hubIP(ctx)
	if err != nil {
		return err
	}
	rs, err := regions.List(root)
	if err != nil {
		return err
	}
	for _, r := range rs {
		c, err := forContext("kind-region-" + r)
		if err != nil {
			return err
		}
		if err := c.ApplyYAML(ctx, []byte(hubService(ip)), kube.FieldManager); err != nil {
			return fmt.Errorf("apply hub-traefik in %s: %w", c.Ctx, err)
		}
		if err := ensureRewrite(ctx, c, inClusterHosts, regionTarget, log); err != nil {
			return err
		}
	}
	return nil
}

// HubContext is the kube context of the hub cluster.
const HubContext = hubCtx
