package main

// embeddedPluginSource is the api-proxy WASM plugin.
// Keep in sync with mcper-cloud/plugins/api-proxy/main.go.
const embeddedPluginSource = `//go:build wasip1

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/breml/rootcerts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "github.com/stealthrocket/net/http"
	_ "github.com/stealthrocket/net/wasip1"
)

var (
	pluginName  = "api-proxy"
	baseURL     = ""
	allowedURLs = ""
	authHeader  = "Authorization"
	authFormat  = "Bearer %s"
	authEnvVar  = ""
	openAPIURL  = ""
	description = ""
)

func newHTTP11Client(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return &http.Client{Transport: transport, Timeout: timeout}
}

var compiledAllowedURLs []*regexp.Regexp

func init() {
	if allowedURLs == "" || allowedURLs == "[]" {
		return
	}
	var patterns []string
	if err := json.Unmarshal([]byte(allowedURLs), &patterns); err != nil {
		log.Printf("Warning: failed to parse allowedURLs: %v", err)
		return
	}
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiledAllowedURLs = append(compiledAllowedURLs, re)
		}
	}
}

func isPathAllowed(path string) bool {
	if len(compiledAllowedURLs) == 0 {
		return true
	}
	for _, re := range compiledAllowedURLs {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

type HTTPRequestParams struct {
	Method  string            ` + "`" + `json:"method"` + "`" + `
	Path    string            ` + "`" + `json:"path"` + "`" + `
	Headers map[string]string ` + "`" + `json:"headers,omitempty"` + "`" + `
	Body    string            ` + "`" + `json:"body,omitempty"` + "`" + `
	Query   map[string]string ` + "`" + `json:"query,omitempty"` + "`" + `
}

func main() {
	server := mcp.NewServer(pluginName+" API Proxy", "1.0.0", nil)

	toolDescription := fmt.Sprintf("Make HTTP requests to the %s API (%s)", pluginName, baseURL)
	if description != "" {
		toolDescription = description
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "http_request",
		Description: toolDescription,
	}, httpRequestHandler)

	if openAPIURL != "" {
		go fetchAndLogOpenAPI()
	}

	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

func httpRequestHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[HTTPRequestParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments

	method := strings.ToUpper(args.Method)
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD":
	default:
		return errResult("Invalid method: %s", method), nil
	}

	if args.Path == "" {
		return errResult("Path is required"), nil
	}
	if !strings.HasPrefix(args.Path, "/") {
		args.Path = "/" + args.Path
	}
	if !isPathAllowed(args.Path) {
		return errResult("Path %q not allowed", args.Path), nil
	}

	fullURL := strings.TrimRight(baseURL, "/") + args.Path
	if len(args.Query) > 0 {
		q := url.Values{}
		for k, v := range args.Query {
			q.Set(k, v)
		}
		fullURL += "?" + q.Encode()
	}

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return errResult("Failed to create request: %v", err), nil
	}

	req.Header.Set("Accept", "application/json")
	if args.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	if authEnvVar != "" {
		if token := os.Getenv(authEnvVar); token != "" {
			req.Header.Set(authHeader, fmt.Sprintf(authFormat, token))
		}
	}

	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	resp, err := newHTTP11Client(60 * time.Second).Do(req)
	if err != nil {
		return errResult("Request failed: %v", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errResult("Failed to read response: %v", err), nil
	}

	respHeaders := make(map[string]string)
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	resultJSON, _ := json.MarshalIndent(map[string]any{
		"status":  resp.StatusCode,
		"headers": respHeaders,
		"body":    string(body),
	}, "", "  ")

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(resultJSON)}},
	}, nil
}

func fetchAndLogOpenAPI() {
	resp, err := newHTTP11Client(30 * time.Second).Get(openAPIURL)
	if err != nil {
		log.Printf("Warning: failed to fetch OpenAPI spec: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return
	}
	log.Printf("Fetched OpenAPI spec (%d bytes)", len(body))
	os.WriteFile(fmt.Sprintf("/tmp/openapi-%s.json", pluginName), body, 0644)
}

func errResult(format string, args ...any) *mcp.CallToolResultFor[any] {
	return &mcp.CallToolResultFor[any]{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}
`

const embeddedPluginGoMod = `module api-proxy

go 1.24

require (
	github.com/breml/rootcerts v0.3.0
	github.com/modelcontextprotocol/go-sdk v0.1.0
	github.com/stealthrocket/net v0.2.1
)

require (
	github.com/stealthrocket/wazergo v0.19.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
)

replace github.com/modelcontextprotocol/go-sdk => github.com/joshcarp/go-sdk v0.0.0-20250710224607-e052a06d1858
`

const embeddedPluginGoSum = `github.com/breml/rootcerts v0.3.0 h1:lED3QcIJvBsWta8faA/EXq9L+5nTwNMRyMTbA9UkzCM=
github.com/breml/rootcerts v0.3.0/go.mod h1:S/PKh+4d1HUn4HQovEB8hPJZO6pUZYrIhmXBhsegfXw=
github.com/joshcarp/go-sdk v0.0.0-20250710224607-e052a06d1858 h1:X3/1G720LmAwlYPJcGQckiiaXUUUEXrm7THl93EtCcg=
github.com/joshcarp/go-sdk v0.0.0-20250710224607-e052a06d1858/go.mod h1:NVqw+FXG6dbGmyYkoaAyvh/lwgBJCkIQmREZ4U7dt1Y=
github.com/stealthrocket/net v0.2.1 h1:PehPGAAjuV46zaeHGlNgakFV7QDGUAREMcEQsZQ8NLo=
github.com/stealthrocket/net v0.2.1/go.mod h1:VvoFod9pYC9mo+bEg2NQB/D+KVOjxfhZjZ5zyvozq7M=
github.com/stealthrocket/wazergo v0.19.1 h1:BPrITETPgSFwiytwmToO0MbUC/+RGC39JScz1JmmG6c=
github.com/stealthrocket/wazergo v0.19.1/go.mod h1:riI0hxw4ndZA5e6z7PesHg2BtTftcZaMxRcoiGGipTs=
github.com/yosida95/uritemplate/v3 v3.0.2 h1:Ed3Oyj9yrmi9087+NczuL5BwkIc4wvTb5zIM+UJPGz4=
github.com/yosida95/uritemplate/v3 v3.0.2/go.mod h1:ILOh0sOhIJR3+L/8afwt/kE++YT040gmv5BQTMR2HP4=
`
