package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins in the current project",
	Long: `Manage MCP plugins configured in the current project.

Commands:
  plugin list      List plugins configured in this project
  plugin update    Update all plugins to latest versions`,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List plugins configured in this project",
	Long: `List all MCP plugins configured in the current project's .mcper/start.sh.

Examples:
  mcper plugin list          List configured plugins
  mcper plugin list --json   Output as JSON`,
	RunE: runPluginList,
}

var pluginUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update all plugins to latest versions",
	Long: `Update all configured plugins to use the latest versions from the registry.

This updates the plugin URLs in .mcper/start.sh to point to the latest releases.

Examples:
  mcper plugin update        Update all plugins`,
	RunE: runPluginUpdate,
}

var pluginListJSON bool

func init() {
	pluginListCmd.Flags().BoolVar(&pluginListJSON, "json", false, "Output as JSON")
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginUpdateCmd)
}

func runPluginList(cmd *cobra.Command, args []string) error {
	// Find .mcper directory
	mcperDir := ".mcper"
	startScript := filepath.Join(mcperDir, "start.sh")

	if _, err := os.Stat(startScript); os.IsNotExist(err) {
		return fmt.Errorf("no .mcper/start.sh found. Run 'mcper init' first")
	}

	// Parse the start script to get config
	config, err := mcper.ParseStartScript(startScript)
	if err != nil {
		return fmt.Errorf("failed to parse start script: %w", err)
	}

	if len(config.Plugins) == 0 {
		fmt.Println("No plugins configured.")
		fmt.Println("\nTo add a plugin: mcper add <name>")
		return nil
	}

	if pluginListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(config.Plugins)
	}

	// Output as table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSOURCE\tENV VARS")
	fmt.Fprintln(w, "----\t------\t--------")

	for _, p := range config.Plugins {
		// Parse plugin info
		parsed, _ := mcper.ParsePluginSource(p.Source)
		name := "unknown"
		if parsed != nil && parsed.Name != "" {
			name = parsed.Name
		}

		// Count env vars
		envCount := len(p.Env)
		envStr := fmt.Sprintf("%d configured", envCount)

		// Truncate source URL for display
		source := p.Source
		if len(source) > 50 {
			source = "..." + source[len(source)-47:]
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", name, source, envStr)
	}

	w.Flush()
	fmt.Printf("\nTotal: %d plugin(s)\n", len(config.Plugins))

	return nil
}

func runPluginUpdate(cmd *cobra.Command, args []string) error {
	// Find .mcper directory
	mcperDir := ".mcper"
	startScript := filepath.Join(mcperDir, "start.sh")

	if _, err := os.Stat(startScript); os.IsNotExist(err) {
		return fmt.Errorf("no .mcper/start.sh found. Run 'mcper init' first")
	}

	// Parse the start script to get config
	config, err := mcper.ParseStartScript(startScript)
	if err != nil {
		return fmt.Errorf("failed to parse start script: %w", err)
	}

	if len(config.Plugins) == 0 {
		fmt.Println("No plugins configured.")
		return nil
	}

	// Fetch registry to get latest URLs
	manifest, err := fetchPluginsManifest()
	if err != nil {
		return fmt.Errorf("failed to fetch plugin registry: %w", err)
	}

	// Build lookup map
	registryMap := make(map[string]PluginInfo)
	for _, p := range manifest.Plugins {
		registryMap[p.Name] = p
	}

	// Update each plugin
	updated := 0
	for i, p := range config.Plugins {
		parsed, err := mcper.ParsePluginSource(p.Source)
		if err != nil || parsed == nil {
			continue
		}

		// Look up in registry
		if regPlugin, ok := registryMap[parsed.Name]; ok {
			// Construct the URL using the registry's version (not the "latest" tag)
			newSource := mcper.PluginURL(parsed.Name, regPlugin.Version)
			if p.Source != newSource {
				fmt.Printf("Updating %s...\n", parsed.Name)
				fmt.Printf("  Old: %s\n", p.Source)
				fmt.Printf("  New: %s\n", newSource)
				config.Plugins[i].Source = newSource
				updated++
			} else {
				fmt.Printf("%s is up to date\n", parsed.Name)
			}
		}
	}

	if updated > 0 {
		// Save updated config
		if err := mcper.UpdateStartScript(startScript, config); err != nil {
			return fmt.Errorf("failed to update start script: %w", err)
		}
		fmt.Printf("\nUpdated %d plugin(s)\n", updated)
	} else {
		fmt.Println("\nAll plugins are up to date")
	}

	return nil
}
