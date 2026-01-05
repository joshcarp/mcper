// MCPER Chrome Bridge - Headless Chrome Controller
//
// This binary provides an HTTP server that controls Chrome via CDP (Chrome DevTools Protocol).
// No Chrome extension required - it launches and controls Chrome directly.
//
// Usage:
//   chrome-bridge -server                    # Run HTTP server, launch headless Chrome
//   chrome-bridge -server -port 8080         # Custom HTTP port
//   chrome-bridge -server -headless=false    # Run with visible Chrome window
//   chrome-bridge -server -chrome-path=/path # Custom Chrome path

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Bridge manages Chrome browser and handles HTTP requests
type Bridge struct {
	mu           sync.Mutex
	allocCtx     context.Context
	allocCancel  context.CancelFunc
	browserCtx   context.Context
	browserCancel context.CancelFunc
	headless     bool
	chromePath   string
}

func main() {
	port := flag.Int("port", 9223, "HTTP server port")
	headless := flag.Bool("headless", true, "Run Chrome in headless mode")
	chromePath := flag.String("chrome-path", "", "Path to Chrome executable (auto-detect if empty)")
	flag.Bool("server", true, "Run as HTTP server (default, kept for compatibility)")
	flag.Parse()

	bridge := &Bridge{
		headless:   *headless,
		chromePath: *chromePath,
	}

	// Start Chrome
	if err := bridge.startChrome(); err != nil {
		log.Fatalf("Failed to start Chrome: %v", err)
	}
	defer bridge.stopChrome()

	// Set up HTTP server
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"headless": bridge.headless,
		})
	})

	// Command endpoints
	mux.HandleFunc("/command", bridge.handleCommand)
	mux.HandleFunc("/navigate", bridge.handleNavigate)
	mux.HandleFunc("/click", bridge.handleClick)
	mux.HandleFunc("/type", bridge.handleType)
	mux.HandleFunc("/screenshot", bridge.handleScreenshot)
	mux.HandleFunc("/html", bridge.handleGetHTML)
	mux.HandleFunc("/text", bridge.handleGetText)
	mux.HandleFunc("/evaluate", bridge.handleEvaluate)
	mux.HandleFunc("/tabs", bridge.handleListTabs)
	mux.HandleFunc("/tabs/new", bridge.handleNewTab)
	mux.HandleFunc("/tabs/close", bridge.handleCloseTab)
	mux.HandleFunc("/scroll", bridge.handleScroll)
	mux.HandleFunc("/wait", bridge.handleWait)

	handler := corsMiddleware(mux)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: handler,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	mode := "headless"
	if !*headless {
		mode = "visible"
	}
	log.Printf("MCPER Chrome Bridge starting on port %d (Chrome: %s)", *port, mode)
	log.Printf("No extension required - controlling Chrome directly via CDP")

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func (b *Bridge) startChrome() error {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	if b.headless {
		opts = append(opts, chromedp.Headless)
	}

	if b.chromePath != "" {
		opts = append(opts, chromedp.ExecPath(b.chromePath))
	}

	b.allocCtx, b.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	b.browserCtx, b.browserCancel = chromedp.NewContext(b.allocCtx, chromedp.WithLogf(log.Printf))

	// Start browser by running a simple action
	if err := chromedp.Run(b.browserCtx); err != nil {
		return fmt.Errorf("failed to start Chrome: %w", err)
	}

	log.Println("Chrome started successfully")
	return nil
}

