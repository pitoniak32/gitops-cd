package kargo

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var (
	stageGVR     = schema.GroupVersionResource{Group: "kargo.akuity.io", Version: "v1alpha1", Resource: "stages"}
	freightGVR   = schema.GroupVersionResource{Group: "kargo.akuity.io", Version: "v1alpha1", Resource: "freights"}
	promotionGVR = schema.GroupVersionResource{Group: "kargo.akuity.io", Version: "v1alpha1", Resource: "promotions"}
)

// Promotion wait knobs (vars so tests can shorten them).
var (
	PromotionPoll    = 2 * time.Second
	PromotionTimeout = 600 * time.Second
)

var (
	promoteMsg = regexp.MustCompile(`(?m)^promote [^ ]+ to [^ ]+ from ([0-9a-f]{40})(,.*)?$`)
	pinnedImg  = regexp.MustCompile(`^(.+):([^:@/]+)@(sha256:[0-9a-f]+)$`)
)

// FreightFromBranch rebuilds the Freight a target's env branch was promoted from: the source commit from
// Kargo's commit message ("promote <cap>/<app> to <stage>/<target> from <sha>[, image|module <tag>]") and
// the pinned artifact (<repo>:<tag>@<digest>) from the branch's deployment.yaml (K8s app), service.yaml (Cloud Run app) or terraform.yaml
// `module` (terraform app, ADR 0021). Same artifacts -> Kargo computes the same Freight ID. The error is the
// reason the branch can't be restored.
func FreightFromBranch(warehouse, repoURL, imageRepo, headMessage, pinned string) (*unstructured.Unstructured, error) {
	m := promoteMsg.FindStringSubmatch(headMessage)
	if m == nil {
		first, _, _ := strings.Cut(headMessage, "\n")
		return nil, fmt.Errorf("head is not a Kargo promotion commit: %s", first)
	}
	im := pinnedImg.FindStringSubmatch(pinned)
	if im == nil {
		return nil, fmt.Errorf("no pinned artifact (<repo>:<tag>@<digest>) in deployment.yaml, service.yaml or terraform.yaml (promoted before ADR 0016?)")
	}
	if im[1] != imageRepo {
		return nil, fmt.Errorf("pinned artifact %s is not %s", im[1], imageRepo)
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kargo.akuity.io/v1alpha1", "kind": "Freight",
		"metadata": map[string]any{},
		"origin":   map[string]any{"kind": "Warehouse", "name": warehouse},
		"commits":  []any{map[string]any{"repoURL": repoURL, "id": m[1]}},
		"images":   []any{map[string]any{"repoURL": imageRepo, "tag": im[2], "digest": im[3]}},
	}}, nil
}

// BranchReader reads an env branch head.
type BranchReader interface {
	// Head returns the head commit message and the pinned artifact (branchArtifact; "" if unreadable);
	// exists is false when the branch doesn't exist.
	Head(repoURL, branch string) (msg string, deployImage string, exists bool, err error)
}

// GHReader reads branches through the gh CLI.
type GHReader struct{}

func (GHReader) Head(repoURL, branch string) (string, string, bool, error) {
	slug := strings.TrimPrefix(repoURL, "https://github.com/")
	ref := url.PathEscape(branch)
	var stderr bytes.Buffer
	c := exec.Command("gh", "api", "repos/"+slug+"/commits/"+ref, "--jq", ".commit.message")
	c.Stderr = &stderr
	msg, err := c.Output()
	if err != nil {
		if ghNotFound(stderr.String()) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("gh api commits/%s: %w: %s", branch, err, strings.TrimSpace(stderr.String()))
	}
	img := branchArtifact(func(path string) ([]byte, bool) {
		b, err := exec.Command("gh", "api", "repos/"+slug+"/contents/"+path+"?ref="+ref, "--jq", ".content").Output()
		if err != nil {
			return nil, false
		}
		raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(b)), ""))
		return raw, err == nil
	})
	return strings.TrimRight(string(msg), "\n"), img, true, nil
}

// branchArtifact returns what an env branch pins: the deployment.yaml image (K8s app), else the
// service.yaml image (Cloud Run app, same container image path), else the terraform.yaml module
// (terraform app); "" if none is readable.
func branchArtifact(read func(path string) ([]byte, bool)) string {
	if raw, ok := read("deployment.yaml"); ok {
		return deploymentImage(raw)
	}
	if raw, ok := read("service.yaml"); ok {
		return deploymentImage(raw)
	}
	if raw, ok := read("terraform.yaml"); ok {
		return terraformModule(raw)
	}
	return ""
}

