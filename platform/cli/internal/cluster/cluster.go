// Package cluster creates and deletes the kind clusters (hub kargo-demo + region-<r>) on the kargo-demo
// docker network.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/logx"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	kind "sigs.k8s.io/kind/pkg/cluster"
	kindlog "sigs.k8s.io/kind/pkg/log"
)

const (
	// Hub is the hub kind cluster name (context kind-kargo-demo).
	Hub = "kargo-demo"
	// Network is the v4-only docker network shared by all clusters. The default `kind` network is
	// dual-stack, so docker desktop forwards ::1 to container v6 where the v4-only cluster has no NodePort
	// (broken https://kargo.localhost).
	Network = "kargo-demo"
	// ReadyTimeout bounds node readiness per cluster.
	ReadyTimeout = 120 * time.Second
)

// Up deletes existing clusters, creates the network, the hub, then the regions in parallel (same network,
// no host ports), waits for nodes Ready and sets current context kind-kargo-demo.
func Up(ctx context.Context, root string, regions []string, log io.Writer) error {
	if err := Down(ctx, root, regions, log); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "docker", "network", "create", "--ipv4", "--ipv6=false", Network).CombinedOutput(); err != nil {
		return fmt.Errorf("docker network create %s: %w: %s", Network, err, out)
	}
	if err := os.Setenv("KIND_EXPERIMENTAL_DOCKER_NETWORK", Network); err != nil {
		return fmt.Errorf("set KIND_EXPERIMENTAL_DOCKER_NETWORK: %w", err)
	}
	if err := create(ctx, Hub, kind.CreateWithConfigFile(filepath.Join(root, "platform/cluster/kind.yaml")), logx.Prefix(log, "[hub] ")); err != nil {
		return err
	}
	tmpl, err := os.ReadFile(filepath.Join(root, "platform/cluster/region.yaml"))
	if err != nil {
		return fmt.Errorf("read region.yaml: %w", err)
	}
	g, gctx := errgroup.WithContext(ctx)
	for _, r := range regions {
		name := "region-" + r
		raw, err := regionConfig(tmpl, name)
		if err != nil {
			return err
		}
		// kind merges the kubeconfig under a lock, so parallel creates are safe
		g.Go(func() error {
			return create(gctx, name, kind.CreateWithRawConfig(raw), logx.Prefix(log, "["+name+"] "))
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return useContext("kind-" + Hub)
}

// Down deletes the hub and region clusters, the docker network (ignored if missing) and $KUBECONFIG.
func Down(ctx context.Context, _ string, regions []string, log io.Writer) error {
	p := provider(log)
	for _, name := range append([]string{Hub}, regionNames(regions)...) {
		fmt.Fprintf(log, "Deleting cluster %q ...\n", name)
		if err := p.Delete(name, os.Getenv("KUBECONFIG")); err != nil {
			return fmt.Errorf("delete cluster %s: %w", name, err)
		}
	}
	_ = exec.CommandContext(ctx, "docker", "network", "rm", Network).Run() // missing network is fine
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		if err := os.Remove(kc); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", kc, err)
		}
	}
	return nil
}

func regionNames(regions []string) []string {
	names := make([]string, len(regions))
	for i, r := range regions {
		names[i] = "region-" + r
	}
	return names
}

var nameLine = regexp.MustCompile(`(?m)^name:.*$`)

// regionConfig sets the top-level `name` of the region.yaml template.
func regionConfig(tmpl []byte, name string) ([]byte, error) {
	if !nameLine.Match(tmpl) {
		return nil, fmt.Errorf("region.yaml: no top-level name")
	}
	return nameLine.ReplaceAll(tmpl, []byte("name: "+name)), nil
}

func provider(log io.Writer) *kind.Provider {
	return kind.NewProvider(kind.ProviderWithDocker(), kind.ProviderWithLogger(logger{log}))
}

func create(ctx context.Context, name string, cfg kind.CreateOption, log io.Writer) error {
	err := provider(log).Create(name, cfg,
		kind.CreateWithKubeconfigPath(os.Getenv("KUBECONFIG")),
		kind.CreateWithWaitForReady(ReadyTimeout),
		kind.CreateWithDisplayUsage(false),
		kind.CreateWithDisplaySalutation(false))
	if err != nil {
		return fmt.Errorf("create cluster %s: %w", name, err)
	}
	return waitNodesReady(ctx, "kind-"+name, log)
}

// waitNodesReady polls until every node of the context is Ready (ReadyTimeout).
func waitNodesReady(ctx context.Context, kubeCtx string, log io.Writer) error {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kubeCtx}).ClientConfig()
	if err != nil {
		return fmt.Errorf("kube config %s: %w", kubeCtx, err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kube client %s: %w", kubeCtx, err)
	}
	ctx, cancel := context.WithTimeout(ctx, ReadyTimeout)
	defer cancel()
	for {
		nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err == nil && len(nodes.Items) > 0 && allReady(nodes.Items) {
			fmt.Fprintf(log, "nodes Ready in %s\n", kubeCtx)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait nodes Ready in %s: %w", kubeCtx, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func allReady(nodes []corev1.Node) bool {
	for _, n := range nodes {
		ready := false
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			return false
		}
	}
	return true
}

// useContext sets current-context in $KUBECONFIG.
func useContext(name string) error {
	po := clientcmd.NewDefaultPathOptions()
	cfg, err := po.GetStartingConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}
	if _, ok := cfg.Contexts[name]; !ok {
		return fmt.Errorf("kubeconfig: no context %s", name)
	}
	cfg.CurrentContext = name
	if err := clientcmd.ModifyConfig(po, *cfg, true); err != nil {
		return fmt.Errorf("set current-context %s: %w", name, err)
	}
	return nil
}

// logger adapts an io.Writer to kind's logger: warnings, errors and V(0) status lines.
type logger struct{ w io.Writer }

func (l logger) Warn(m string)                        { fmt.Fprintln(l.w, "WARN:", m) }
func (l logger) Warnf(f string, a ...any)             { l.Warn(fmt.Sprintf(f, a...)) }
func (l logger) Error(m string)                       { fmt.Fprintln(l.w, "ERROR:", m) }
func (l logger) Errorf(f string, a ...any)            { l.Error(fmt.Sprintf(f, a...)) }
func (l logger) V(v kindlog.Level) kindlog.InfoLogger { return info{l.w, v <= 0} }

type info struct {
	w  io.Writer
	on bool
}

func (i info) Info(m string) {
	if i.on {
		fmt.Fprintln(i.w, m)
	}
}
func (i info) Infof(f string, a ...any) { i.Info(fmt.Sprintf(f, a...)) }
func (i info) Enabled() bool            { return i.on }
