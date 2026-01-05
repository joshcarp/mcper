//go:build wasip1

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/breml/rootcerts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "github.com/stealthrocket/net/http"
	_ "github.com/stealthrocket/net/wasip1"
)

func main() {
	// Create a new MCP server
	server := mcp.NewServer("Hello World MCP Server", "1.0.0", nil)

	// Add the hello world tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "hello_world_wasm",
		Description: "Say hello to someone",
	}, helloHandler)

	// Add network test tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "network_test_wasm",
		Description: "Test various networking capabilities",
	}, networkTestHandler)

	// Start the server
	log.Println("Starting Hello World MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

// HelloParams defines the parameters for hello_world tool
type HelloParams struct {
	Name string `json:"name"`
}

// helloHandler handles the hello_world tool calls
func helloHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[HelloParams]) (*mcp.CallToolResultFor[any], error) {
	name := params.Arguments.Name
	if name == "" {
		name = "World"
	}

	// Test WASM HTTP networking by calling httpbin.org
	httpResult := testHTTPBin()

	message := fmt.Sprintf("Hello, %s! Welcome to the Hello World MCP Server!\n\n%s", name, httpResult)
	log.Printf("Greeting user: %s", name)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}, nil
}

// testHTTPBin tests HTTP connectivity - tries direct first, then proxy fallback
func testHTTPBin() string {
	client := createHTTP11Client()
	targetURL := "https://httpbin.org/get"

	// Try direct HTTP call first
	result, err := tryDirectHTTP(client, targetURL)
	if err == nil {
		return fmt.Sprintf("HTTP Test (Direct): SUCCESS\n%s", result)
	}
	directErr := err

	// Try via proxy if MCPER_PROXY_URL is set
	proxyURL := os.Getenv("MCPER_PROXY_URL")
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTP_PROXY")
	}

	if proxyURL != "" {
		result, err := tryProxyHTTP(client, proxyURL, "httpbin.org/get")
		if err == nil {
			return fmt.Sprintf("HTTP Test (Proxy): SUCCESS\n%s", result)
		}
		return fmt.Sprintf("HTTP Test: FAILED\n- Direct error: %v\n- Proxy error: %v", directErr, err)
	}

	return fmt.Sprintf("HTTP Test (Direct): FAILED\n- Error: %v\n- No proxy configured", directErr)
}

// createHTTP11Client creates an HTTP client that forces HTTP/1.1
func createHTTP11Client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

// tryDirectHTTP attempts a direct HTTP call
func tryDirectHTTP(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("URL: %s\nStatus: %d\nBody: %d bytes", url, resp.StatusCode, len(body)), nil
}

// tryProxyHTTP attempts an HTTP call via the mcper proxy
func tryProxyHTTP(client *http.Client, proxyBaseURL, path string) (string, error) {
	// Build proxy URL: {proxyBaseURL}/{path}
	proxyURL := strings.TrimSuffix(proxyBaseURL, "/") + "/" + strings.TrimPrefix(path, "/")

	resp, err := client.Get(proxyURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Proxy URL: %s\nStatus: %d\nBody: %d bytes", proxyURL, resp.StatusCode, len(body)), nil
}

// NetworkTestParams defines the parameters for network_test tool
type NetworkTestParams struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// networkTestHandler demonstrates various networking capabilities
func networkTestHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[NetworkTestParams]) (*mcp.CallToolResultFor[any], error) {
	testType := params.Arguments.Type
	target := params.Arguments.Target

	if target == "" {
		switch testType {
		case "http":
			target = "https://httpbin.org/get"
		case "tcp", "udp":
			target = "localhost:8080"
		}
	}

	var result string
	var err error

	switch testType {
	case "http":
		result, err = testHTTP(target)
	case "tcp":
		result, err = testTCP(target)
	case "udp":
		result, err = testUDP(target)
	default:
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Unknown test type: %s. Supported types: http, tcp, udp", testType)}},
		}, nil
	}

	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s test failed: %v", testType, err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

func testHTTP(url string) (string, error) {
	// Force HTTP/1.1 - WASM runtime doesn't support HTTP/2 parsing
	// Clone default transport to preserve stealthrocket/net WASM patches
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Disable HTTP/2 by setting TLSNextProto to empty map
	transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("HTTP Test Results:\nURL: %s\nStatus: %d\nBody Length: %d bytes\nHeaders: %v",
		url, resp.StatusCode, len(body), resp.Header), nil
}

func testTCP(addr string) (string, error) {
	// Test TCP connection
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("TCP connection failed: %v", err)
	}
	defer conn.Close()

	// Try to write some data
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: " + addr + "\r\n\r\n"))
	if err != nil {
		return "", fmt.Errorf("TCP write failed: %v", err)
	}

	// Try to read response (with timeout)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		// This is expected if the server doesn't respond
		return fmt.Sprintf("TCP Test Results:\nAddress: %s\nConnection: SUCCESS\nWrite: SUCCESS\nRead: %v (expected for non-HTTP servers)",
			addr, err), nil
	}

	return fmt.Sprintf("TCP Test Results:\nAddress: %s\nConnection: SUCCESS\nWrite: SUCCESS\nRead: SUCCESS (%d bytes)",
		addr, n), nil
}

func testUDP(addr string) (string, error) {
	// Test UDP connection
	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("UDP connection failed: %v", err)
	}
	defer conn.Close()

	// Try to write some data
	_, err = conn.Write([]byte("Hello UDP"))
	if err != nil {
		return "", fmt.Errorf("UDP write failed: %v", err)
	}

	// Try to read response (with timeout)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		// This is expected if the server doesn't respond
		return fmt.Sprintf("UDP Test Results:\nAddress: %s\nConnection: SUCCESS\nWrite: SUCCESS\nRead: %v (expected for non-UDP servers)",
			addr, err), nil
	}

	return fmt.Sprintf("UDP Test Results:\nAddress: %s\nConnection: SUCCESS\nWrite: SUCCESS\nRead: SUCCESS (%d bytes)",
		addr, n), nil
}
