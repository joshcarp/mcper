package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var (
	addAPIName        string
	addAPIBaseURL     string
	addAPIAllowedURLs []string
	addAPIAuthToken   string
	addAPIAuthHeader  string
	addAPIAuthFormat  string
	addAPIOpenAPIURL  string
	addAPIDescription string
)

// presets for common APIs that work with mcper-cloud OAuth proxy
var apiPresets = map[string]struct {
	BaseURL     string
	Description string
	AuthEnvVar  string // fallback env var if not using proxy
}{
	"github": {
		BaseURL:     "https://api.github.com",
		Description: "GitHub API — repos, issues, PRs, users, search, and more",
		AuthEnvVar:  "GITHUB_TOKEN",
	},
	"google": {
		BaseURL:     "https://www.googleapis.com",
		Description: "Google APIs — Drive, Sheets, Calendar, Gmail, and more",
		AuthEnvVar:  "GOOGLE_ACCESS_TOKEN",
	},
	"gmail": {
		BaseURL:     "https://gmail.googleapis.com",
		Description: "Gmail API — read, send, label, and search emails",
		AuthEnvVar:  "GMAIL_ACCESS_TOKEN",
	},
	"google-drive": {
		BaseURL:     "https://drive.googleapis.com",
		Description: "Google Drive API — files, folders, permissions, and search",
		AuthEnvVar:  "GOOGLE_ACCESS_TOKEN",
	},
	"google-sheets": {
		BaseURL:     "https://sheets.googleapis.com",
		Description: "Google Sheets API — read, write, and format spreadsheets",
		AuthEnvVar:  "GOOGLE_ACCESS_TOKEN",
	},
	"google-calendar": {
		BaseURL:     "https://www.googleapis.com/calendar",
		Description: "Google Calendar API — events, calendars, and scheduling",
		AuthEnvVar:  "GOOGLE_ACCESS_TOKEN",
	},
	"linkedin": {
		BaseURL:     "https://api.linkedin.com",
		Description: "LinkedIn API — profile, posts, connections, and messaging",
		AuthEnvVar:  "LINKEDIN_ACCESS_TOKEN",
	},
	"slack": {
		BaseURL:     "https://slack.com/api",
		Description: "Slack API — messages, channels, users, and reactions",
		AuthEnvVar:  "SLACK_TOKEN",
	},
	"microsoft-graph": {
		BaseURL:     "https://graph.microsoft.com",
		Description: "Microsoft Graph API — Office 365, Azure AD, OneDrive, Teams",
		AuthEnvVar:  "AZURE_ACCESS_TOKEN",
	},
}

var addAPICmd = &cobra.Command{
	Use:   "add-api [preset]",
	Short: "Generate a custom API proxy WASM plugin",
	Long: `Generate a WASM plugin that proxies HTTP requests to any API with automatic auth injection.

Presets (auto-configured, use mcper-cloud OAuth when logged in):
  mcper add-api github
  mcper add-api google
  mcper add-api gmail
  mcper add-api slack
  mcper add-api linkedin
  mcper add-api microsoft-graph

Custom APIs:
  mcper add-api --name stripe --base-url https://api.stripe.com --auth-token '${STRIPE_KEY}'
  mcper add-api --name httpbin --base-url https://httpbin.org`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAddAPI,
}

func init() {
	addAPICmd.Flags().StringVar(&addAPIName, "name", "", "API name (e.g., stripe)")
	addAPICmd.Flags().StringVar(&addAPIBaseURL, "base-url", "", "Base URL for the API")
	addAPICmd.Flags().StringSliceVar(&addAPIAllowedURLs, "allowed-urls", nil, "URL path regex patterns (default: allow all)")
	addAPICmd.Flags().StringVar(&addAPIAuthToken, "auth-token", "", "Env var reference for auth token (e.g., ${STRIPE_KEY})")
	addAPICmd.Flags().StringVar(&addAPIAuthHeader, "auth-header", "Authorization", "HTTP header name for auth")
	addAPICmd.Flags().StringVar(&addAPIAuthFormat, "auth-format", "Bearer %s", "Format string for auth header value")
	addAPICmd.Flags().StringVar(&addAPIOpenAPIURL, "openapi-url", "", "URL to fetch OpenAPI spec at runtime")
	addAPICmd.Flags().StringVar(&addAPIDescription, "description", "", "API description for tool")
}

