// Package schemas generates the generic editor JSON Schemas (stage/target names are a
// pattern, so teams never regenerate) from the CUE #defs, then post-processes them for
// yaml-language-server. Port of the former platform/render/scripts/schemas.sh.
package schemas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/encoding/jsonschema"
	"github.com/pitoniak32/gitops-cd/platform/cli/internal/cueload"
)

// Defs are the exported schemas, in generation order.
var Defs = []string{"App", "Stage", "Target", "EnvironmentVariables", "Owners", "Team"}

var kebabRe = regexp.MustCompile(`([a-z])([A-Z])`)

// FileName is the kebab-case JSON file for a def (EnvironmentVariables -> environment-variables.json).
func FileName(def string) string {
	return strings.ToLower(kebabRe.ReplaceAllString(def, "${1}-${2}")) + ".json"
}

// Raw returns the unprocessed JSON Schema for #def (what `cue def -e '#def' --out jsonschema` emits).
func Raw(v cue.Value, def string) (any, error) {
	d := v.LookupPath(cue.MakePath(cue.Def("#" + def)))
	if err := d.Err(); err != nil {
		return nil, fmt.Errorf("#%s: %w", def, err)
	}
	expr, err := jsonschema.Generate(d, &jsonschema.GenerateConfig{})
	if err != nil {
		return nil, fmt.Errorf("#%s: %w", def, err)
	}
	ev := v.Context().BuildExpr(expr)
	if err := ev.Err(); err != nil {
		return nil, fmt.Errorf("#%s: %w", def, err)
	}
	b, err := ev.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("#%s: %w", def, err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Process applies the post-processing passes in order.
func Process(s any) any {
	s = dropFalseAnyOf(s)
	s = inlineRefProps(s)
	s = propertyNamesMessages(s)
	s = foldNotPattern(s)
	s = patternMessages(s)
	s = patternErrorMessage(s)
	return s
}

// Generate writes every schema to outDir (default <root>/platform/render/schemas) and prints "wrote <file>".
func Generate(root, outDir string, w io.Writer) error {
	if outDir == "" {
		outDir = filepath.Join(root, "platform", "render", "schemas")
	}
	v, err := cueload.Generic(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, def := range Defs {
		raw, err := Raw(v, def)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(Process(raw)); err != nil {
			return err
		}
		f := filepath.Join(outDir, FileName(def))
		if err := os.WriteFile(f, buf.Bytes(), 0o644); err != nil {
			return err
		}
		shown := f
		if rel, err := filepath.Rel(root, f); err == nil && !strings.HasPrefix(rel, "..") {
			shown = rel
		}
		fmt.Fprintln(w, "wrote "+shown)
	}
	return nil
}

// walk is jq's walk: children first, then f on the rebuilt value.
func walk(v any, f func(any) any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			t[k] = walk(c, f)
		}
	case []any:
		for i, c := range t {
			t[i] = walk(c, f)
		}
	}
	return f(v)
}

func walkObj(v any, f func(map[string]any)) any {
	return walk(v, func(x any) any {
		if m, ok := x.(map[string]any); ok {
			f(m)
		}
		return x
	})
}

// dropFalseAnyOf: error("...") branches export as `false`; drop them from anyOf.
func dropFalseAnyOf(s any) any {
	return walkObj(s, func(m map[string]any) {
		a, ok := m["anyOf"].([]any)
		if !ok {
			return
		}
		out := []any{}
		for _, e := range a {
			if b, isBool := e.(bool); isBool && !b {
				continue
			}
			out = append(out, e)
		}
		m["anyOf"] = out
	})
}

// inlineRefProps: CUE embedding exports {$ref, properties, additionalProperties:false}; inline
// the ref'd $defs properties (local ones win) and drop the $ref.
func inlineRefProps(s any) any {
	var defs map[string]any
	if root, ok := s.(map[string]any); ok {
		defs, _ = root["$defs"].(map[string]any)
	}
	const pre = "#/$defs/"
	return walkObj(s, func(m map[string]any) {
		ref, ok := m["$ref"].(string)
		props, hasProps := m["properties"]
		if !ok || !hasProps || !strings.HasPrefix(ref, pre) {
			return
		}
		merged := map[string]any{}
		if d, ok := defs[strings.TrimPrefix(ref, pre)].(map[string]any); ok {
			if dp, ok := d["properties"].(map[string]any); ok {
				for k, x := range dp {
					merged[k] = x
				}
			}
		}
		if lp, ok := props.(map[string]any); ok {
			for k, x := range lp {
				merged[k] = x
			}
		}
		m["properties"] = merged
		delete(m, "$ref")
	})
}

func sortedKeys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

// propertyNamesMessages: replace closed objects ("Property x is not allowed") by propertyNames
// with a message saying what is allowed.
func propertyNamesMessages(s any) any {
	return walkObj(s, func(m map[string]any) {
		if ap, ok := m["additionalProperties"].(bool); !ok || ap {
			return
		}
		if props, ok := m["properties"].(map[string]any); ok {
			keys := sortedKeys(props)
			enum := make([]any, len(keys))
			for i, k := range keys {
				enum[i] = k
			}
			delete(m, "additionalProperties")
			m["propertyNames"] = map[string]any{
				"enum":         enum,
				"errorMessage": "Unknown key. Allowed: " + strings.Join(keys, ", "),
			}
		} else if pp, ok := m["patternProperties"].(map[string]any); ok {
			p := strings.Join(sortedKeys(pp), " or ")
			delete(m, "additionalProperties")
			m["propertyNames"] = map[string]any{
				"pattern":      p,
				"errorMessage": "Invalid name, must match " + p,
			}
		}
	})
}

// foldNotPattern: yamlls ignores errorMessage on `not`; fold not:{pattern} into one lookahead pattern.
func foldNotPattern(s any) any {
	return walkObj(s, func(m map[string]any) {
		not, ok := m["not"].(map[string]any)
		if !ok || len(not) != 1 {
			return
		}
		np, ok := not["pattern"]
		if !ok {
			return
		}
		p := "^(?![\\s\\S]*?(?:" + str(np) + "))"
		if cur, has := m["pattern"]; has && cur != nil && cur != false {
			p += "[\\s\\S]*?(?:" + str(cur) + ")"
		}
		m["pattern"] = p
		delete(m, "not")
	})
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

var (
	domainTail = regexp.MustCompile(`^.*@`) // greedy: through the last @
)

// patternMessages: readable messages for pattern-heavy fields.
func patternMessages(s any) any {
	return walkObj(s, func(m map[string]any) {
		pat, _ := m["pattern"].(string)
		desc, _ := m["description"].(string)
		_, hasAnyOf := m["anyOf"]
		switch {
		case strings.Contains(pat, "(/[a-z0-9._-]+)+$"):
			m["errorMessage"] = "Image repository only, no tag or digest (Kargo discovers SemVer tags)"
		case strings.HasPrefix(pat, "^[a-z0-9._%+-]+@"):
			d := domainTail.ReplaceAllString(pat, "")
			d = strings.TrimSuffix(d, "$")
			d = strings.ReplaceAll(d, `\`, "")
			m["errorMessage"] = "Lowercase email in the allowed domain: " + d
		case hasAnyOf && strings.HasPrefix(desc, "Plain value, or a Secret"):
			m["errorMessage"] = "Plain string, or secret:<gcp-project>/<secret>[@<version>]"
		}
	})
}

// patternErrorMessage: yamlls reports pattern failures (incl. inside anyOf branches) via patternErrorMessage.
func patternErrorMessage(s any) any {
	return walkObj(s, func(m map[string]any) {
		msg, ok := m["errorMessage"]
		if !ok {
			return
		}
		_, hasPat := m["pattern"]
		a, hasAnyOf := m["anyOf"].([]any)
		if !hasPat && !hasAnyOf {
			return
		}
		m["patternErrorMessage"] = msg
		for _, e := range a {
			if em, ok := e.(map[string]any); ok {
				em["patternErrorMessage"] = msg
			}
		}
	})
}
