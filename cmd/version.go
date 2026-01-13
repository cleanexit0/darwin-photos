package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version information set via ldflags at build time
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("darwin-photos %s\n", Version)
		fmt.Printf("commit: %s\n", CommitSHA)
		fmt.Printf("built: %s\n", BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
