package cmd

import (
	"os"
	"testing"
)

// Tests see no GitHub Actions environment (CI runs them inside one); tests that need it set getenv.
func TestMain(m *testing.M) {
	getenv = func(string) string { return "" }
	os.Exit(m.Run())
}
