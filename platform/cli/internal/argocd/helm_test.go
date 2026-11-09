package argocd

import (
	"testing"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
)

func TestNewInstallTimeout(t *testing.T) {
	HelmTimeout = 7 // distinct from default; restored below
	defer func() { HelmTimeout = defaultHelmTimeout }()
	in := newInstall(new(action.Configuration), "argocd", "argocd", "https://r", "1.2.3")
	if in.Timeout != HelmTimeout {
		t.Errorf("Timeout = %v, want %v", in.Timeout, HelmTimeout)
	}
	if in.WaitStrategy != kube.HookOnlyStrategy {
		t.Errorf("WaitStrategy = %v", in.WaitStrategy)
	}
	if in.RepoURL != "https://r" || in.Version != "1.2.3" || !in.CreateNamespace || in.Namespace != "argocd" {
		t.Errorf("install = %+v", in)
	}
}

func TestInstallDecision(t *testing.T) {
	for _, c := range []struct {
		exists bool
		status string
		want   installAction
	}{
		{false, "", actInstall},
		{true, "deployed", actSkip},
		{true, "failed", actReinstall},
		{true, "pending-install", actReinstall},
		{true, "uninstalling", actReinstall},
	} {
		if got := decideInstall(c.exists, c.status); got != c.want {
			t.Errorf("decideInstall(%v,%q) = %v, want %v", c.exists, c.status, got, c.want)
		}
	}
}
