package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestBridgeProxiesPassthrough drives the bridge end-to-end against a fake
// upstream and asserts that the JSON-RPC request hits the upstream verbatim
// and the response makes it back to stdout.
func TestBridgeProxiesPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"method":"tools/list"`) {
			t.Errorf("upstream got unexpected body: %s", body)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing Content-Type")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"echo"}]}}`)
	}))
	defer upstream.Close()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
	var out bytes.Buffer

	b := newTestBridge(upstream.URL)
	if err := b.run(context.Background(), in, &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}

	got := strings.TrimSpace(out.String())
	if !strings.Contains(got, `"name":"echo"`) {
		t.Errorf("expected upstream tools/list result on stdout, got: %s", got)
	}
}

// TestBridgePreservesApprovalGateError is the headline assertion: when the
// upstream returns a JSON-RPC error response with code -32000 + structured
// data (the cloud's approval-gate signature), the bridge passes the body
// through byte-for-byte. The SDK-based path historically flattened this to a
// stringified Go error, hiding approval_id.
func TestBridgePreservesApprovalGateError(t *testing.T) {
	const upstreamBody = `{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"Approval required.","data":{"approval_id":"abc-123","tool":"github/merge_pr","policy":"ask"}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, upstreamBody)
	}))
	defer upstream.Close()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"github/merge_pr","arguments":{"pr":1}}}` + "\n")
	var out bytes.Buffer

	b := newTestBridge(upstream.URL)
	if err := b.run(context.Background(), in, &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}

	var resp struct {
		Error struct {
			Code int            `json:"code"`
			Data map[string]any `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode bridged response: %v\nbody=%s", err, out.String())
	}
	if resp.Error.Code != -32000 {
		t.Errorf("bridge dropped error code: got %d, want -32000", resp.Error.Code)
	}
	if got, _ := resp.Error.Data["approval_id"].(string); got != "abc-123" {
		t.Errorf("bridge dropped approval_id; got %v, want abc-123", resp.Error.Data["approval_id"])
	}
}

// TestBridgeRetriesTransient5xx asserts the bridge tries again on a 502 and
// succeeds when the upstream recovers — and it does not retry forever.
func TestBridgeRetriesTransient5xx(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			http.Error(w, "transient", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"result":{"ok":true}}`)
	}))
	defer upstream.Close()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	b := newTestBridge(upstream.URL)
	b.retries = 5
	if err := b.run(context.Background(), in, &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("upstream call count = %d, want 3 (2 transient + 1 success)", got)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Errorf("expected recovered response, got %s", out.String())
	}
}

// TestBridgeForwardsStaticHeader sends a custom Authorization header and
// asserts upstream actually receives it on every call.
func TestBridgeForwardsStaticHeader(t *testing.T) {
	var seen atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":3,"result":{}}`)
	}))
	defer upstream.Close()

	b := newTestBridge(upstream.URL)
	b.headers.Set("Authorization", "Bearer secret-xyz")

	in := strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}` + "\n")
	if err := b.run(context.Background(), in, &bytes.Buffer{}); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}
	if got := seen.Load(); got != "Bearer secret-xyz" {
		t.Errorf("upstream Authorization = %q, want Bearer secret-xyz", got)
	}
}

// TestBridgeSyntheticErrorOnNetworkFailure: when the upstream is unreachable,
// the bridge must emit a JSON-RPC error frame on stdout (with the original
// request id) so the client sees a useful failure instead of silence.
func TestBridgeSyntheticErrorOnNetworkFailure(t *testing.T) {
	// Point at an unroutable address.
	b := newTestBridge("http://127.0.0.1:1/mcp")
	b.client = &http.Client{Timeout: 250 * time.Millisecond}
	b.retries = 0

	in := strings.NewReader(`{"jsonrpc":"2.0","id":42,"method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if err := b.run(context.Background(), in, &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}

	var resp struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, out.String())
	}
	if resp.ID != 42 {
		t.Errorf("synthetic error must echo request id 42, got %d", resp.ID)
	}
	if resp.Error.Code != -32000 || !strings.Contains(resp.Error.Message, "mcper bridge:") {
		t.Errorf("expected -32000 + bridge prefix, got code=%d msg=%q", resp.Error.Code, resp.Error.Message)
	}
}

// TestBridgeForwardsSSEEvents simulates a streaming response where the
// upstream emits two `data:` events and asserts both reach stdout as
// separate JSON-RPC frames.
func TestBridgeForwardsSSEEvents(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":0.5}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"done\":true}}\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"long_op"}}` + "\n")
	var out bytes.Buffer
	b := newTestBridge(upstream.URL)
	if err := b.run(context.Background(), in, &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 SSE-derived frames, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], `"progress":0.5`) {
		t.Errorf("first frame missing progress notification: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"done":true`) {
		t.Errorf("second frame missing final result: %s", lines[1])
	}
}

// TestBridgeEchoesSessionID asserts the bridge captures Mcp-Session-Id from
// the first response and replays it on subsequent requests, so a server
// that requires a session sees a stable Mcp-Session-Id across the bridge.
func TestBridgeEchoesSessionID(t *testing.T) {
	var sentBack atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"initialize"`) {
			w.Header().Set("Mcp-Session-Id", "sess-7")
		} else {
			sentBack.Store(r.Header.Get("Mcp-Session-Id"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer upstream.Close()

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n",
	)
	var out bytes.Buffer
	b := newTestBridge(upstream.URL)
	if err := b.run(context.Background(), in, &out); err != nil {
		t.Fatalf("bridge.run: %v", err)
	}
	if got := sentBack.Load(); got != "sess-7" {
		t.Errorf("bridge dropped session id: replayed %v, want sess-7", got)
	}
}

func newTestBridge(upstream string) *bridge {
	h := http.Header{
		"Content-Type": {"application/json"},
		"Accept":       {"application/json, text/event-stream"},
	}
	return &bridge{
		upstream: upstream,
		headers:  h,
		client:   &http.Client{Timeout: 5 * time.Second},
		retries:  0,
		stderr:   io.Discard,
		quiet:    true,
	}
}
