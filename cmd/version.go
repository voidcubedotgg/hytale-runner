package cmd

import (
	"github.com/spf13/cobra"
)

// version is overridable at build time via -ldflags "-X .../cmd.version=...".
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println(version)
	},
}
