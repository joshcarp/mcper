// `mcper bridge <upstream-url>` — a no-config stdio MCP server that proxies
// every JSON-RPC message to a single upstream HTTP MCP server.
//
// Why this exists: Claude Code's built-in HTTP MCP client has been unreliable
// for some users. Running mcper as a stdio bridge swaps that transport: Claude
// Code only sees stdio (its battle-tested path), and mcper handles HTTP to
// the upstream. We deliberately bypass the MCP SDK on the upstream side so
// JSON-RPC error responses (including the cloud's -32000 approval-gate error
// with `data.approval_id`) round-trip verbatim instead of getting flattened
// into a Go error string.
//
// Limitations of v1:
//   - Server-initiated requests (the GET-side of streamable HTTP for sampling
//     callbacks etc.) are not yet bridged. Most tool-only servers don't use it.
//   - No automatic reconnect/retry. Transient upstream failures surface as a
//     synthetic -32000 error response so the client sees something useful.
//   - Session-ID echo (`Mcp-Session-Id`) is supported on a best-effort basis:
//     we capture it from the first response and replay on subsequent requests.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var (
	bridgeHeaders []string
	bridgeTimeout time.Duration
	bridgeRetries int
	bridgeQuiet   bool
)

var bridgeCmd = &cobra.Command{
	Use:   "bridge <upstream-url>",
	Short: "Stdio MCP proxy in front of one HTTP MCP server",
	Long: `Bridge stdio JSON-RPC to a single upstream HTTP MCP server.

Use this when an MCP host's built-in HTTP transport is unreliable: configure
the host to launch 'mcper bridge <url>' as a stdio command, and the host
only sees stdio while mcper does the HTTP work. JSON-RPC errors and SSE
streams pass through faithfully.

Examples:
  mcper bridge https://api.example.com/mcp

  mcper bridge https://mcper-9161453686.us-central1.run.app/mcp \
    --header "Authorization: Bearer $MCPER_TOKEN"

Add to ~/.claude.json (or .mcp.json) as:
  {
    "mcpServers": {
      "mcper-cloud": {
        "command": "mcper",
        "args": ["bridge", "https://mcper-9161453686.us-central1.run.app/mcp",
                 "--header", "Authorization: Bearer YOUR_MCPER_TOKEN"]
      }
    }
  }`,
	Args: cobra.ExactArgs(1),
	RunE: runBridge,
}

func init() {
	bridgeCmd.Flags().StringArrayVar(&bridgeHeaders, "header", nil,
		`extra HTTP header sent on every upstream request (repeatable, format "K: V")`)
	bridgeCmd.Flags().DurationVar(&bridgeTimeout, "timeout", 5*time.Minute,
		"per-request timeout to the upstream server")
	bridgeCmd.Flags().IntVar(&bridgeRetries, "retries", 1,
		"retry attempts on transient (5xx, network) failures before giving up")
	bridgeCmd.Flags().BoolVar(&bridgeQuiet, "quiet", false,
		"suppress diagnostic messages on stderr (default off)")
}

func runBridge(cmd *cobra.Command, args []string) error {
	upstream := args[0]
	if !strings.HasPrefix(upstream, "http://") && !strings.HasPrefix(upstream, "https://") {
		return fmt.Errorf("upstream URL must start with http:// or https://")
	}

	staticHeaders := make(http.Header)
	staticHeaders.Set("Content-Type", "application/json")
	staticHeaders.Set("Accept", "application/json, text/event-stream")
	for _, h := range bridgeHeaders {
		i := strings.Index(h, ":")
		if i < 0 {
			return fmt.Errorf(`invalid --header %q (expected "Key: Value")`, h)
		}
		staticHeaders.Add(strings.TrimSpace(h[:i]), strings.TrimSpace(h[i+1:]))
	}

	b := &bridge{
		upstream: upstream,
		headers:  staticHeaders,
		client:   &http.Client{Timeout: bridgeTimeout},
		retries:  bridgeRetries,
		stderr:   os.Stderr,
		quiet:    bridgeQuiet,
	}
	return b.run(cmd.Context(), os.Stdin, os.Stdout)
}

type bridge struct {
	upstream string
	headers  http.Header
	client   *http.Client
	retries  int
	stderr   io.Writer
	quiet    bool

	sessMu    sync.Mutex
	sessionID string
}

func (b *bridge) run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	bw := bufio.NewWriter(out)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		// Copy because Bytes() reuses the scanner buffer between iterations.
		body := append([]byte(nil), line...)
		if err := b.proxyOne(ctx, body, bw); err != nil {
			b.diag("proxy: %v", err)
			b.writeSyntheticError(bw, body, err)
		}
	}
	bw.Flush()
	return scanner.Err()
}

