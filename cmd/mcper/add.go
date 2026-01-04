package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var (
	addEnvVars []string
)

var addCmd = &cobra.Command{
	Use:   "add <plugin>",
	Short: "Add a plugin to the project",
	Long: `Add a plugin to .mcper/start.sh configuration.

Plugin sources can be:
  linkedin                           Plugin name from registry (uses latest version)
  linkedin@1.2.0                     Plugin name with specific version
  ./custom.wasm                      Local WASM file
  http://localhost:3000/mcp          HTTP MCP server

Examples:
  mcper add linkedin
  mcper add github@2.0.0 --env TOKEN=GITHUB_TOKEN
  mcper add ./local-plugin.wasm`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringArrayVar(&addEnvVars, "env", nil, "Environment variable mapping (PLUGIN_VAR=ENV_VAR)")
}

// resolvePluginSource resolves a simple plugin name to a full GitHub releases URL
// e.g., "linkedin" -> "https://github.com/joshcarp/mcper/releases/download/v0.1.0/plugin-linkedin.wasm"
// e.g., "linkedin@1.2.0" -> "https://github.com/joshcarp/mcper/releases/download/v1.2.0/plugin-linkedin.wasm"
func resolvePluginSource(source string) (string, *PluginInfo, error) {
	// If it's already a URL or local path, return as-is
	if strings.Contains(source, "://") || strings.HasPrefix(source, "./") || strings.HasPrefix(source, "/") {
		return source, nil, nil
	}

	// Parse name and optional version
	name := source
	version := ""
	if idx := strings.Index(source, "@"); idx != -1 {
		name = source[:idx]
		version = source[idx+1:]
	}

	// Fetch plugins manifest
	manifest, err := fetchPluginsManifest()
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch plugins registry: %w", err)
	}

	// Find the plugin
	for _, p := range manifest.Plugins {
		if p.Name == name {
			// Use specified version or default to "latest"
			// (we'll add proper versioning later)
			if version == "" {
				version = "latest"
			}
			resolvedSource := mcper.PluginURL(name, version)
			return resolvedSource, &p, nil
		}
	}

	return "", nil, fmt.Errorf("plugin '%s' not found in registry. Run 'mcper registry list' to see available plugins", name)
}

func runAdd(cmd *cobra.Command, args []string) error {
	source := args[0]

	// Resolve simple plugin names to full URLs
	resolvedSource, pluginInfo, err := resolvePluginSource(source)
	if err != nil {
		return err
	}

	// Parse the plugin source
	parsed, err := mcper.ParsePluginSource(resolvedSource)
	if err != nil {
		return fmt.Errorf("invalid plugin source: %w", err)
	}

	// Find start.sh
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	startPath := filepath.Join(cwd, ".mcper", mcper.StartScriptName)
	if _, err := os.Stat(startPath); os.IsNotExist(err) {
		return fmt.Errorf("no .mcper/start.sh found - run 'mcper init' first")
	}

	// Parse existing config
	config, err := mcper.ParseStartScript(startPath)
	if err != nil {
		return fmt.Errorf("failed to parse start.sh: %w", err)
	}

	// Check if plugin already exists
	if config.HasPlugin(resolvedSource) {
		return fmt.Errorf("plugin %s already exists in configuration", resolvedSource)
	}

	// Parse env mappings from flags
	envMap := make(map[string]string)
	for _, e := range addEnvVars {
		if idx := strings.Index(e, "="); idx != -1 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}

	// If no env flags provided but we have plugin info, auto-add required env vars
	if len(envMap) == 0 && pluginInfo != nil && len(pluginInfo.Env) > 0 {
		for _, envVar := range pluginInfo.Env {
			envMap[envVar] = envVar
		}
	}

	// Create plugin config
	plugin := mcper.PluginConfig{
		Source: resolvedSource,
	}
	if len(envMap) > 0 {
		plugin.Env = envMap
	}

	// Add plugin to config
	config.AddPlugin(plugin)

	// Update start.sh
	if err := mcper.UpdateStartScript(startPath, config); err != nil {
		return fmt.Errorf("failed to update start.sh: %w", err)
	}

	fmt.Printf("Added plugin: %s\n", resolvedSource)

	// Show info about the plugin
	if parsed.Type == mcper.PluginTypeWASM {
		fmt.Printf("  Name: %s\n", parsed.Name)
		if parsed.Version != "" {
			fmt.Printf("  Version: %s\n", parsed.Version)
		}
	}

	if pluginInfo != nil {
		fmt.Printf("  Description: %s\n", pluginInfo.Description)
	}

	if len(envMap) > 0 {
		fmt.Println("  Environment variables:")
		for k, v := range envMap {
			fmt.Printf("    %s -> ${%s}\n", k, v)
		}
		fmt.Println("\n  Make sure to set these in your MCP client config.")
	}

	return nil
}
