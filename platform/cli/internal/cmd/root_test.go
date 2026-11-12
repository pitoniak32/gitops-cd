package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	c := NewRoot()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetArgs([]string{"--help"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gcd") {
		t.Fatalf("help lacks gcd: %q", out.String())
	}
}

// --root is global: every command that reads a config root takes it, before or after the subcommand.
func TestGlobalRoot(t *testing.T) {
	root, _ := filepath.Abs(fixtureRoot)
	for _, args := range [][]string{
		{"--root", root, "show", "payments"},
		{"show", "payments", "--root", root},
		{"--root", root, "lint", "payments"},
		{"--root", root, "schema", "-s", "owners"},
	} {
		c := NewRoot()
		var out, errOut bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&errOut)
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			t.Errorf("%v: %v\n%s", args, err, errOut.String())
		}
	}
	c := NewRoot()
	for _, sub := range c.Commands() {
		if sub.LocalNonPersistentFlags().Lookup("root") != nil {
			t.Errorf("%s defines its own --root", sub.Name())
		}
	}
}
