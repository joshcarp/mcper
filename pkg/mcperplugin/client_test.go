//go:build !wasip1

package mcperplugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyRewriteTransport(t *testing.T) {
	var seen *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	info := CapInfo{
		Cap:          "test-cap-xyz",
		InvocationID: "inv-1",
		ProxyURL:     upstream.URL,
	}
	client := NewProxyAwareClient("api.github.com", info)

	resp, err := client.Get("https://api.github.com/repos/foo/bar")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" {
		t.Errorf("body = %q", body)
	}

	if seen == nil {
		t.Fatal("upstream never received a request")
	}
	if !strings.HasPrefix(seen.URL.Path, "/api.github.com/repos/foo/bar") {
		t.Errorf("upstream path = %q, want /<host>/<path> prefix", seen.URL.Path)
	}
	if seen.Header.Get("X-MCPER-Cap") != "test-cap-xyz" {
		t.Errorf("X-MCPER-Cap missing: %q", seen.Header.Get("X-MCPER-Cap"))
	}
	if seen.Header.Get("Authorization") != "" {
		t.Errorf("Authorization should be stripped: %q", seen.Header.Get("Authorization"))
	}
}

func TestProxyRewriteRefusesOtherHost(t *testing.T) {
	info := CapInfo{
		Cap:      "test-cap",
		ProxyURL: "https://cloud.example/proxy_v2",
	}
	client := NewProxyAwareClient("api.github.com", info)
	_, err := client.Get("https://evil.example/anything")
	if err == nil {
		t.Error("expected error for cross-host request")
	}
}

func TestEmptyCapPassthrough(t *testing.T) {
	info := CapInfo{}
	client := NewProxyAwareClient("api.github.com", info)
	if client != http.DefaultClient {
		t.Error("empty cap should return http.DefaultClient (passthrough)")
	}
}
