package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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

// chromeBridgeManager handles automatic chrome-bridge lifecycle
type chromeBridgeManager struct {
	cmd     *exec.Cmd
	running bool
}

// startChromeBridge starts chrome-bridge if chrome plugin is being used
func startChromeBridge(plugins []mcper.PluginConfig) (*chromeBridgeManager, error) {
	// Check if any plugin is the chrome plugin
	needsChrome := false
	for _, p := range plugins {
		if strings.Contains(strings.ToLower(p.Source), "chrome") ||
			strings.Contains(strings.ToLower(p.Source), "plugin-chrome") {
			needsChrome = true
			break
		}
	}

	if !needsChrome {
		return nil, nil
	}

	log.Printf("Chrome plugin detected, starting chrome-bridge...")

	// Find or download chrome-bridge
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	mcperDir := filepath.Join(homeDir, ".mcper")
	bridgePath := filepath.Join(mcperDir, "chrome-bridge")

	// Check if chrome-bridge exists
	if _, err := os.Stat(bridgePath); os.IsNotExist(err) {
		log.Printf("chrome-bridge not found, downloading...")
		if err := downloadChromeBridge(bridgePath); err != nil {
			return nil, fmt.Errorf("failed to download chrome-bridge: %w", err)
		}
	}

	// Make executable
	if err := os.Chmod(bridgePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to make chrome-bridge executable: %w", err)
	}

	// Find Chrome executable
	chromePath := findChrome()
	if chromePath == "" {
		log.Printf("Warning: Chrome not found in standard locations, chrome-bridge may fail")
	}

	// Start chrome-bridge
	args := []string{"-server", "-port", "9223"}
	if chromePath != "" {
		args = append(args, "-chrome-path", chromePath)
	}

	cmd := exec.Command(bridgePath, args...)
	cmd.Stdout = os.Stderr // Redirect to stderr so it doesn't interfere with MCP protocol
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start chrome-bridge: %w", err)
	}

	// Wait for chrome-bridge to be ready
	ready := false
	for i := 0; i < 30; i++ { // Wait up to 15 seconds
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get("http://localhost:9223/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
	}

	if !ready {
		cmd.Process.Kill()
		return nil, fmt.Errorf("chrome-bridge failed to start within timeout")
	}

	log.Printf("chrome-bridge started successfully (PID: %d)", cmd.Process.Pid)

	return &chromeBridgeManager{cmd: cmd, running: true}, nil
}

func (m *chromeBridgeManager) Stop() {
	if m == nil || !m.running {
		return
	}
	log.Printf("Stopping chrome-bridge...")
	if m.cmd.Process != nil {
		m.cmd.Process.Kill()
		m.cmd.Wait()
	}
	m.running = false
}

