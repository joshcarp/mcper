//go:build wasip1

// MCPER Chrome Control Plugin
//
// This WASM plugin provides MCP tools for controlling Chrome browser tabs.
// It communicates with the Chrome extension via the native messaging host
// HTTP server running on localhost:9223.

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/breml/rootcerts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "github.com/stealthrocket/net/http"
	_ "github.com/stealthrocket/net/wasip1"
)

const (
	defaultBridgeURL = "http://localhost:9223"
)

var bridgeURL string

func init() {
	bridgeURL = os.Getenv("MCPER_CHROME_BRIDGE_URL")
	if bridgeURL == "" {
		bridgeURL = defaultBridgeURL
	}
}

func main() {
	server := mcp.NewServer("Chrome Control MCP Server", "1.0.0", nil)

	// Navigation tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_navigate",
		Description: "Navigate the browser to a URL. Waits for page load to complete.",
	}, navigateHandler)

	// Interaction tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_click",
		Description: "Click on an element matching the CSS selector.",
	}, clickHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_type",
		Description: "Type text into an input element matching the CSS selector.",
	}, typeHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_scroll",
		Description: "Scroll the page by x,y pixels or scroll an element into view.",
	}, scrollHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_wait",
		Description: "Wait for an element matching the CSS selector to appear.",
	}, waitHandler)

	// Content extraction tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_screenshot",
		Description: "Take a screenshot of the current tab. Returns base64 encoded image.",
	}, screenshotHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_get_html",
		Description: "Get the HTML content of the page or a specific element.",
	}, getHtmlHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_get_text",
		Description: "Get the text content of the page or a specific element.",
	}, getTextHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_evaluate",
		Description: "Execute JavaScript code in the page context and return the result.",
	}, evaluateHandler)

	// Tab management tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_list_tabs",
		Description: "List all open browser tabs with their IDs, URLs, and titles.",
	}, listTabsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_switch_tab",
		Description: "Switch to a tab by its ID.",
	}, switchTabHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_new_tab",
		Description: "Open a new browser tab, optionally navigating to a URL.",
	}, newTabHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_close_tab",
		Description: "Close a browser tab by its ID, or close the current tab.",
	}, closeTabHandler)

	// Advanced tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "chrome_cdp",
		Description: "Send a raw Chrome DevTools Protocol command. For advanced automation.",
	}, cdpHandler)

	log.Println("Starting Chrome Control MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

// HTTP client that forces HTTP/1.1 (required for WASM)
func createHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}

// sendCommand sends a command to the Chrome bridge and returns the response
func sendCommand(command string, params interface{}) (map[string]interface{}, error) {
	client := createHTTPClient()

	payload := map[string]interface{}{
		"command": command,
		"params":  params,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := client.Post(bridgeURL+"/command", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Chrome bridge at %s: %w", bridgeURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		return nil, fmt.Errorf("chrome bridge error: %s", errMsg)
	}

	return result, nil
}

// Tool parameter structs

type NavigateParams struct {
	URL       string `json:"url"`
	TabID     int    `json:"tab_id,omitempty"`
	WaitUntil string `json:"wait_until,omitempty"` // load, domcontentloaded, networkidle
}

type ClickParams struct {
	Selector string `json:"selector"`
	TabID    int    `json:"tab_id,omitempty"`
	Button   string `json:"button,omitempty"` // left, right, middle
}

type TypeParams struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
	TabID    int    `json:"tab_id,omitempty"`
	Delay    int    `json:"delay,omitempty"` // ms between keystrokes
}

type ScrollParams struct {
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	Selector string `json:"selector,omitempty"` // scroll element into view
	TabID    int    `json:"tab_id,omitempty"`
}

type WaitParams struct {
	Selector string `json:"selector"`
	Timeout  int    `json:"timeout,omitempty"` // ms, default 10000
	State    string `json:"state,omitempty"`   // visible, hidden, attached
	TabID    int    `json:"tab_id,omitempty"`
}

type ScreenshotParams struct {
	TabID    int    `json:"tab_id,omitempty"`
	Selector string `json:"selector,omitempty"` // screenshot specific element
	FullPage bool   `json:"full_page,omitempty"`
	Format   string `json:"format,omitempty"` // png, jpeg
}

type GetHtmlParams struct {
	Selector string `json:"selector,omitempty"` // if empty, returns full page
	TabID    int    `json:"tab_id,omitempty"`
}

type GetTextParams struct {
	Selector string `json:"selector,omitempty"`
	TabID    int    `json:"tab_id,omitempty"`
}

type EvaluateParams struct {
	Script string `json:"script"`
	TabID  int    `json:"tab_id,omitempty"`
}

type TabIDParams struct {
	TabID int `json:"tab_id"`
}

type NewTabParams struct {
	URL string `json:"url,omitempty"`
}