func (b *Bridge) stopChrome() {
	if b.browserCancel != nil {
		b.browserCancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
	log.Println("Chrome stopped")
}

func (b *Bridge) getContext() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.browserCtx
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Command string          `json:"command"`
		Params  json.RawMessage `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Route to appropriate handler
	switch req.Command {
	case "navigate":
		b.doNavigate(w, req.Params)
	case "click":
		b.doClick(w, req.Params)
	case "type":
		b.doType(w, req.Params)
	case "screenshot":
		b.doScreenshot(w, req.Params)
	case "get_html":
		b.doGetHTML(w, req.Params)
	case "get_text":
		b.doGetText(w, req.Params)
	case "evaluate":
		b.doEvaluate(w, req.Params)
	case "list_tabs":
		b.doListTabs(w)
	case "new_tab":
		b.doNewTab(w, req.Params)
	case "close_tab":
		b.doCloseTab(w, req.Params)
	case "scroll":
		b.doScroll(w, req.Params)
	case "wait":
		b.doWait(w, req.Params)
	case "ping":
		jsonResponse(w, map[string]interface{}{"success": true, "message": "pong"})
	default:
		jsonError(w, fmt.Sprintf("Unknown command: %s", req.Command), http.StatusBadRequest)
	}
}

// Navigate to URL
func (b *Bridge) handleNavigate(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doNavigate(w, params)
}

func (b *Bridge) doNavigate(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		jsonError(w, "Invalid params", http.StatusBadRequest)
		return
	}

	if p.URL == "" {
		jsonError(w, "url is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(b.getContext(), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(p.URL)); err != nil {
		jsonError(w, fmt.Sprintf("Navigation failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "url": p.URL})
}

// Click element
func (b *Bridge) handleClick(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doClick(w, params)
}

func (b *Bridge) doClick(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		jsonError(w, "Invalid params", http.StatusBadRequest)
		return
	}

	if p.Selector == "" {
		jsonError(w, "selector is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Click(p.Selector, chromedp.ByQuery)); err != nil {
		jsonError(w, fmt.Sprintf("Click failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "selector": p.Selector})
}

// Type text
func (b *Bridge) handleType(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doType(w, params)
}

func (b *Bridge) doType(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Clear    bool   `json:"clear"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		jsonError(w, "Invalid params", http.StatusBadRequest)
		return
	}

	if p.Selector == "" || p.Text == "" {
		jsonError(w, "selector and text are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	actions := []chromedp.Action{
		chromedp.Click(p.Selector, chromedp.ByQuery),
	}

	if p.Clear {
		actions = append(actions, chromedp.Clear(p.Selector, chromedp.ByQuery))
	}

	actions = append(actions, chromedp.SendKeys(p.Selector, p.Text, chromedp.ByQuery))

	if err := chromedp.Run(ctx, actions...); err != nil {
		jsonError(w, fmt.Sprintf("Type failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "selector": p.Selector})
}

// Take screenshot
func (b *Bridge) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		params = []byte("{}")
	}
	b.doScreenshot(w, params)
}

func (b *Bridge) doScreenshot(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Selector string `json:"selector"`
		FullPage bool   `json:"full_page"`
	}
	json.Unmarshal(params, &p)

	ctx, cancel := context.WithTimeout(b.getContext(), 30*time.Second)
	defer cancel()

	var buf []byte
	var action chromedp.Action

	if p.Selector != "" {
		action = chromedp.Screenshot(p.Selector, &buf, chromedp.ByQuery)
	} else if p.FullPage {
		action = chromedp.FullScreenshot(&buf, 90)
	} else {
		action = chromedp.CaptureScreenshot(&buf)
	}

	if err := chromedp.Run(ctx, action); err != nil {
		jsonError(w, fmt.Sprintf("Screenshot failed: %v", err), http.StatusInternalServerError)
		return
	}

	dataUrl := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf)
	jsonResponse(w, map[string]interface{}{
		"success": true,
		"dataUrl": dataUrl,
		"format":  "png",
		"size":    len(buf),
	})
}

// Get HTML
func (b *Bridge) handleGetHTML(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		params = []byte("{}")
	}
	b.doGetHTML(w, params)
}

