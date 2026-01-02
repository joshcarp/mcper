package main

import (
	"fmt"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("mcper v%s\n", mcper.Version)
		// Check for updates after showing version
		CheckForUpdates()
	},
}
