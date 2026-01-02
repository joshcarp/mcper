package mcper

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// GitHubRepo is the GitHub repository for mcper
	GitHubRepo = "joshcarp/mcper"

	// GitHubReleasesURL is the base URL for GitHub releases
	GitHubReleasesURL = "https://github.com/joshcarp/mcper/releases/download"
)

// PluginType represents the type of plugin source
type PluginType int

const (
	PluginTypeWASM PluginType = iota
	PluginTypeLocal
	PluginTypeHTTP
)

// ParsedPlugin represents a parsed plugin URL
type ParsedPlugin struct {
	Type    PluginType
	Name    string // e.g., "linkedin"
	Version string // e.g., "1.2.0"
	RawURL  string // Original URL
}

// pluginURLPattern matches GitHub release WASM URLs like:
// https://github.com/joshcarp/mcper/releases/download/v0.1.0/plugin-github.wasm
var pluginURLPattern = regexp.MustCompile(`^https://github\.com/joshcarp/mcper/releases/download/v([^/]+)/plugin-([^/]+)\.wasm$`)

// ParsePluginSource parses a plugin source URL
// Supported formats:
//   - https://github.com/joshcarp/mcper/releases/download/v0.1.0/plugin-linkedin.wasm
//   - ./local.wasm
//   - http://localhost:3000/mcp
func ParsePluginSource(source string) (*ParsedPlugin, error) {
	parsed := &ParsedPlugin{RawURL: source}

	// Local file paths
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "/") {
		parsed.Type = PluginTypeLocal
		parsed.Name = filepath.Base(source)
		return parsed, nil
	}

	// Check for GitHub release WASM plugin URL
	if matches := pluginURLPattern.FindStringSubmatch(source); matches != nil {
		parsed.Type = PluginTypeWASM
		parsed.Version = matches[1]
		parsed.Name = matches[2]
		return parsed, nil
	}

	// Parse as URL
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin source: %w", err)
	}

	switch u.Scheme {
	case "http", "https":
		parsed.Type = PluginTypeHTTP
		parsed.Name = u.Path

	default:
		return nil, fmt.Errorf("unsupported plugin scheme: %s", u.Scheme)
	}

	return parsed, nil
}

// PluginURL returns the full GitHub releases URL for a plugin name and version
func PluginURL(name, version string) string {
	return fmt.Sprintf("%s/v%s/plugin-%s.wasm", GitHubReleasesURL, version, name)
}

// CachePath returns the cache file path for a WASM plugin
func (p *ParsedPlugin) CachePath(cacheDir string) string {
	if p.Type != PluginTypeWASM {
		return ""
	}

	filename := p.Name
	if p.Version != "" {
		filename = fmt.Sprintf("%s@%s", p.Name, p.Version)
	}

	return filepath.Join(cacheDir, "plugins", filename+".wasm")
}

// MetadataPath returns the metadata JSON file path for a cached plugin
func (p *ParsedPlugin) MetadataPath(cacheDir string) string {
	if p.Type != PluginTypeWASM {
		return ""
	}

	filename := p.Name
	if p.Version != "" {
		filename = fmt.Sprintf("%s@%s", p.Name, p.Version)
	}

	return filepath.Join(cacheDir, "plugins", filename+".json")
}

// RegistryURL returns the URL to download the plugin from
func (p *ParsedPlugin) RegistryURL() string {
	if p.Type != PluginTypeWASM {
		return ""
	}

	// Already have the full URL in RawURL
	return p.RawURL
}
