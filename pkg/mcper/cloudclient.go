// Package mcper — PR 5 (CLI side): cap-mint client + log scrubber.
//
// CloudClient talks to /api/cap/mint and /api/cap/mint/poll/:id on
// mcper-cloud, returning either a cap token (success), structured
// error (per cap-mint contract), or a pending-approval handle.

package mcper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Cap is the result of a successful /api/cap/mint or
// /api/cap/mint/poll/:id call.
type Cap struct {
	Cap       string    `json:"cap"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CapMintError matches mcper-cloud's capMintErrorResponse JSON shape.
type CapMintError struct {
	Status        int    `json:"-"`
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
	CloudHash     string `json:"cloud_hash,omitempty"`
	RequestedHash string `json:"requested_hash,omitempty"`
}

func (e *CapMintError) Error() string {
	return fmt.Sprintf("cap mint %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

// IsManifestStale reports whether the error is a manifest_stale 409.
// CLI retries once after re-downloading the plugin; second mismatch
// is terminal per plan.
func IsManifestStale(err error) bool {
	var e *CapMintError
	return errors.As(err, &e) && e.Code == "manifest_stale"
}

// IsPendingApproval is returned in the pending-approval shape; CLI
// polls the embedded URL.
type PendingApproval struct {
	ApprovalID   string `json:"approval_id"`
	InvocationID string `json:"invocation_id"`
	PollURL      string `json:"poll_url"`
	Tier         string `json:"tier"`
}

// CloudClient is the cap-mint client. Authenticates with the CLI's
// stored API key bearer token.
type CloudClient struct {
	creds  *Credentials
	http   *http.Client
}

// NewCloudClient returns a client for the given credentials. http.Client
// defaults to 60s timeout.
func NewCloudClient(creds *Credentials) *CloudClient {
	return &CloudClient{
		creds: creds,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CapMintRequest is the body of POST /api/cap/mint.
type CapMintRequest struct {
	Plugin        string         `json:"plugin"`
	PluginVersion string         `json:"plugin_version"`
	ManifestHash  string         `json:"manifest_hash"`
	Tool          string         `json:"tool"`
	InvocationID  string         `json:"invocation_id,omitempty"`
	Args          map[string]any `json:"args,omitempty"`
	TTLSeconds    int            `json:"ttl_seconds,omitempty"`
}

// MintCap calls POST /api/cap/mint. Returns either:
//   - Cap on success (HTTP 200), pending = nil.
//   - PendingApproval on pre-mode (HTTP 202), cap = nil.
//   - error (CapMintError or transport error) on failure.
func (c *CloudClient) MintCap(ctx context.Context, req *CapMintRequest) (*Cap, *PendingApproval, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.creds.CloudURL+"/api/cap/mint", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.creds.APIKey)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		var cap Cap
		if err := json.Unmarshal(raw, &cap); err != nil {
			return nil, nil, fmt.Errorf("decode cap: %w", err)
		}
		return &cap, nil, nil
	case http.StatusAccepted:
		var p PendingApproval
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, nil, fmt.Errorf("decode pending approval: %w", err)
		}
		return nil, &p, nil
	default:
		var e CapMintError
		_ = json.Unmarshal(raw, &e)
		e.Status = resp.StatusCode
		if e.Code == "" {
			e.Code = "unknown"
			e.Message = string(raw)
		}
		return nil, nil, &e
	}
}

// PollCap polls /api/cap/mint/poll/:approval_id until cap is minted,
// approval is denied/expired, or ctx is cancelled. 204 responses
// trigger a re-poll with exponential backoff (max 5s).
func (c *CloudClient) PollCap(ctx context.Context, pending *PendingApproval) (*Cap, error) {
	backoff := 1 * time.Second
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.creds.CloudURL+pending.PollURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.creds.APIKey)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			var cap Cap
			if err := json.Unmarshal(raw, &cap); err != nil {
				return nil, fmt.Errorf("decode cap: %w", err)
			}
			return &cap, nil
		case http.StatusNoContent:
			// Still pending — back off.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				if backoff < 5*time.Second {
					backoff *= 2
				}
			}
		default:
			var e CapMintError
			_ = json.Unmarshal(raw, &e)
			e.Status = resp.StatusCode
			if e.Code == "" {
				e.Code = "unknown"
				e.Message = string(raw)
			}
			return nil, &e
		}
	}
}