func downloadChromeBridge(destPath string) error {
	// Get current version
	version := mcper.Version

	// Determine platform
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	suffix := fmt.Sprintf("%s-%s", goos, goarch)

	url := fmt.Sprintf("https://storage.googleapis.com/mcper-releases/v%s/chrome-bridge-%s", version, suffix)
	log.Printf("Downloading chrome-bridge from %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Try latest
		url = fmt.Sprintf("https://storage.googleapis.com/mcper-releases/latest/chrome-bridge-%s", suffix)
		log.Printf("Version-specific not found, trying %s", url)
		resp, err = http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to download chrome-bridge: HTTP %d", resp.StatusCode)
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Write to file
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func findChrome() string {
	// Common Chrome paths
	paths := []string{
		// macOS
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		// Linux
		"/usr/bin/google-chrome-stable",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		// Windows (when running via WSL or similar)
		"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try to find via PATH
	if path, err := exec.LookPath("google-chrome"); err == nil {
		return path
	}
	if path, err := exec.LookPath("chromium"); err == nil {
		return path
	}

	return ""
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

	// Auto-start chrome-bridge if chrome plugin is being used
	chromeBridge, err := startChromeBridge(config.Plugins)
	if err != nil {
		log.Printf("Warning: failed to start chrome-bridge: %v", err)
		log.Printf("Chrome plugin may not work correctly")
	}
	if chromeBridge != nil {
		defer chromeBridge.Stop()
	}

	// Check for cloud credentials and configure proxy
	var proxyURL string
	var apiKey string
	creds, err := mcper.LoadCredentials()
	if err == nil && creds.IsValid() {
		proxyURL = creds.GetProxyURL()
		apiKey = creds.APIKey
		log.Printf("Logged in as %s, using cloud proxy for OAuth tokens: %s", creds.UserEmail, proxyURL)

		// Fetch remote servers from mcper-cloud
		remoteServers, err := mcper.FetchRemoteServers(creds)
		if err != nil {
			log.Printf("Warning: failed to fetch remote servers: %v", err)
		} else if len(remoteServers) > 0 {
			log.Printf("Fetched %d remote server(s) from mcper-cloud", len(remoteServers))
			for _, srv := range remoteServers {
				// Add remote servers to config with IsCloud flag
				config.Plugins = append(config.Plugins, mcper.PluginConfig{
					Source:  srv.URL,
					IsCloud: true, // Mark as fetched from mcper-cloud
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

		// Cloud plugins are forwarded to mcper-cloud, not run locally
		if plugin.IsCloud {
			log.Printf("Loading cloud plugin: %s (forwarding to mcper-cloud)", plugin.Source)
			session, err := loadCloudPlugin(ctx, mcpServer, name, plugin, creds)
			if err != nil {
				log.Printf("ERROR: failed to load cloud plugin %s: %v", plugin.Source, err)
				return fmt.Errorf("failed to load cloud plugin %s: %w", plugin.Source, err)
			}
			sessions[name] = session
			log.Printf("Successfully loaded cloud plugin: %s", plugin.Source)
			continue // Skip local WASM handling
		}

		switch parsed.Type {
		case mcper.PluginTypeLocal:
			// Local WASM file
			log.Printf("Loading local WASM: %s", plugin.Source)
			session, err := loadLocalWASM(ctx, wasmHost, mcpServer, name, plugin, proxyURL, apiKey)
			if err != nil {
				log.Printf("ERROR: failed to load local WASM %s: %v", plugin.Source, err)
				return fmt.Errorf("failed to load local WASM %s: %w", plugin.Source, err)
			}
			sessions[name] = session
			log.Printf("Successfully loaded local WASM: %s", plugin.Source)

		case mcper.PluginTypeWASM:
			// Remote WASM - check cache first
			log.Printf("Loading remote WASM: %s", plugin.Source)
			session, err := loadRemoteWASM(ctx, wasmHost, mcpServer, name, plugin, parsed, proxyURL, apiKey)
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
func loadLocalWASM(ctx context.Context, host *wasmhost.WasmHost, server *mcp.Server, name string, plugin mcper.PluginConfig, proxyURL, apiKey string) (*mcp.ClientSession, error) {
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

	return runWASMModule(ctx, host, server, name, pluginName, wasmBytes, plugin, proxyURL, apiKey)
}

// loadRemoteWASM loads a remote WASM file from cache or downloads it
func loadRemoteWASM(ctx context.Context, host *wasmhost.WasmHost, server *mcp.Server, name string, plugin mcper.PluginConfig, parsed *mcper.ParsedPlugin, proxyURL, apiKey string) (*mcp.ClientSession, error) {
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

	return runWASMModule(ctx, host, server, name, pluginName, wasmBytes, plugin, proxyURL, apiKey)
}

// runWASMModule loads and runs a WASM module, registering its tools with the MCP server
func runWASMModule(ctx context.Context, host *wasmhost.WasmHost, server *mcp.Server, name string, pluginName string, wasmBytes []byte, plugin mcper.PluginConfig, proxyURL, apiKey string) (*mcp.ClientSession, error) {
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
		// Standard proxy vars (for HTTP clients that support them)
		envVars = append(envVars, fmt.Sprintf("HTTP_PROXY=%s", proxyURL))
		envVars = append(envVars, fmt.Sprintf("HTTPS_PROXY=%s", proxyURL))
		// MCPER-specific vars (for plugins that use custom proxy logic)
		envVars = append(envVars, fmt.Sprintf("MCPER_PROXY_URL=%s", proxyURL))
		if apiKey != "" {
			envVars = append(envVars, fmt.Sprintf("MCPER_AUTH_TOKEN=%s", apiKey))
		}
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

		// Create namespaced tool name based on source:
		// - Local WASM: wasm/<pluginName>/<toolName>
		// - Cloud WASM: cloud/<pluginName>/<toolName>
		namespace := "wasm"
		if plugin.IsCloud {
			namespace = "cloud"
		}
		namespacedName := fmt.Sprintf("%s/%s/%s", namespace, pluginName, tool.Name)
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

// loadCloudPlugin connects to mcper-cloud's MCP endpoint and forwards its tools
// This is used for plugins with IsCloud: true - tool calls are forwarded to the cloud
// instead of running WASM locally
func loadCloudPlugin(ctx context.Context, server *mcp.Server, name string, plugin mcper.PluginConfig, creds *mcper.Credentials) (*mcp.ClientSession, error) {
	if creds == nil || !creds.IsValid() {
		return nil, fmt.Errorf("valid credentials required for cloud plugins")
	}

	// Connect to mcper-cloud's MCP endpoint (Streamable HTTP transport)
	mcpEndpointURL := creds.CloudURL + "/mcp"
	log.Printf("Connecting to cloud MCP endpoint: %s", mcpEndpointURL)

	// Create HTTP client with Bearer token auth
	httpClient := &http.Client{
		Transport: &bearerAuthRoundTripper{
			base:  http.DefaultTransport,
			token: creds.APIKey,
		},
		Timeout: 5 * time.Minute, // Long timeout for streaming
	}

	// Create MCP client for the cloud server
	cloudClient := mcp.NewClient("Cloud-"+name, "1.0.0", nil)
	transport := mcp.NewStreamableClientTransport(mcpEndpointURL, &mcp.StreamableClientTransportOptions{
		HTTPClient: httpClient,
	})

	session, err := cloudClient.Connect(ctx, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to cloud MCP server: %w", err)
	}

	// Get tools from the cloud server
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to list tools from cloud server: %w", err)
	}

	// Extract plugin name from the source URL
	parsed, parseErr := mcper.ParsePluginSource(plugin.Source)
	pluginName := name // fallback
	if parseErr == nil && parsed.Name != "" {
		pluginName = parsed.Name
	}

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
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Cloud tool call failed: %v", err)}},
				}, nil
			}
			return &mcp.CallToolResult{
				Meta:    result.Meta,
				Content: result.Content,
				IsError: result.IsError,
			}, nil
		}

		// Cloud tools use cloud/{pluginName}/{toolName} namespace
		namespacedName := fmt.Sprintf("cloud/%s/%s", pluginName, tool.Name)
		server.AddTool(&mcp.Tool{
			Name:        namespacedName,
			Description: tool.Description,
			InputSchema: inputSchema,
		}, handler)

		log.Printf("Registered cloud tool: %s", namespacedName)
	}

	log.Printf("Successfully connected to cloud plugin with %d tools", len(tools.Tools))
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

// bearerAuthRoundTripper wraps an http.RoundTripper to add Bearer token auth
type bearerAuthRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (rt *bearerAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid mutating the original
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.base.RoundTrip(req2)
}
