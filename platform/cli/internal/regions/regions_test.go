package regions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
)

var files = []string{regionsFile, realmFile, kargoValues}

func TestWriteMatchesCommitted(t *testing.T) {
	real, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(real, f))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(tmp, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, stale(t, f, b), 0o644)
	}
	cfg, err := Load(real)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ { // second pass = idempotent
		if err := Write(tmp, cfg); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			want, _ := os.ReadFile(filepath.Join(real, f))
			got, _ := os.ReadFile(filepath.Join(tmp, f))
			if string(want) != string(got) {
				t.Errorf("pass %d: %s differs:\n%s", pass, f, got)
			}
		}
	}
}

func TestList(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "platform/cluster"), 0o755)
	os.WriteFile(filepath.Join(tmp, regionsFile), []byte("# c\nonprem\n\nus\n"), 0o644)
	got, err := List(tmp)
	if err != nil || len(got) != 2 || got[0] != "onprem" || got[1] != "us" {
		t.Fatalf("%v %v", got, err)
	}
}

// stale returns a drifted version of a committed generated file.
func stale(t *testing.T, f string, b []byte) []byte {
	t.Helper()
	switch f {
	case regionsFile:
		return []byte("# old\neu\n")
	case realmFile:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		var cl []json.RawMessage
		json.Unmarshal(m["clients"], &cl)
		var keep []json.RawMessage
		for _, c := range cl {
			if !strings.Contains(string(c), `"clientId":"argocd-`) && !strings.Contains(string(c), `"clientId": "argocd-`) {
				keep = append(keep, c)
			}
		}
		keep = append(keep, json.RawMessage(`{"clientId":"argocd-zzz"}`))
		m["clients"], _ = json.Marshal(keep)
		// emit in original key order
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.Token()
		out := "{"
		for dec.More() {
			k, _ := dec.Token()
			var skip json.RawMessage
			dec.Decode(&skip)
			kb, _ := json.Marshal(k)
			out += string(kb) + ":" + string(m[k.(string)]) + ","
		}
		return []byte(strings.TrimSuffix(out, ",") + "}")
	default:
		re := regexp.MustCompile(`(?m)(^    urls:\n)(?:      .*\n)+`)
		out := re.ReplaceAll(b, []byte("${1}      old: https://x\n"))
		if string(out) == string(b) {
			t.Fatal("stale kargo values not applied")
		}
		return out
	}
}

func TestWriteSynthetic(t *testing.T) {
	real, _ := repo.Root()
	tmp := t.TempDir()
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(real, f))
		p := filepath.Join(tmp, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, stale(t, f, b), 0o644)
	}
	cfg := Config{
		Regions:         []string{"a", "b"},
		KeycloakClients: []json.RawMessage{json.RawMessage(`{"clientId":"argocd-a","z":1,"b":2}`)},
		KargoArgocdURLs: map[string]string{"b": "https://b", "a": "https://a"},
	}
	if err := Write(tmp, cfg); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(tmp, regionsFile))
	if !strings.HasSuffix(string(got), "\na\nb\n") || strings.Contains(string(got), "eu") {
		t.Fatalf("regions: %q", got)
	}
	realm, _ := os.ReadFile(filepath.Join(tmp, realmFile))
	var m struct{ Clients []struct{ ClientID string } }
	json.Unmarshal(realm, &m)
	var ids []string
	for _, c := range m.Clients {
		ids = append(ids, c.ClientID)
	}
	if strings.Join(ids, ",") != "kargo,argocd,argocd-a" {
		t.Fatalf("clients order: %v", ids)
	}
	if !strings.Contains(string(realm), "\"z\": 1,\n      \"b\": 2") {
		t.Fatalf("generated key order lost")
	}
	vals, _ := os.ReadFile(filepath.Join(tmp, kargoValues))
	if !strings.Contains(string(vals), "      a: https://a\n      b: https://b\n") || strings.Contains(string(vals), "old") {
		t.Fatalf("values: %s", vals)
	}
}

func TestWriteKargoValuesEmptyDocument(t *testing.T) {
	p := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeKargoValues(p, Config{}); err == nil {
		t.Fatal("want error for empty values file")
	}
}
