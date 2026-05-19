// Package mcper — PR 1 (CLI side): plugin manifest v2 parser.
//
// Both repos share Go module path `github.com/joshcarp/mcper` so direct
// Go imports are impossible. Manifests are the contract — both repos
// parse the same JSON bytes; CI runs `testdata/manifest-fixtures/*.json`
// through both parsers and asserts identical canonical results.

package mcper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PluginInfoV2 mirrors mcper-cloud/pkg/cap.PluginInfoV2. Both parsers
// must agree on the shared fixtures.
type PluginInfoV2 struct {
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	Version       string       `json:"version,omitempty"`
	Author        string       `json:"author,omitempty"`
	Source        string       `json:"source,omitempty"`
	Env           []string     `json:"env,omitempty"`
	OAuthProvider string       `json:"oauth_provider,omitempty"`
	Egress        []EgressDecl `json:"egress,omitempty"`
	Tools         []ToolDecl   `json:"tools,omitempty"`
	SDKVersion    string       `json:"sdk_version,omitempty"`
}

// EgressDecl is one entry in a plugin's static egress allowlist.
type EgressDecl struct {
	Host       string   `json:"host"`
	PathPrefix string   `json:"path_prefix,omitempty"`
	Methods    []string `json:"methods,omitempty"`
}

// ToolDecl describes one tool inside a v2 plugin manifest.
type ToolDecl struct {
	Name           string       `json:"name"`
	Description    string       `json:"description,omitempty"`
	Egress         []EgressDecl `json:"egress,omitempty"`
	ApprovalMode   string       `json:"approval_mode,omitempty"` // "allow" | "pre" | "deny"
	MaxEgressCalls *int         `json:"max_egress_calls,omitempty"`
	Streaming      bool         `json:"streaming,omitempty"`
}

// ParseManifestV2 deserialises raw manifest bytes into PluginInfoV2.
// Rejects approval_mode="ask" (reserved for future deferred-approval flow).
// Applies the same egress validity rules as mcper-cloud's ParseManifest so
// the cross-repo contract is symmetric — a manifest that parses cleanly on
// the CLI side also parses cleanly on the cloud side (and vice versa).
func ParseManifestV2(raw []byte) (*PluginInfoV2, error) {
	var pi PluginInfoV2
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pi); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if pi.Name == "" {
		return nil, fmt.Errorf("manifest: missing name")
	}
	for i, e := range pi.Egress {
		if err := validateEgress(e); err != nil {
			return nil, fmt.Errorf("manifest: egress[%d]: %w", i, err)
		}
	}
	for i, t := range pi.Tools {
		if t.Name == "" {
			return nil, fmt.Errorf("manifest: tool[%d] missing name", i)
		}
		switch t.ApprovalMode {
		case "", "allow", "pre", "deny":
		case "ask":
			return nil, fmt.Errorf("manifest: tool %q approval_mode=ask is reserved (not in v1)", t.Name)
		default:
			return nil, fmt.Errorf("manifest: tool %q approval_mode %q invalid", t.Name, t.ApprovalMode)
		}
		for j, e := range t.Egress {
			if err := validateEgress(e); err != nil {
				return nil, fmt.Errorf("manifest: tool %q egress[%d]: %w", t.Name, j, err)
			}
		}
	}
	return &pi, nil
}

// validateEgress mirrors mcper-cloud/pkg/cap/manifest.go:validateEgress.
// Any drift breaks the cross-repo contract.
func validateEgress(e EgressDecl) error {
	if e.Host == "" {
		return fmt.Errorf("host required")
	}
	if strings.Contains(e.Host, "@") {
		return fmt.Errorf("host must not contain userinfo")
	}
	if strings.Contains(e.Host, ":") {
		return fmt.Errorf("host must not contain port")
	}
	if e.Host != strings.ToLower(e.Host) {
		return fmt.Errorf("host must be lowercase")
	}
	if strings.ContainsAny(e.Host, "*?[]{}\\") {
		return fmt.Errorf("host wildcards not supported in v1")
	}
	for _, m := range e.Methods {
		switch m {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			return fmt.Errorf("method %q invalid (uppercase HTTP verbs only)", m)
		}
	}
	return nil
}

// HashRawManifest returns "sha256:<hex>" of raw manifest bytes. Hash
// is computed over RAW GCS object bytes, NEVER re-serialised content
// — both repos must hash identical bytes for cloud's manifest_stale
// check to agree.
func HashRawManifest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// FindTool returns the ToolDecl for `toolName`, or nil if absent.
func (pi *PluginInfoV2) FindTool(toolName string) *ToolDecl {
	for i := range pi.Tools {
		if pi.Tools[i].Name == toolName {
			return &pi.Tools[i]
		}
	}
	return nil
}

// FetchedManifest bundles a parsed PluginInfoV2 with the raw bytes that were
// hashed. The cap-proxy path needs all three (parsed for policy lookup, raw
// for cross-repo agreement, hash for the cap-mint request).
type FetchedManifest struct {
	Manifest *PluginInfoV2
	Raw      []byte
	Hash     string
}

// FetchManifestV2 GETs the plugin's manifest.json from `url`, parses it, and
// returns the bundle. Returns nil on any failure (404, network, parse) so
// callers can fall back to legacy without distinguishing causes — failure
// here just means cap-proxy isn't yet available for this plugin.
func FetchManifestV2(ctx context.Context, url string) (*FetchedManifest, error) {
	if url == "" {
		return nil, fmt.Errorf("manifest: empty url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest fetch: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap at 1 MiB
	if err != nil {
		return nil, fmt.Errorf("manifest read: %w", err)
	}
	parsed, err := ParseManifestV2(raw)
	if err != nil {
		return nil, fmt.Errorf("manifest parse: %w", err)
	}
	return &FetchedManifest{
		Manifest: parsed,
		Raw:      raw,
		Hash:     HashRawManifest(raw),
	}, nil
}
