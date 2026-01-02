package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

const pluginsURL = "https://raw.githubusercontent.com/joshcarp/mcper/main/scripts/release/plugins.json"

// PluginInfo represents a plugin in the registry
type PluginInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author,omitempty"`
	Source      string   `json:"source"`
	Env         []string `json:"env,omitempty"`
}

// PluginsManifest is the registry manifest
type PluginsManifest struct {
	Plugins []PluginInfo `json:"plugins"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available MCP plugins",
	Long: `List all MCP plugins available from the mcper registry.

Examples:
  mcper list              List all available plugins
  mcper list --json       Output as JSON`,
	RunE: runList,
}

var listJSON bool

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
}

func runList(cmd *cobra.Command, args []string) error {
	// Fetch plugins manifest
	resp, err := http.Get(pluginsURL)
	if err != nil {
		return fmt.Errorf("failed to fetch plugins: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch plugins: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var manifest PluginsManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("failed to parse plugins manifest: %w", err)
	}

	if listJSON {
		// Output as JSON
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(manifest.Plugins)
	}

	// Output as table
	if len(manifest.Plugins) == 0 {
		fmt.Println("No plugins available.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
	fmt.Fprintln(w, "----\t-------\t-----------")

	for _, p := range manifest.Plugins {
		desc := p.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Version, desc)
	}

	w.Flush()

	fmt.Printf("\nTotal: %d plugins\n", len(manifest.Plugins))
	fmt.Println("\nTo add a plugin: mcper add <name>[@<version>]")

	// Check for mcper updates
	CheckForUpdates()

	return nil
}
