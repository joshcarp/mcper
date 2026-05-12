package main

import (
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List plugins configured in this project",
	Long: `List all MCP plugins configured in the current project's .mcper/start.sh.

Examples:
  mcper list          List configured plugins
  mcper list --json   Output as JSON`,
	RunE: runPluginList,
}

func init() {
	listCmd.Flags().BoolVar(&pluginListJSON, "json", false, "Output as JSON")
}
