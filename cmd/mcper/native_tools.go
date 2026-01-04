package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/modelcontextprotocol/go-sdk/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerNativeTools adds mcper's own tools to the MCP server
func registerNativeTools(server *mcp.Server) {
	// mcper/native/registry_list - List all available plugins in the registry
	server.AddTool(&mcp.Tool{
		Name:        "mcper/native/registry_list",
		Description: "List all available plugins in the mcper registry. Returns plugin names, descriptions, versions, and required environment variables.",
		InputSchema: &jsonschema.Schema{
			Type:       "object",
			Properties: map[string]*jsonschema.Schema{},
		},
	}, handleRegistryList)

	// mcper/native/registry_search - Search for plugins
	server.AddTool(&mcp.Tool{
		Name:        "mcper/native/registry_search",
		Description: "Search for plugins in the mcper registry by name or description.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"query": {
					Type:        "string",
					Description: "Search query to match against plugin names and descriptions",
				},
			},
			Required: []string{"query"},
		},
	}, handleRegistrySearch)

	// mcper/native/plugin_list - List configured plugins in current project
	server.AddTool(&mcp.Tool{
		Name:        "mcper/native/plugin_list",
		Description: "List all plugins configured in the current project's .mcper/start.sh file.",
		InputSchema: &jsonschema.Schema{
			Type:       "object",
			Properties: map[string]*jsonschema.Schema{},
		},
	}, handlePluginList)

	// mcper/native/cache_list - List cached plugins
	server.AddTool(&mcp.Tool{
		Name:        "mcper/native/cache_list",
		Description: "List all cached WASM plugins with their metadata including size, hash, and download date.",
		InputSchema: &jsonschema.Schema{
			Type:       "object",
			Properties: map[string]*jsonschema.Schema{},
		},
	}, handleCacheList)

	// mcper/native/version - Get mcper version info
	server.AddTool(&mcp.Tool{
		Name:        "mcper/native/version",
		Description: "Get the current mcper version and installation information.",
		InputSchema: &jsonschema.Schema{
			Type:       "object",
			Properties: map[string]*jsonschema.Schema{},
		},
	}, handleVersion)

	// mcper/native/plugin_info - Get detailed info about a specific plugin
	server.AddTool(&mcp.Tool{
		Name:        "mcper/native/plugin_info",
		Description: "Get detailed information about a specific plugin from the registry.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"name": {
					Type:        "string",
					Description: "Name of the plugin to get info for (e.g., 'github', 'gmail')",
				},
			},
			Required: []string{"name"},
		},
	}, handlePluginInfo)
}