// proxyOne POSTs one JSON-RPC frame to the upstream and writes the response
// (or each SSE event) to the output as a newline-delimited frame.
func (b *bridge) proxyOne(ctx context.Context, body []byte, out *bufio.Writer) error {
	var lastErr error
	for attempt := 0; attempt <= b.retries; attempt++ {
		if attempt > 0 {
			// Exponential-ish backoff capped at 2s. Bridge is for interactive
			// MCP traffic — don't make humans wait minutes.
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			b.diag("retry %d/%d", attempt, b.retries)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", b.upstream, bytes.NewReader(body))
		if err != nil {
			return err
		}
		for k, vv := range b.headers {
			req.Header[k] = vv
		}
		if sid := b.session(); sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}

		resp, err := b.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		// Capture session id if upstream issued one.
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			b.setSession(sid)
		}

		// 5xx is retriable; everything else is terminal (success or 4xx-style
		// upstream rejection — we forward the body verbatim if the server
		// followed JSON-RPC and returned an error in the body).
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			lastErr = fmt.Errorf("upstream %d", resp.StatusCode)
			resp.Body.Close()
			continue
		}

		ct := resp.Header.Get("Content-Type")
		switch {
		case strings.Contains(ct, "text/event-stream"):
			err = forwardSSE(resp.Body, out)
			resp.Body.Close()
			if err != nil {
				return err
			}
			return nil
		case resp.StatusCode == http.StatusAccepted:
			// Notification ack with no body; nothing to forward.
			resp.Body.Close()
			return nil
		default:
			// Try JSON. If the body isn't JSON (e.g., plain-text 401
			// "Authentication required"), turn it into an actionable
			// synthetic error that includes the upstream message and
			// status code so the human-readable failure isn't lost.
			err = forwardJSON(resp.Body, out)
			if err != nil {
				// Re-read failed body — but we already consumed it inside
				// forwardJSON. Surface what we know via fmt error so the
				// caller's writeSyntheticError carries the status code.
				resp.Body.Close()
				return fmt.Errorf("upstream %d %s: %v", resp.StatusCode, resp.Status, err)
			}
			resp.Body.Close()
			return nil
		}
	}
	return lastErr
}

// forwardJSON reads a single JSON-RPC response object and writes it as one
// line to out. Body may be empty (notification ack); in that case nothing is
// written. Returns a descriptive error containing the body excerpt when the
// upstream returned non-JSON (e.g., a plain-text 401), so the caller can
// surface the upstream message in the synthetic error frame.
func forwardJSON(body io.Reader, out *bufio.Writer) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil
	}
	// Ensure single line — if upstream returned a pretty-printed body, compact it.
	var compact bytes.Buffer
	if err := json.Compact(&compact, b); err != nil {
		excerpt := string(b)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200] + "…"
		}
		return fmt.Errorf("non-JSON body: %s", excerpt)
	}
	if _, err := out.Write(compact.Bytes()); err != nil {
		return err
	}
	if err := out.WriteByte('\n'); err != nil {
		return err
	}
	return out.Flush()
}

// forwardSSE walks `data:` lines from a text/event-stream response and writes
// each event payload as a newline-delimited JSON-RPC frame.
func forwardSSE(body io.Reader, out *bufio.Writer) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var current strings.Builder
	flush := func() error {
		if current.Len() == 0 {
			return nil
		}
		s := strings.TrimSpace(current.String())
		current.Reset()
		if s == "" {
			return nil
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(s)); err != nil {
			return nil // non-JSON event, ignore
		}
		if _, err := out.Write(compact.Bytes()); err != nil {
			return err
		}
		if err := out.WriteByte('\n'); err != nil {
			return err
		}
		return out.Flush()
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			current.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return scanner.Err()
}

func (b *bridge) session() string {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	return b.sessionID
}

func (b *bridge) setSession(id string) {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	b.sessionID = id
}

func (b *bridge) diag(format string, args ...any) {
	if b.quiet {
		return
	}
	fmt.Fprintf(b.stderr, "[bridge] "+format+"\n", args...)
}

// writeSyntheticError emits a JSON-RPC error response on out so the client
// sees something actionable instead of a hung request. Best-effort id
// extraction so the response correlates with the original call.
func (b *bridge) writeSyntheticError(out *bufio.Writer, originalRequest []byte, err error) {
	id := extractRequestID(originalRequest)
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32000,
			"message": "mcper bridge: " + err.Error(),
		},
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
	out.Flush()
}

func extractRequestID(body []byte) any {
	var msg struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &msg); err != nil || len(msg.ID) == 0 {
		return nil
	}
	var id any
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return nil
	}
	return id
}
