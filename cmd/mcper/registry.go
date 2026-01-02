package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

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

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Interact with the mcper plugin registry",
	Long: `Interact with the mcper plugin registry.

Commands:
  registry list    List all available plugins in the registry`,
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available plugins in the registry",
	Long: `List all MCP plugins available from the mcper registry.

Examples:
  mcper registry list              List all available plugins
  mcper registry list --json       Output as JSON`,
	RunE: runRegistryList,
}

var registryListJSON bool

func init() {
	registryListCmd.Flags().BoolVar(&registryListJSON, "json", false, "Output as JSON")
	registryCmd.AddCommand(registryListCmd)
}

func fetchPluginsManifest() (*PluginsManifest, error) {
	pluginsURL := mcper.GCSBaseURL + "/plugins.json"

	resp, err := http.Get(pluginsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugins: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch plugins: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var manifest PluginsManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse plugins manifest: %w", err)
	}

	return &manifest, nil
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	manifest, err := fetchPluginsManifest()
	if err != nil {
		return err
	}

	if registryListJSON {
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
	fmt.Fprintln(w, "NAME\tDESCRIPTION\tENV VARS")
	fmt.Fprintln(w, "----\t-----------\t--------")

	for _, p := range manifest.Plugins {
		desc := p.Description
		if len(desc) > 45 {
			desc = desc[:42] + "..."
		}

		envStr := "-"
		if len(p.Env) > 0 {
			envStr = fmt.Sprintf("%d required", len(p.Env))
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, desc, envStr)
	}

	w.Flush()

	fmt.Printf("\nTotal: %d plugins available\n", len(manifest.Plugins))
	fmt.Println("\nTo add a plugin: mcper add <name>")

	return nil
}
