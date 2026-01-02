package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize mcper in the current project",
	Long: `Creates .mcper/start.sh in the current directory.

The start.sh script contains:
- Embedded JSON configuration with markers for programmatic editing
- Auto-installation of mcper if not present
- MCP server startup

After running init, add the following to your MCP client config:

{
  "mcpServers": {
    "my-project": {
      "command": "./.mcper/start.sh",
      "env": {
        "API_KEY": "your-api-key"
      }
    }
  }
}`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Create .mcper directory
	mcperDir := filepath.Join(cwd, ".mcper")
	if err := os.MkdirAll(mcperDir, 0755); err != nil {
		return fmt.Errorf("failed to create .mcper directory: %w", err)
	}

	// Check if start.sh already exists
	startPath := filepath.Join(mcperDir, mcper.StartScriptName)
	if _, err := os.Stat(startPath); err == nil {
		return fmt.Errorf("start.sh already exists at %s", startPath)
	}

	// Create default config
	config := mcper.DefaultConfig()

	// Write start.sh
	if err := mcper.WriteStartScript(startPath, config); err != nil {
		return fmt.Errorf("failed to write start.sh: %w", err)
	}

	fmt.Printf("Created %s\n", startPath)

	// Auto-detect Claude Code and enable
	mcpJsonPath := filepath.Join(cwd, ".mcp.json")
	if _, err := os.Stat(mcpJsonPath); err == nil {
		fmt.Println("Detected .mcp.json, enabling for Claude Code...")
		if err := enableForClaude(); err != nil {
			fmt.Printf("Warning: failed to enable for Claude: %v\n", err)
		}
	} else {
		fmt.Println("\nNext steps:")
		fmt.Println("  1. Add plugins: mcper add <plugin>")
		fmt.Println("  2. Enable for Claude: mcper enable --claude")
		fmt.Println("  3. Commit .mcper/start.sh to your repository")
	}

	return nil
}