type CDPParams struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
	TabID  int                    `json:"tab_id,omitempty"`
}

// Tool handlers

func navigateHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[NavigateParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.URL == "" {
		return errorResult("url is required")
	}

	result, err := sendCommand("navigate", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("Navigated to %s (tab: %v)", params.Arguments.URL, result["tabId"]))
}

func clickHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ClickParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.Selector == "" {
		return errorResult("selector is required")
	}

	result, err := sendCommand("click", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("Clicked element: %s (result: %v)", params.Arguments.Selector, result["success"]))
}

func typeHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[TypeParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.Selector == "" {
		return errorResult("selector is required")
	}
	if params.Arguments.Text == "" {
		return errorResult("text is required")
	}

	result, err := sendCommand("type", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("Typed into %s: %q (result: %v)", params.Arguments.Selector, params.Arguments.Text, result["success"]))
}

func scrollHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ScrollParams]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("scroll", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	if params.Arguments.Selector != "" {
		return successResult(fmt.Sprintf("Scrolled element into view: %s", params.Arguments.Selector))
	}
	return successResult(fmt.Sprintf("Scrolled by (%d, %d) (result: %v)", params.Arguments.X, params.Arguments.Y, result))
}

func waitHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[WaitParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.Selector == "" {
		return errorResult("selector is required")
	}

	result, err := sendCommand("wait", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("Element found: %s (state: %s, result: %v)", params.Arguments.Selector, params.Arguments.State, result["success"]))
}

func screenshotHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ScreenshotParams]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("screenshot", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	dataUrl, ok := result["dataUrl"].(string)
	if !ok {
		return errorResult("no screenshot data received")
	}

	// Return just metadata, as the full base64 would be huge
	format := params.Arguments.Format
	if format == "" {
		format = "png"
	}

	return successResult(fmt.Sprintf("Screenshot captured (%s format, %d bytes base64 data)\n\nData URL: %s", format, len(dataUrl), dataUrl))
}

func getHtmlHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetHtmlParams]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("get_html", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	html, ok := result["html"].(string)
	if !ok {
		return errorResult("no HTML content received")
	}

	// Truncate if too long
	if len(html) > 50000 {
		html = html[:50000] + "\n\n... (truncated, " + fmt.Sprintf("%d", len(html)) + " total bytes)"
	}

	return successResult(html)
}

func getTextHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetTextParams]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("get_text", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	text, ok := result["text"].(string)
	if !ok {
		return errorResult("no text content received")
	}

	// Truncate if too long
	if len(text) > 20000 {
		text = text[:20000] + "\n\n... (truncated, " + fmt.Sprintf("%d", len(text)) + " total bytes)"
	}

	return successResult(text)
}

func evaluateHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[EvaluateParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.Script == "" {
		return errorResult("script is required")
	}

	result, err := sendCommand("evaluate", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	evalResult, _ := result["result"].(string)
	return successResult(fmt.Sprintf("Script executed. Result: %s", evalResult))
}

func listTabsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[struct{}]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("list_tabs", nil)
	if err != nil {
		return errorResult(err.Error())
	}

	tabs, ok := result["tabs"].([]interface{})
	if !ok {
		return errorResult("failed to parse tabs list")
	}

	var output string
	for _, t := range tabs {
		tab, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		active := ""
		if isActive, ok := tab["active"].(bool); ok && isActive {
			active = " [ACTIVE]"
		}
		output += fmt.Sprintf("- Tab %v%s: %v\n  URL: %v\n", tab["id"], active, tab["title"], tab["url"])
	}

	if output == "" {
		output = "No tabs found"
	}

	return successResult(output)
}

func switchTabHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[TabIDParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.TabID == 0 {
		return errorResult("tab_id is required")
	}

	result, err := sendCommand("switch_tab", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("Switched to tab %d (result: %v)", params.Arguments.TabID, result["success"]))
}

func newTabHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[NewTabParams]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("new_tab", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("New tab created (ID: %v, URL: %v)", result["tabId"], result["url"]))
}

func closeTabHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[TabIDParams]) (*mcp.CallToolResultFor[any], error) {
	result, err := sendCommand("close_tab", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	return successResult(fmt.Sprintf("Tab closed (ID: %v)", result["tabId"]))
}

func cdpHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[CDPParams]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.Method == "" {
		return errorResult("method is required")
	}

	result, err := sendCommand("cdp", params.Arguments)
	if err != nil {
		return errorResult(err.Error())
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return successResult(fmt.Sprintf("CDP command executed: %s\n\nResult:\n%s", params.Arguments.Method, string(resultJSON)))
}

// Helper functions

func successResult(message string) (*mcp.CallToolResultFor[any], error) {
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}, nil
}

func errorResult(message string) (*mcp.CallToolResultFor[any], error) {
	return &mcp.CallToolResultFor[any]{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}, nil
}
