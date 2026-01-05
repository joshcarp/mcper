package mcper

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config represents the mcper configuration (embedded in serve.sh)
type Config struct {
	Plugins []PluginConfig `json:"plugins"`
}

// PluginConfig represents a single plugin configuration
type PluginConfig struct {
	Source      string            `json:"source"`
	Env         map[string]string `json:"env,omitempty"`
	Permissions *Permissions      `json:"permissions,omitempty"`
	IsCloud     bool              `json:"-"` // Internal: true for plugins fetched from mcper-cloud
}

// Permissions defines what a plugin is allowed to do
type Permissions struct {
	Network    []string `json:"network,omitempty"`    // Allowed hosts
	Filesystem []string `json:"filesystem,omitempty"` // Allowed paths
}

// ParseConfig parses a JSON config string into a Config struct
func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

// LoadConfigFile loads config from a JSON file
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return ParseConfig(data)
}

// ToJSON converts the config to JSON bytes
func (c *Config) ToJSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// AddPlugin adds a plugin to the config
func (c *Config) AddPlugin(plugin PluginConfig) {
	c.Plugins = append(c.Plugins, plugin)
}

// RemovePlugin removes a plugin by source
func (c *Config) RemovePlugin(source string) bool {
	for i, p := range c.Plugins {
		if p.Source == source {
			c.Plugins = append(c.Plugins[:i], c.Plugins[i+1:]...)
			return true
		}
	}
	return false
}

// HasPlugin checks if a plugin exists by source
func (c *Config) HasPlugin(source string) bool {
	for _, p := range c.Plugins {
		if p.Source == source {
			return true
		}
	}
	return false
}

// DefaultConfig returns an empty config
func DefaultConfig() *Config {
	return &Config{
		Plugins: []PluginConfig{},
	}
}
