package mcper

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentials_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		creds    *Credentials
		expected bool
	}{
		{
			name:     "nil credentials",
			creds:    nil,
			expected: false,
		},
		{
			name:     "empty API key",
			creds:    &Credentials{APIKey: ""},
			expected: false,
		},
		{
			name:     "valid credentials no expiry",
			creds:    &Credentials{APIKey: "test-key"},
			expected: true,
		},
		{
			name: "valid credentials with future expiry",
			creds: &Credentials{
				APIKey:    "test-key",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			expected: true,
		},
		{
			name: "expired credentials",
			creds: &Credentials{
				APIKey:    "test-key",
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.creds.IsValid()
			if got != tt.expected {
				t.Errorf("IsValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCredentials_GetProxyURL(t *testing.T) {
	tests := []struct {
		name     string
		creds    *Credentials
		expected string
	}{
		{
			name:     "nil credentials",
			creds:    nil,
			expected: "",
		},
		{
			name:     "empty cloud URL",
			creds:    &Credentials{CloudURL: ""},
			expected: "",
		},
		{
			name:     "valid cloud URL",
			creds:    &Credentials{CloudURL: "https://api.mcper.com"},
			expected: "https://api.mcper.com/proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.creds.GetProxyURL()
			if got != tt.expected {
				t.Errorf("GetProxyURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSaveAndLoadCredentials(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "mcper-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "credentials.json")

	// Create test credentials
	creds := &Credentials{
		APIKey:    "test-api-key",
		UserEmail: "test@example.com",
		UserID:    "user-123",
		CloudURL:  "https://api.mcper.com",
		ExpiresAt: time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
	}

	// Save credentials
	if err := SaveCredentialsToPath(creds, path); err != nil {
		t.Fatalf("SaveCredentialsToPath() error = %v", err)
	}

	// Load credentials
	loaded, err := LoadCredentialsFromPath(path)
	if err != nil {
		t.Fatalf("LoadCredentialsFromPath() error = %v", err)
	}

	// Verify
	if loaded.APIKey != creds.APIKey {
		t.Errorf("APIKey = %q, want %q", loaded.APIKey, creds.APIKey)
	}
	if loaded.UserEmail != creds.UserEmail {
		t.Errorf("UserEmail = %q, want %q", loaded.UserEmail, creds.UserEmail)
	}
	if loaded.UserID != creds.UserID {
		t.Errorf("UserID = %q, want %q", loaded.UserID, creds.UserID)
	}
	if loaded.CloudURL != creds.CloudURL {
		t.Errorf("CloudURL = %q, want %q", loaded.CloudURL, creds.CloudURL)
	}
}

func TestLoadCredentialsFromPath_NotFound(t *testing.T) {
	_, err := LoadCredentialsFromPath("/nonexistent/path/credentials.json")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}
