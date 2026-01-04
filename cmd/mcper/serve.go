package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/joshcarp/mcper/pkg/wasmhost"
	"github.com/modelcontextprotocol/go-sdk/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var (
	configJSON string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run MCP server with plugins",
	Long: `Run an MCP server that aggregates plugins defined in the config.

The server communicates over stdin/stdout using the MCP protocol.

Examples:
  mcper serve --config-json '{"plugins":[...]}'
  .mcper/serve.sh  # which calls: mcper serve --config-json "$CONFIG"`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().StringVar(&configJSON, "config-json", "", "JSON configuration string")
	serveCmd.MarkFlagRequired("config-json")
}

// setupLogging configures logging to write to ~/.mcper/mcper.log
func setupLogging() (*os.File, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	mcperDir := filepath.Join(homeDir, ".mcper")
	if err := os.MkdirAll(mcperDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(mcperDir, "mcper.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	// Write to both stderr and log file
	multiWriter := io.MultiWriter(os.Stderr, logFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	return logFile, nil
}

func runServe(cmd *cobra.Command, args []string) error {
	// Setup file logging
	logFile, err := setupLogging()
	if err != nil {
		log.Printf("Warning: failed to setup file logging: %v", err)
	} else {
		defer logFile.Close()
	}

	// Log startup info
	log.Printf("=== mcper serve starting ===")
	log.Printf("Version: %s", mcper.Version)
	log.Printf("OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	log.Printf("Go version: %s", runtime.Version())
	log.Printf("Time: %s", time.Now().Format(time.RFC3339))
	log.Printf("PID: %d", os.Getpid())
	if cwd, err := os.Getwd(); err == nil {
		log.Printf("Working directory: %s", cwd)
	}

	// Parse config
	config, err := mcper.ParseConfig([]byte(configJSON))
	if err != nil {
		log.Printf("ERROR: failed to parse config: %v", err)
		return fmt.Errorf("failed to parse config: %w", err)
	}
	log.Printf("Config: %s", configJSON)

	log.Printf("Loading %d plugin(s)...", len(config.Plugins))

	// Check for cloud credentials and configure proxy
	var proxyURL string
	creds, err := mcper.LoadCredentials()
	if err == nil && creds.IsValid() {
		proxyURL = creds.GetProxyURL()
		log.Printf("Logged in as %s, using cloud proxy for OAuth tokens: %s", creds.UserEmail, proxyURL)

		// Fetch remote servers from mcper-cloud
		remoteServers, err := mcper.FetchRemoteServers(creds)
		if err != nil {
			log.Printf("Warning: failed to fetch remote servers: %v", err)
		} else if len(remoteServers) > 0 {
			log.Printf("Fetched %d remote server(s) from mcper-cloud", len(remoteServers))
			for _, srv := range remoteServers {
				// Add remote servers to config
				config.Plugins = append(config.Plugins, mcper.PluginConfig{
					Source: srv.URL,
				})
				log.Printf("  - %s (%s): %s", srv.Name, srv.Type, srv.URL)
			}
		}
	} else {
		log.Printf("Not logged in to mcper-cloud, plugins will use direct HTTP (env var auth)")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Create WASM host with optional proxy
	wasmHost := wasmhost.NewLoggingWasmHost(ctx)
	defer wasmHost.Close(ctx)

	// Create MCP server
	mcpServer := mcp.NewServer("mcper", mcper.Version, nil)

	// Register native mcper tools (registry, cache, etc.)
	registerNativeTools(mcpServer)
	log.Printf("Registered native mcper tools")

	// Track sessions for cleanup
	sessions := make(map[string]*mcp.ClientSession)

	// Load and run each plugin
	for i, plugin := range config.Plugins {
		name := fmt.Sprintf("plugin-%d", i)
		log.Printf("Loading plugin %d: %s", i, plugin.Source)

		parsed, err := mcper.ParsePluginSource(plugin.Source)
		if err != nil {
			log.Printf("ERROR: failed to parse plugin source %s: %v", plugin.Source, err)
			return fmt.Errorf("failed to parse plugin source %s: %w", plugin.Source, err)
		}
		log.Printf("Plugin %d parsed: type=%d name=%s version=%s", i, parsed.Type, parsed.Name, parsed.Version)

		switch parsed.Type {
		case mcper.PluginTypeLocal:
			// Local WASM file
			log.Printf("Loading local WASM: %s", plugin.Source)
			session, err := loadLocalWASM(ctx, wasmHost, mcpServer, name, plugin, proxyURL)
			if err != nil {
				log.Printf("ERROR: failed to load local WASM %s: %v", plugin.Source, err)
				return fmt.Errorf("failed to load local WASM %s: %w", plugin.Source, err)
			}
			sessions[name] = session
			log.Printf("Successfully loaded local WASM: %s", plugin.Source)

		case mcper.PluginTypeWASM:
			// Remote WASM - check cache first
			log.Printf("Loading remote WASM: %s", plugin.Source)
			session, err := loadRemoteWASM(ctx, wasmHost, mcpServer, name, plugin, parsed, proxyURL)
			if err != nil {
				log.Printf("ERROR: failed to load remote WASM %s: %v", plugin.Source, err)
				return fmt.Errorf("failed to load remote WASM %s: %w", plugin.Source, err)
			}
			sessions[name] = session
			log.Printf("Successfully loaded remote WASM: %s", plugin.Source)

		case mcper.PluginTypeHTTP:
			// HTTP MCP server
			log.Printf("Loading HTTP plugin: %s", plugin.Source)
			session, err := loadHTTPPlugin(ctx, mcpServer, name, plugin)
			if err != nil {
				log.Printf("ERROR: failed to load HTTP plugin %s: %v", plugin.Source, err)
				return fmt.Errorf("failed to load HTTP plugin %s: %w", plugin.Source, err)
			}
			sessions[name] = session
			log.Printf("Successfully loaded HTTP plugin: %s", plugin.Source)

		default:
			log.Printf("ERROR: unsupported plugin type for %s", plugin.Source)
			return fmt.Errorf("unsupported plugin type for %s", plugin.Source)
		}
	}

	log.Printf("Starting MCP server with %d plugins", len(config.Plugins))

	// Run MCP server on stdin/stdout
	transport := mcp.NewIOTransport(stdinoutRWC{})
	return mcpServer.Run(ctx, transport)
}

// loadLocalWASM loads a local WASM file and registers its tools
func loadLocalWASM(ctx context.Context, host *wasmhost.WasmHost, server *mcp.Server, name string, plugin mcper.PluginConfig, proxyURL string) (*mcp.ClientSession, error) {
	// Resolve source path
	source := plugin.Source
	if strings.HasPrefix(source, "./") {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		source = cwd + source[1:]
	}

	wasmBytes, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM file: %w", err)
	}

	// Extract plugin name from file path (e.g., "plugin-hello.wasm" -> "hello")
	baseName := filepath.Base(plugin.Source)
	pluginName := strings.TrimSuffix(baseName, ".wasm")
	pluginName = strings.TrimPrefix(pluginName, "plugin-")

	return runWASMModule(ctx, host, server, name, pluginName, wasmBytes, plugin, proxyURL)
}

// loadRemoteWASM loads a remote WASM file from cache or downloads it
func loadRemoteWASM(ctx context.Context, host *wasmhost.WasmHost, server *mcp.Server, name string, plugin mcper.PluginConfig, parsed *mcper.ParsedPlugin, proxyURL string) (*mcp.ClientSession, error) {
	// Check cache first
	entry, err := mcper.GetCacheEntry(parsed)
	if err != nil {
		return nil, err
	}

	var wasmBytes []byte

	if entry != nil {
		// Load from cache
		wasmBytes, err = os.ReadFile(entry.WASMPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read cached WASM: %w", err)
		}

		// Verify integrity
		valid, err := mcper.VerifyCache(entry)
		if err != nil || !valid {
			log.Printf("Cache integrity check failed for %s, re-downloading", plugin.Source)
			entry = nil
		}
	}

	if entry == nil {
		// Download from registry
		url := parsed.RegistryURL()
		log.Printf("Downloading plugin from %s", url)

		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to download WASM: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to download WASM: HTTP %d", resp.StatusCode)
		}

		wasmBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read WASM response: %w", err)
		}

		// Get env var names from plugin config
		envVars := make([]string, 0, len(plugin.Env))
		for k := range plugin.Env {
			envVars = append(envVars, k)
		}

		// Save to cache
		_, err = mcper.SaveToCache(parsed, wasmBytes, plugin.Permissions, envVars)
		if err != nil {
			log.Printf("Warning: failed to cache WASM: %v", err)
		}
	}

	// Use parsed plugin name for namespacing
	pluginName := parsed.Name
	if pluginName == "" {
		pluginName = name // fallback to internal name
	}

	return runWASMModule(ctx, host, server, name, pluginName, wasmBytes, plugin, proxyURL)
}

// runWASMModule loads and runs a WASM module, registering its tools with the MCP server
func runWASMModule(ctx context.Context, host *wasmhost.WasmHost, server *mcp.Server, name string, pluginName string, wasmBytes []byte, plugin mcper.PluginConfig, proxyURL string) (*mcp.ClientSession, error) {
	// Load the module
	if err := host.LoadModule(ctx, name, wasmBytes); err != nil {
		return nil, fmt.Errorf("failed to load WASM module: %w", err)
	}

	// Resolve environment variables: plugin.Env maps WASM env name -> host env name
	var envVars []string
	for wasmEnvName, hostEnvName := range plugin.Env {
		value := os.Getenv(hostEnvName)
		if value != "" {
			envVars = append(envVars, fmt.Sprintf("%s=%s", wasmEnvName, value))
			log.Printf("Passing env var %s to WASM module", wasmEnvName)
		} else {
			log.Printf("Warning: env var %s (mapped from %s) is empty", wasmEnvName, hostEnvName)
		}
	}

	// Add proxy environment variables if user is logged in
	if proxyURL != "" {
		envVars = append(envVars, fmt.Sprintf("HTTP_PROXY=%s", proxyURL))
		envVars = append(envVars, fmt.Sprintf("HTTPS_PROXY=%s", proxyURL))
		log.Printf("Setting proxy for WASM module: %s", proxyURL)
	}

	// Run the module with environment variables
	read, write, err := host.RunModuleWithLogging(ctx, name, envVars...)
	if err != nil {
		return nil, fmt.Errorf("failed to run WASM module: %w", err)
	}

	// Create MCP client for the WASM module
	wasmClient := mcp.NewClient("WASM-"+name, "1.0.0", nil)
	transport := mcp.NewIOTransport(&wasmConn{read: read, write: write})

	session, err := wasmClient.Connect(ctx, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to WASM module: %w", err)
	}

	// Get tools from the WASM module
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from WASM module: %w", err)
	}

	// Register each tool with the MCP server
	for _, tool := range tools.Tools {
		inputSchema := tool.InputSchema
		if inputSchema == nil || inputSchema.Type == "" {
			inputSchema = &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
		}

		// Create a handler that forwards calls to the WASM module
		toolSession := session
		toolName := tool.Name
		handler := func(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
			callParams := &mcp.CallToolParams{
				Name:      toolName,
				Arguments: params.Arguments,
			}
			result, err := toolSession.CallTool(ctx, callParams)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Tool call failed: %v", err)}}}, nil
			}
			return &mcp.CallToolResult{
				Meta:    result.Meta,
				Content: result.Content,
				IsError: result.IsError,
			}, nil
		}

		// Create namespaced tool name: wasm/<pluginName>/<toolName>
		namespacedName := fmt.Sprintf("wasm/%s/%s", pluginName, tool.Name)
		server.AddTool(&mcp.Tool{
			Name:        namespacedName,
			Description: tool.Description,
			InputSchema: inputSchema,
		}, handler)

		log.Printf("Registered tool: %s", namespacedName)
	}

	return session, nil
}