func terraformModule(raw []byte) string {
	var d struct {
		Module string `yaml:"module"`
	}
	if yaml.Unmarshal(raw, &d) != nil {
		return ""
	}
	return d.Module
}

// ghNotFound reports whether gh's stderr says the ref doesn't exist (HTTP 404), as opposed to an
// auth/network/rate-limit failure.
func ghNotFound(stderr string) bool { return strings.Contains(stderr, "HTTP 404") }

func deploymentImage(raw []byte) string {
	var d struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `yaml:"image"`
					} `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if yaml.Unmarshal(raw, &d) != nil || len(d.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return d.Spec.Template.Spec.Containers[0].Image
}

func allCaps(root string) ([]string, error) {
	es, err := os.ReadDir(root + "/capabilities")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

type target struct {
	stage, warehouse, repo, branch, imageRepo string
	hasFreight                                bool
}

func targets(stages []unstructured.Unstructured) []target {
	var out []target
	for _, s := range stages {
		tmpl, found, _ := unstructured.NestedMap(s.Object, "spec", "promotionTemplate")
		if !found || tmpl == nil {
			continue // gate Stages are not restored
		}
		vars := map[string]string{}
		vs, _, _ := unstructured.NestedSlice(s.Object, "spec", "promotionTemplate", "spec", "vars")
		for _, v := range vs {
			m, _ := v.(map[string]any)
			n, _ := m["name"].(string)
			val, _ := m["value"].(string)
			vars[n] = val
		}
		rf, _, _ := unstructured.NestedSlice(s.Object, "spec", "requestedFreight")
		wh := ""
		if len(rf) > 0 {
			if m, ok := rf[0].(map[string]any); ok {
				wh, _, _ = unstructured.NestedString(m, "origin", "name")
			}
		}
		hist, _, _ := unstructured.NestedSlice(s.Object, "status", "freightHistory")
		has := false
		if len(hist) > 0 {
			if m, ok := hist[0].(map[string]any); ok {
				items, _, _ := unstructured.NestedMap(m, "items")
				has = len(items) > 0
			}
		}
		out = append(out, target{s.GetName(), wh, vars["repo"], vars["branch"], vars["image"], has})
	}
	return out
}

func findFreight(items []unstructured.Unstructured, f *unstructured.Unstructured) string {
	key := func(u *unstructured.Unstructured) [3]string {
		o, _, _ := unstructured.NestedString(u.Object, "origin", "name")
		c, _, _ := unstructured.NestedSlice(u.Object, "commits")
		i, _, _ := unstructured.NestedSlice(u.Object, "images")
		var id, dg string
		if len(c) > 0 {
			m, _ := c[0].(map[string]any)
			id, _ = m["id"].(string)
		}
		if len(i) > 0 {
			m, _ := i[0].(map[string]any)
			dg, _ = m["digest"].(string)
		}
		return [3]string{o, id, dg}
	}
	want := key(f)
	for i := range items {
		if key(&items[i]) == want {
			return items[i].GetName()
		}
	}
	return ""
}

// Restore recreates Kargo's record of what runs where after a cluster rebuild, from the env/* branches.
// For each target Stage with no current Freight it recreates the Freight the branch was promoted from,
// approves it for the Stage and promotes it (identical manifests: the workload doesn't change).
// Branches it can't reproduce are skipped with a reason. Gate Stages are not restored.
func Restore(ctx context.Context, root string, hub *kube.Clients, caps []string, branches BranchReader, out io.Writer) error {
	if len(caps) == 0 {
		var err error
		if caps, err = allCaps(root); err != nil {
			return err
		}
	}
	var promos []promo
	for _, cap := range caps {
		// one Kargo project per app (ADR 0029), found by its rendered namespace label
		sel := capabilityLabel + "=" + cap
		nss, err := hub.Core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: sel})
		if err != nil {
			return fmt.Errorf("list namespaces %s: %w", sel, err)
		}
		if len(nss.Items) == 0 {
			fmt.Fprintf(out, "%s: no Kargo project labeled %s (not rendered/synced yet?), skipped\n", cap, sel)
			continue
		}
		sort.Slice(nss.Items, func(i, j int) bool { return nss.Items[i].Name < nss.Items[j].Name })
		for _, ns := range nss.Items {
			if err := restoreProject(ctx, hub, branches, out, cap+"/"+ns.Labels[appLabel], ns.Name, &promos); err != nil {
				return err
			}
		}
	}

	// wait for every restore promotion; any failure fails the task
	failed := 0
	for _, p := range promos {
		phase, msg := waitPromotion(ctx, hub, p.ns, p.name)
		if phase == "" {
			phase = "timed out"
		}
		fmt.Fprintf(out, "%s: %s\n", p.name, phase)
		if phase != "Succeeded" {
			failed++
			if msg != "" {
				fmt.Fprintln(out, msg)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d restore promotion(s) did not succeed", failed)
	}
	return nil
}

func waitPromotion(ctx context.Context, hub *kube.Clients, ns, name string) (phase, msg string) {
	ctx, cancel := context.WithTimeout(ctx, PromotionTimeout)
	defer cancel()
	for {
		u, err := hub.Dyn.Resource(promotionGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			phase, _, _ = unstructured.NestedString(u.Object, "status", "phase")
			msg, _, _ = unstructured.NestedString(u.Object, "status", "message")
			switch phase {
			case "Succeeded", "Failed", "Errored", "Aborted":
				return phase, msg
			}
		}
		select {
		case <-ctx.Done():
			return phase, msg
		case <-time.After(PromotionPoll):
		}
	}
}

type promo struct{ ns, name string }

const (
	capabilityLabel = "example.com/capability"
	appLabel        = "example.com/app"
)

// restoreProject restores the target Stages of one Kargo project (namespace pns); prefix (<cap>/<app>) labels its output.
func restoreProject(ctx context.Context, hub *kube.Clients, branches BranchReader, out io.Writer, prefix, pns string, promos *[]promo) error {
	stages, err := hub.Dyn.Resource(stageGVR).Namespace(pns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list stages in %s: %w", pns, err)
	}
	fr := hub.Dyn.Resource(freightGVR).Namespace(pns)
	for _, t := range targets(stages.Items) {
		label := prefix + "/" + t.stage
		if t.hasFreight {
			fmt.Fprintf(out, "%s: already has Freight, skipped\n", label)
			continue
		}
		if t.warehouse == "" {
			fmt.Fprintf(out, "%s: no requestedFreight origin (unexpected Stage shape), skipped\n", label)
			continue
		}
		msg, img, exists, err := branches.Head(t.repo, t.branch)
		if err != nil {
			return fmt.Errorf("%s: read %s: %w", label, t.branch, err)
		}
		if !exists {
			fmt.Fprintf(out, "%s: %s doesn't exist (never promoted), skipped\n", label, t.branch)
			continue
		}
		f, err := FreightFromBranch(t.warehouse, t.repo, t.imageRepo, msg, img)
		if err != nil {
			fmt.Fprintf(out, "%s: %s, skipped\n", label, err)
			continue
		}
		// existing Freight with the same artifacts (re-run, or another target of the same app) else create it
		existing, err := fr.List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list freight in %s: %w", pns, err)
		}
		name := findFreight(existing.Items, f)
		if name == "" {
			// Kargo's mutating webhook names the Freight from its artifacts, so no name is sent.
			created, err := fr.Create(ctx, f, metav1.CreateOptions{FieldManager: kube.FieldManager})
			if err != nil {
				return fmt.Errorf("%s: create freight: %w", label, err)
			}
			name = created.GetName()
		}
		// available to the target Stage without re-running the gates (same as a break-glass approval)
		patch := fmt.Sprintf(`{"status":{"approvedFor":{%q:{"approvedAt":%q}}}}`, t.stage, time.Now().UTC().Format(time.RFC3339))
		if _, err := fr.Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: kube.FieldManager}, "status"); err != nil {
			return fmt.Errorf("%s: approve freight %s: %w", label, name, err)
		}
		p := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "kargo.akuity.io/v1alpha1", "kind": "Promotion",
			"metadata": map[string]any{"generateName": t.stage + "-restore-", "namespace": pns},
			"spec":     map[string]any{"stage": t.stage, "freight": name},
		}}
		cp, err := hub.Dyn.Resource(promotionGVR).Namespace(pns).Create(ctx, p, metav1.CreateOptions{FieldManager: kube.FieldManager})
		if err != nil {
			return fmt.Errorf("%s: create promotion: %w", label, err)
		}
		*promos = append(*promos, promo{pns, cp.GetName()})
		im := f.Object["images"].([]any)[0].(map[string]any)
		cm := f.Object["commits"].([]any)[0].(map[string]any)
		fmt.Fprintf(out, "%s: promoting %s from %s (Freight %s)\n", label, im["tag"], cm["id"].(string)[:7], name)
	}
	return nil
}
