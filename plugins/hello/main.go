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

	// Make an HTTP call to test if this works with WASI
	log.Printf("Making HTTP call for user: %s", name)

	// Create HTTP client with timeout
	// Force HTTP/1.1 - WASM runtime doesn't support HTTP/2 parsing
	// Clone default transport to preserve stealthrocket/net WASM patches
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		NextProtos: []string{"http/1.1"},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	// Make a simple HTTP GET request
	resp, err := client.Get("https://httpbin.org/get")
	if err != nil {
		log.Printf("HTTP call failed: %v", err)
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("HTTP call failed: %v", err)}},
		}, nil
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body: %v", err)
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to read response body: %v", err)}},
		}, nil
	}

	log.Printf("HTTP call successful, status: %d, body length: %d", resp.StatusCode, len(body))

	message := fmt.Sprintf("Hello, %s! Welcome to the Hello World MCP Server! 🎉 (HTTP call successful: %d)", name, resp.StatusCode)
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}, nil
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
	transport.TLSClientConfig = &tls.Config{
		NextProtos: []string{"http/1.1"},
	}
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