func handleRegistryList(ctx context.Context, ss *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
	manifest, err := fetchPluginsManifest()
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to fetch registry: %v", err)), nil
	}

	if len(manifest.Plugins) == 0 {
		return textResult("No plugins available in the registry."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# MCPer Plugin Registry\n\nFound %d plugins:\n\n", len(manifest.Plugins)))

	for _, p := range manifest.Plugins {
		sb.WriteString(fmt.Sprintf("## %s (v%s)\n", p.Name, p.Version))
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", p.Description))
		if len(p.Env) > 0 {
			sb.WriteString(fmt.Sprintf("**Required env vars:** %s\n", strings.Join(p.Env, ", ")))
		} else {
			sb.WriteString("**Required env vars:** None\n")
		}
		sb.WriteString(fmt.Sprintf("**Install:** `mcper add %s`\n\n", p.Name))
	}

	return textResult(sb.String()), nil
}

func handleRegistrySearch(ctx context.Context, ss *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
	query, ok := params.Arguments["query"].(string)
	if !ok || query == "" {
		return errorResult("Missing required parameter: query"), nil
	}

	manifest, err := fetchPluginsManifest()
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to fetch registry: %v", err)), nil
	}

	query = strings.ToLower(query)
	var matches []PluginInfo

	for _, p := range manifest.Plugins {
		if strings.Contains(strings.ToLower(p.Name), query) ||
			strings.Contains(strings.ToLower(p.Description), query) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 0 {
		return textResult(fmt.Sprintf("No plugins found matching '%s'.", query)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Search Results for '%s'\n\nFound %d matching plugins:\n\n", query, len(matches)))

	for _, p := range matches {
		sb.WriteString(fmt.Sprintf("## %s (v%s)\n", p.Name, p.Version))
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", p.Description))
		if len(p.Env) > 0 {
			sb.WriteString(fmt.Sprintf("**Required env vars:** %s\n", strings.Join(p.Env, ", ")))
		}
		sb.WriteString(fmt.Sprintf("**Install:** `mcper add %s`\n\n", p.Name))
	}

	return textResult(sb.String()), nil
}

func handlePluginList(ctx context.Context, ss *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
	// Find .mcper directory
	cwd, err := os.Getwd()
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to get working directory: %v", err)), nil
	}

	startScript := filepath.Join(cwd, ".mcper", "start.sh")
	if _, err := os.Stat(startScript); os.IsNotExist(err) {
		return textResult("No .mcper/start.sh found in current directory. Run 'mcper init' to initialize."), nil
	}

	config, err := mcper.ParseStartScript(startScript)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to parse start script: %v", err)), nil
	}

	if len(config.Plugins) == 0 {
		return textResult("No plugins configured.\n\nTo add a plugin: `mcper add <name>`"), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Configured Plugins\n\nFound %d plugins in .mcper/start.sh:\n\n", len(config.Plugins)))

	for i, p := range config.Plugins {
		parsed, _ := mcper.ParsePluginSource(p.Source)
		name := "unknown"
		version := ""
		if parsed != nil {
			if parsed.Name != "" {
				name = parsed.Name
			}
			version = parsed.Version
		}

		sb.WriteString(fmt.Sprintf("## %d. %s", i+1, name))
		if version != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", version))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("**Source:** `%s`\n", p.Source))
		if len(p.Env) > 0 {
			sb.WriteString("**Environment mappings:**\n")
			for k, v := range p.Env {
				sb.WriteString(fmt.Sprintf("  - %s -> $%s\n", k, v))
			}
		}
		sb.WriteString("\n")
	}

	return textResult(sb.String()), nil
}

func handleCacheList(ctx context.Context, ss *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to get home directory: %v", err)), nil
	}

	cacheDir := filepath.Join(homeDir, ".mcper", "cache", "plugins")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return textResult("No plugins cached yet."), nil
		}
		return errorResult(fmt.Sprintf("Failed to read cache directory: %v", err)), nil
	}

	var plugins []struct {
		Name     string
		Version  string
		Size     int64
		Metadata *mcper.CacheMetadata
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".wasm") {
			continue
		}

		wasmPath := filepath.Join(cacheDir, entry.Name())
		metaPath := strings.TrimSuffix(wasmPath, ".wasm") + ".json"

		info, err := entry.Info()
		if err != nil {
			continue
		}

		plugin := struct {
			Name     string
			Version  string
			Size     int64
			Metadata *mcper.CacheMetadata
		}{
			Name: strings.TrimSuffix(entry.Name(), ".wasm"),
			Size: info.Size(),
		}

		// Try to read metadata
		if metaData, err := os.ReadFile(metaPath); err == nil {
			var meta mcper.CacheMetadata
			if json.Unmarshal(metaData, &meta) == nil {
				plugin.Metadata = &meta
				// Extract version from source URL if available
				if parsed, err := mcper.ParsePluginSource(meta.Source); err == nil && parsed != nil {
					plugin.Version = parsed.Version
				}
			}
		}

		plugins = append(plugins, plugin)
	}

	if len(plugins) == 0 {
		return textResult("No plugins cached yet."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Cached Plugins\n\nFound %d cached plugins in ~/.mcper/cache/plugins:\n\n", len(plugins)))

	for _, p := range plugins {
		sb.WriteString(fmt.Sprintf("## %s\n", p.Name))
		if p.Version != "" {
			sb.WriteString(fmt.Sprintf("**Version:** %s\n", p.Version))
		}
		sb.WriteString(fmt.Sprintf("**Size:** %.2f KB\n", float64(p.Size)/1024))
		if p.Metadata != nil {
			sb.WriteString(fmt.Sprintf("**Downloaded:** %s\n", p.Metadata.DownloadedAt))
			sb.WriteString(fmt.Sprintf("**SHA256:** %s\n", p.Metadata.SHA256[:16]+"..."))
		}
		sb.WriteString("\n")
	}

	return textResult(sb.String()), nil
}

func handleVersion(ctx context.Context, ss *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
	homeDir, _ := os.UserHomeDir()
	binPath := filepath.Join(homeDir, ".mcper", "bin", "mcper")

	var sb strings.Builder
	sb.WriteString("# MCPer Version Info\n\n")
	sb.WriteString(fmt.Sprintf("**Version:** %s\n", mcper.Version))
	sb.WriteString(fmt.Sprintf("**Binary:** %s\n", binPath))
	sb.WriteString(fmt.Sprintf("**Registry:** %s/plugins.json\n", mcper.GCSBaseURL))
	sb.WriteString(fmt.Sprintf("**Cache:** %s/.mcper/cache/\n", homeDir))

	return textResult(sb.String()), nil
}

func handlePluginInfo(ctx context.Context, ss *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
	name, ok := params.Arguments["name"].(string)
	if !ok || name == "" {
		return errorResult("Missing required parameter: name"), nil
	}

	manifest, err := fetchPluginsManifest()
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to fetch registry: %v", err)), nil
	}

	for _, p := range manifest.Plugins {
		if strings.EqualFold(p.Name, name) {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("# Plugin: %s\n\n", p.Name))
			sb.WriteString(fmt.Sprintf("**Version:** %s\n", p.Version))
			sb.WriteString(fmt.Sprintf("**Description:** %s\n", p.Description))
			if p.Author != "" {
				sb.WriteString(fmt.Sprintf("**Author:** %s\n", p.Author))
			}
			sb.WriteString(fmt.Sprintf("**Source:** %s\n", p.Source))

			if len(p.Env) > 0 {
				sb.WriteString("\n**Required Environment Variables:**\n")
				for _, env := range p.Env {
					sb.WriteString(fmt.Sprintf("  - `%s`\n", env))
				}
			} else {
				sb.WriteString("\n**Required Environment Variables:** None\n")
			}

			sb.WriteString(fmt.Sprintf("\n**Install Command:**\n```bash\nmcper add %s\n```\n", p.Name))

			// Show the resolved URL
			resolvedURL := mcper.PluginURL(p.Name, p.Version)
			sb.WriteString(fmt.Sprintf("\n**Download URL:** %s\n", resolvedURL))

			return textResult(sb.String()), nil
		}
	}

	return textResult(fmt.Sprintf("Plugin '%s' not found in registry.\n\nRun `mcper/native/registry_list` to see available plugins.", name)), nil
}

// Helper functions for creating tool results
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}
