package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "mcper",
	Short: "MCP tool aggregation with WASM plugins",
	Long: `MCPer: One login, all your AI tools, everywhere.

A zero-config MCP (Model Context Protocol) tool that enables:
- WASM-sandboxed plugin execution
- Plugin aggregation from multiple sources
- Self-bootstrapping project setup

Usage:
  mcper init                    Initialize .mcper/start.sh in current project
  mcper add <plugin>            Add a plugin to the project
  mcper serve --config-json ... Run MCP server with the given config
  mcper cache list              List cached plugins
  mcper version                 Show version information`,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(updateCmd)
}
