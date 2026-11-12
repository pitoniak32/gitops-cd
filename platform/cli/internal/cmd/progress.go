package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// stderrFile is the command's stderr when it is a file (a terminal or not), else nil: tests and other
// writers get no progress.
func stderrFile(c *cobra.Command) *os.File {
	f, _ := c.ErrOrStderr().(*os.File)
	return f
}
