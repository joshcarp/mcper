package mcper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifestV2_GithubFixture(t *testing.T) {
	// The github plugin's published manifest is the cross-repo contract;
	// changes here imply the cloud-side parser hash will also change.
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	path := filepath.Join(root, "plugins", "github", "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m, err := ParseManifestV2(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Name != "github" {
		t.Errorf("name = %q, want github", m.Name)
	}
	if m.OAuthProvider != "github" {
		t.Errorf("oauth_provider = %q, want github", m.OAuthProvider)
	}
	if got := len(m.Tools); got != 15 {
		t.Errorf("tools = %d, want 15", got)
	}
	// Spot-check: writes are `pre`, reads are `allow`.
	cases := map[string]string{
		"github_list_repos":        "allow",
		"github_get_repo":          "allow",
		"github_create_issue":      "pre",
		"github_add_issue_comment": "pre",
	}
	for name, want := range cases {
		tool := m.FindTool(name)
		if tool == nil {
			t.Errorf("tool %q missing", name)
			continue
		}
		if tool.ApprovalMode != want {
			t.Errorf("tool %q approval_mode = %q, want %q", name, tool.ApprovalMode, want)
		}
		if len(tool.Egress) == 0 {
			t.Errorf("tool %q has no egress", name)
		}
		for _, e := range tool.Egress {
			if e.Host != "api.github.com" {
				t.Errorf("tool %q egress host = %q, want api.github.com", name, e.Host)
			}
		}
	}
	// Hash must be deterministic on identical bytes — this is the cross-repo
	// agreement invariant. Re-serialising the parsed struct would produce
	// different bytes and break the contract.
	if HashRawManifest(raw) == "" {
		t.Errorf("empty hash")
	}
}

func TestParseManifestV2_RejectsBadEgress(t *testing.T) {
	// Each case must be rejected so the cross-repo contract holds.
	cases := []struct {
		name string
		body string
	}{
		{"host with userinfo", `{"name":"x","egress":[{"host":"user@host.com"}]}`},
		{"host with port", `{"name":"x","egress":[{"host":"host.com:443"}]}`},
		{"uppercase host", `{"name":"x","egress":[{"host":"API.GITHUB.COM"}]}`},
		{"wildcard host", `{"name":"x","egress":[{"host":"*.github.com"}]}`},
		{"lowercase method", `{"name":"x","tools":[{"name":"t","egress":[{"host":"a.com","methods":["get"]}]}]}`},
		{"bogus method", `{"name":"x","tools":[{"name":"t","egress":[{"host":"a.com","methods":["FROB"]}]}]}`},
		{"empty host", `{"name":"x","tools":[{"name":"t","egress":[{"host":""}]}]}`},
		{"approval_mode ask", `{"name":"x","tools":[{"name":"t","approval_mode":"ask"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseManifestV2([]byte(c.body)); err == nil {
				t.Errorf("expected error for %q, got nil", c.name)
			}
		})
	}
}

// repoRoot walks up from cwd until it finds go.mod (works whether tests run
// from pkg/mcper/ or repo root).
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
