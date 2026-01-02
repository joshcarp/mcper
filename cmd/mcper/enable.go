package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

// MCPConfig represents the .mcp.json file structure
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPServerConfig represents a single MCP server configuration
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable mcper in an MCP client",
	Long: `Enable mcper as an MCP server for a specific client.

Supported clients:
  --claude    Add to .mcp.json for Claude Code

Examples:
  mcper enable --claude              # Add mcper to Claude Code
  mcper enable --claude --name tools # Use custom name`,
	RunE: runEnable,
}

var (
	enableClaude bool
	enableName   string
)

func init() {
	enableCmd.Flags().BoolVar(&enableClaude, "claude", false, "Enable for Claude Code (.mcp.json)")
	enableCmd.Flags().StringVar(&enableName, "name", "", "Name for the MCP server (defaults to directory name)")
}

func runEnable(cmd *cobra.Command, args []string) error {
	if !enableClaude {
		return fmt.Errorf("please specify a client: --claude")
	}

	return enableForClaude()
}

func enableForClaude() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if start.sh exists, if not run init
	startPath := filepath.Join(cwd, ".mcper", mcper.StartScriptName)
	if _, err := os.Stat(startPath); os.IsNotExist(err) {
		fmt.Println("No .mcper/start.sh found, initializing...")
		if err := runInit(nil, nil); err != nil {
			return err
		}
	}

	// Determine the server name
	serverName := enableName
	if serverName == "" {
		serverName = filepath.Base(cwd)
	}

	// Read or create .mcp.json
	mcpPath := filepath.Join(cwd, ".mcp.json")
	mcpConfig := MCPConfig{
		MCPServers: make(map[string]MCPServerConfig),
	}

	if data, err := os.ReadFile(mcpPath); err == nil {
		if err := json.Unmarshal(data, &mcpConfig); err != nil {
			return fmt.Errorf("failed to parse .mcp.json: %w", err)
		}
		// Ensure map is initialized even if JSON had null/empty
		if mcpConfig.MCPServers == nil {
			mcpConfig.MCPServers = make(map[string]MCPServerConfig)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .mcp.json: %w", err)
	}

	// Check if already installed
	if _, exists := mcpConfig.MCPServers[serverName]; exists {
		fmt.Printf("mcper is already configured as '%s' in .mcp.json\n", serverName)
		return nil
	}

	// Get required env vars from start.sh config
	config, err := mcper.ParseStartScript(startPath)
	if err != nil {
		return fmt.Errorf("failed to parse start.sh: %w", err)
	}

	// Collect all unique env vars from plugins
	envVars := make(map[string]string)
	for _, plugin := range config.Plugins {
		for _, envVar := range plugin.Env {
			envVars[envVar] = ""
		}
	}

	// Add mcper to config
	mcpConfig.MCPServers[serverName] = MCPServerConfig{
		Command: "./.mcper/start.sh",
		Env:     envVars,
	}

	// Write .mcp.json
	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize .mcp.json: %w", err)
	}

	if err := os.WriteFile(mcpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .mcp.json: %w", err)
	}

	fmt.Printf("Added mcper to .mcp.json as '%s'\n", serverName)

	if len(envVars) > 0 {
		fmt.Println("\nRequired environment variables (add values to .mcp.json):")
		for k := range envVars {
			fmt.Printf("  %s\n", k)
		}
	}

	fmt.Println("\nRestart Claude Code to activate the MCP server.")

	return nil
}