func runAddAPI(cmd *cobra.Command, args []string) error {
	// Apply preset if provided as positional arg
	if len(args) == 1 {
		preset, ok := apiPresets[args[0]]
		if !ok {
			// List available presets
			fmt.Printf("Unknown preset %q. Available presets:\n", args[0])
			for name := range apiPresets {
				fmt.Printf("  %s\n", name)
			}
			return fmt.Errorf("use --name and --base-url for custom APIs")
		}
		if addAPIName == "" {
			addAPIName = args[0]
		}
		if addAPIBaseURL == "" {
			addAPIBaseURL = preset.BaseURL
		}
		if addAPIDescription == "" {
			addAPIDescription = preset.Description
		}
		if addAPIAuthToken == "" {
			addAPIAuthToken = "${" + preset.AuthEnvVar + "}"
		}
	}

	if addAPIName == "" || addAPIBaseURL == "" {
		return fmt.Errorf("--name and --base-url are required (or use a preset name)")
	}

	// Resolve auth env var name from ${VAR} syntax
	authEnvVar := strings.TrimPrefix(strings.TrimSuffix(addAPIAuthToken, "}"), "${")

	// Build allowed URLs JSON
	allowedURLsJSON, _ := json.Marshal(addAPIAllowedURLs) // string slices always marshal safely

	// Write embedded source to temp dir
	tmpDir, err := os.MkdirTemp("", "mcper-api-proxy-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for name, content := range map[string]string{
		"main.go": embeddedPluginSource,
		"go.mod":  embeddedPluginGoMod,
		"go.sum":  embeddedPluginGoSum,
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	// Ensure output dir exists
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home dir: %w", err)
	}
	pluginsDir := filepath.Join(homeDir, ".mcper", "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugins dir: %w", err)
	}

	outputPath := filepath.Join(pluginsDir, fmt.Sprintf("plugin-%s.wasm", addAPIName))

	// Build with ldflags
	ldflags := fmt.Sprintf(
		"-X main.pluginName=%s -X main.baseURL=%s -X 'main.allowedURLs=%s' -X main.authHeader=%s -X 'main.authFormat=%s' -X main.authEnvVar=%s -X main.openAPIURL=%s -X 'main.description=%s'",
		addAPIName, addAPIBaseURL, string(allowedURLsJSON),
		addAPIAuthHeader, addAPIAuthFormat, authEnvVar,
		addAPIOpenAPIURL, addAPIDescription,
	)

	fmt.Printf("Building %s API proxy plugin...\n", addAPIName)

	buildCmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", outputPath, ".")
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	buildCmd.Stdout = os.Stderr
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("failed to build WASM plugin: %w", err)
	}

	fmt.Printf("Built: %s\n", outputPath)

	// Add to project config if .mcper/start.sh exists
	cwd, err := os.Getwd()
	if err == nil {
		startPath := filepath.Join(cwd, ".mcper", mcper.StartScriptName)
		if _, statErr := os.Stat(startPath); statErr == nil {
			if config, parseErr := mcper.ParseStartScript(startPath); parseErr == nil && !config.HasPlugin(outputPath) {
				plugin := mcper.PluginConfig{Source: outputPath}
				if authEnvVar != "" {
					plugin.Env = map[string]string{authEnvVar: authEnvVar}
				}
				config.AddPlugin(plugin)
				if err := mcper.UpdateStartScript(startPath, config); err != nil {
					fmt.Printf("Warning: could not update start.sh: %v\n", err)
				} else {
					fmt.Printf("Added to .mcper/start.sh\n")
				}
			}
		}
	}

	fmt.Printf("\nPlugin %s ready!\n", addAPIName)
	fmt.Printf("  Base URL: %s\n", addAPIBaseURL)
	if authEnvVar != "" {
		fmt.Printf("  Auth: %s (or auto via mcper-cloud OAuth proxy)\n", authEnvVar)
	}

	return nil
}
