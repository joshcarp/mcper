// Package mcperplugin — PR 6: plugin-side helpers for cap-aware HTTP.
//
// Plugin guests import this to read the cap + invocation_id + proxy_url
// from MCP _meta and build an HTTP client that:
//   - rewrites https://<targetHost>/... → <proxyURL>/<targetHost>/...
//   - attaches X-MCPER-Cap header
//   - keeps a fresh client per CallToolRequest (no package-level retention)
//
// Plugins MUST construct clients per CallToolRequest. Package-level
// retention would let one tool call's cap leak into the next (different
// invocation_id, possibly different user). The linter rule
// `mcperplugin/lint` flags package-level usage.

package mcperplugin

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CapInfo carries the per-invocation cap context.
type CapInfo struct {
	Cap          string
	InvocationID string
	ProxyURL     string
}

// CapFromRequest extracts cap/invocation_id/proxy_url from MCP _meta.
// Returns (zero, false) if any field is missing — caller may then fall
// back to legacy direct HTTP (when MCPER_USE_CAP_PROXY is off CLI-side)
// or refuse the call (compile-time enforcement mode).
func CapFromRequest(req *mcp.CallToolRequest) (CapInfo, bool) {
	if req == nil || req.Params == nil || req.Params.Meta == nil {
		return CapInfo{}, false
	}
	get := func(k string) string {
		v, ok := req.Params.Meta[k]
		if !ok {
			return ""
		}
		s, _ := v.(string)
		return s
	}
	c := CapInfo{
		Cap:          get("mcper_cap"),
		InvocationID: get("mcper_invocation_id"),
		ProxyURL:     get("mcper_proxy_url"),
	}
	if c.Cap == "" || c.ProxyURL == "" {
		return CapInfo{}, false
	}
	return c, true
}

// NewProxyAwareClient returns an *http.Client that rewrites all requests
// targeting `targetHost` to go through the mcper-cloud /proxy_v2
// endpoint with the cap attached.
//
// `cap` and `proxyURL` come from CapFromRequest. If both are empty, the
// helper returns a passthrough http.DefaultClient (legacy mode); plugins
// that REQUIRE cap-proxy enforcement should refuse to call this with
// empty inputs.
func NewProxyAwareClient(targetHost string, info CapInfo) *http.Client {
	if info.Cap == "" || info.ProxyURL == "" {
		return http.DefaultClient
	}
	return &http.Client{
		Transport: &proxyRewriteTransport{
			base:       http.DefaultTransport,
			targetHost: strings.ToLower(targetHost),
			proxyURL:   strings.TrimRight(info.ProxyURL, "/"),
			cap:        info.Cap,
		},
	}
}

type proxyRewriteTransport struct {
	base       http.RoundTripper
	targetHost string
	proxyURL   string
	cap        string
}

func (t *proxyRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only rewrite requests targeting our declared host.
	if strings.ToLower(req.URL.Host) != t.targetHost {
		return nil, fmt.Errorf("mcperplugin: request to %s not allowed (only %s)", req.URL.Host, t.targetHost)
	}
	// Rewrite to <proxyURL>/<host><path>?<query>.
	newURL := fmt.Sprintf("%s/%s%s", t.proxyURL, req.URL.Host, req.URL.EscapedPath())
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	parsed, err := url.Parse(newURL)
	if err != nil {
		return nil, fmt.Errorf("mcperplugin: rewrite URL invalid: %w", err)
	}
	out := req.Clone(req.Context())
	out.URL = parsed
	out.Host = ""
	out.Header.Set("X-MCPER-Cap", t.cap)
	// Don't leak the plugin's intention to provide its own Authorization;
	// /proxy strips it, but stripping client-side first prevents
	// accidental logging.
	out.Header.Del("Authorization")
	return t.base.RoundTrip(out)
}