func (b *Bridge) doGetHTML(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Selector string `json:"selector"`
	}
	json.Unmarshal(params, &p)

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	var html string
	var action chromedp.Action

	if p.Selector != "" {
		action = chromedp.OuterHTML(p.Selector, &html, chromedp.ByQuery)
	} else {
		action = chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			html, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			return err
		})
	}

	if err := chromedp.Run(ctx, action); err != nil {
		jsonError(w, fmt.Sprintf("GetHTML failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "html": html})
}

// Get text content
func (b *Bridge) handleGetText(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		params = []byte("{}")
	}
	b.doGetText(w, params)
}

func (b *Bridge) doGetText(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Selector string `json:"selector"`
	}
	json.Unmarshal(params, &p)

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	var text string
	selector := p.Selector
	if selector == "" {
		selector = "body"
	}

	if err := chromedp.Run(ctx, chromedp.Text(selector, &text, chromedp.ByQuery)); err != nil {
		jsonError(w, fmt.Sprintf("GetText failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "text": text})
}

// Evaluate JavaScript
func (b *Bridge) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doEvaluate(w, params)
}

func (b *Bridge) doEvaluate(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Script string `json:"script"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		jsonError(w, "Invalid params", http.StatusBadRequest)
		return
	}

	if p.Script == "" {
		jsonError(w, "script is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(b.getContext(), 30*time.Second)
	defer cancel()

	var result interface{}
	if err := chromedp.Run(ctx, chromedp.Evaluate(p.Script, &result)); err != nil {
		jsonError(w, fmt.Sprintf("Evaluate failed: %v", err), http.StatusInternalServerError)
		return
	}

	resultJSON, _ := json.Marshal(result)
	jsonResponse(w, map[string]interface{}{"success": true, "result": string(resultJSON)})
}

// List tabs
func (b *Bridge) handleListTabs(w http.ResponseWriter, r *http.Request) {
	b.doListTabs(w)
}

func (b *Bridge) doListTabs(w http.ResponseWriter) {
	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	var targets []*target.Info
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targets, err = target.GetTargets().Do(ctx)
		return err
	})); err != nil {
		jsonError(w, fmt.Sprintf("ListTabs failed: %v", err), http.StatusInternalServerError)
		return
	}

	tabs := make([]map[string]interface{}, 0)
	for _, t := range targets {
		if t.Type == "page" {
			tabs = append(tabs, map[string]interface{}{
				"id":    t.TargetID,
				"url":   t.URL,
				"title": t.Title,
			})
		}
	}

	jsonResponse(w, map[string]interface{}{"success": true, "tabs": tabs})
}

// New tab
func (b *Bridge) handleNewTab(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		params = []byte("{}")
	}
	b.doNewTab(w, params)
}

func (b *Bridge) doNewTab(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		URL string `json:"url"`
	}
	json.Unmarshal(params, &p)

	if p.URL == "" {
		p.URL = "about:blank"
	}

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	var targetID target.ID
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targetID, err = target.CreateTarget(p.URL).Do(ctx)
		return err
	})); err != nil {
		jsonError(w, fmt.Sprintf("NewTab failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "tabId": targetID, "url": p.URL})
}

// Close tab
func (b *Bridge) handleCloseTab(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doCloseTab(w, params)
}

func (b *Bridge) doCloseTab(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		TabID string `json:"tab_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		jsonError(w, "Invalid params", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return target.CloseTarget(target.ID(p.TabID)).Do(ctx)
	})); err != nil {
		jsonError(w, fmt.Sprintf("CloseTab failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "tabId": p.TabID})
}

// Scroll
func (b *Bridge) handleScroll(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doScroll(w, params)
}

func (b *Bridge) doScroll(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		X        int    `json:"x"`
		Y        int    `json:"y"`
		Selector string `json:"selector"`
	}
	json.Unmarshal(params, &p)

	ctx, cancel := context.WithTimeout(b.getContext(), 10*time.Second)
	defer cancel()

	var script string
	if p.Selector != "" {
		script = fmt.Sprintf(`document.querySelector(%q).scrollIntoView({behavior: 'smooth', block: 'center'})`, p.Selector)
	} else {
		script = fmt.Sprintf(`window.scrollBy(%d, %d)`, p.X, p.Y)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
		jsonError(w, fmt.Sprintf("Scroll failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true})
}

// Wait for element
func (b *Bridge) handleWait(w http.ResponseWriter, r *http.Request) {
	params, err := getParams(r)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.doWait(w, params)
}

func (b *Bridge) doWait(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Selector string `json:"selector"`
		Timeout  int    `json:"timeout"` // milliseconds
		State    string `json:"state"`   // visible, hidden, attached
	}
	if err := json.Unmarshal(params, &p); err != nil {
		jsonError(w, "Invalid params", http.StatusBadRequest)
		return
	}

	if p.Selector == "" {
		jsonError(w, "selector is required", http.StatusBadRequest)
		return
	}

	timeout := 10 * time.Second
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(b.getContext(), timeout)
	defer cancel()

	var action chromedp.Action
	switch p.State {
	case "hidden":
		action = chromedp.WaitNotPresent(p.Selector, chromedp.ByQuery)
	case "visible":
		action = chromedp.WaitVisible(p.Selector, chromedp.ByQuery)
	default:
		action = chromedp.WaitReady(p.Selector, chromedp.ByQuery)
	}

	if err := chromedp.Run(ctx, action); err != nil {
		jsonError(w, fmt.Sprintf("Wait failed: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true, "selector": p.Selector, "state": p.State})
}

// Helper functions

func getParams(r *http.Request) (json.RawMessage, error) {
	if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if len(body) > 0 {
			return body, nil
		}
	}

	// Convert query params to JSON for GET requests
	params := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	if len(params) > 0 {
		return json.Marshal(params)
	}
	return []byte("{}"), nil
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": message})
}

// Unused but kept for compatibility - page action helpers
var _ = runtime.Evaluate
var _ = page.CaptureScreenshot