// loadHTTPPlugin connects to an HTTP MCP server and forwards its tools
func loadHTTPPlugin(ctx context.Context, server *mcp.Server, name string, plugin mcper.PluginConfig) (*mcp.ClientSession, error) {
	httpClient := mcp.NewClient("HTTP-"+name, "1.0.0", nil)
	transport := mcp.NewStreamableClientTransport(plugin.Source, nil)

	session, err := httpClient.Connect(ctx, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP plugin: %w", err)
	}

	// Get tools from the HTTP server
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from HTTP plugin: %w", err)
	}

	// Extract plugin name from URL or use provided name
	pluginName := name

	// Register each tool with the MCP server
	for _, tool := range tools.Tools {
		inputSchema := tool.InputSchema
		if inputSchema == nil || inputSchema.Type == "" {
			inputSchema = &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
		}

		toolSession := session
		toolName := tool.Name
		handler := func(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[map[string]any]) (*mcp.CallToolResult, error) {
			callParams := &mcp.CallToolParams{
				Name:      toolName,
				Arguments: params.Arguments,
			}
			result, err := toolSession.CallTool(ctx, callParams)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Tool call failed: %v", err)}}}, nil
			}
			return &mcp.CallToolResult{
				Meta:    result.Meta,
				Content: result.Content,
				IsError: result.IsError,
			}, nil
		}

		// Create namespaced tool name: http/<pluginName>/<toolName>
		namespacedName := fmt.Sprintf("http/%s/%s", pluginName, tool.Name)
		server.AddTool(&mcp.Tool{
			Name:        namespacedName,
			Description: tool.Description,
			InputSchema: inputSchema,
		}, handler)

		log.Printf("Registered HTTP tool: %s", namespacedName)
	}

	return session, nil
}

// wasmConn implements io.ReadWriteCloser for WASM communication
type wasmConn struct {
	read  io.Reader
	write io.Writer
}

func (w *wasmConn) Read(p []byte) (n int, err error) {
	return w.read.Read(p)
}

func (w *wasmConn) Write(p []byte) (n int, err error) {
	return w.write.Write(p)
}

func (w *wasmConn) Close() error {
	return nil
}

// stdinoutRWC wraps stdin/stdout as an io.ReadWriteCloser
type stdinoutRWC struct{}

func (stdinoutRWC) Read(p []byte) (n int, err error) {
	return os.Stdin.Read(p)
}

func (stdinoutRWC) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

func (stdinoutRWC) Close() error {
	return nil
}
