package mcper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials represents stored authentication credentials for mcper-cloud
type Credentials struct {
	APIKey    string    `json:"api_key"`
	UserEmail string    `json:"user_email"`
	UserID    string    `json:"user_id"`
	CloudURL  string    `json:"cloud_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// DefaultCloudURL is the default mcper-cloud server URL
const DefaultCloudURL = "https://api.mcper.com"

// CredentialsFile is the filename for stored credentials
const CredentialsFile = "credentials.json"

// GetCredentialsPath returns the path to the credentials file
func GetCredentialsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(homeDir, ".mcper", CredentialsFile), nil
}

// LoadCredentials loads credentials from the default location
func LoadCredentials() (*Credentials, error) {
	path, err := GetCredentialsPath()
	if err != nil {
		return nil, err
	}

	return LoadCredentialsFromPath(path)
}

// LoadCredentialsFromPath loads credentials from a specific path
func LoadCredentialsFromPath(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not logged in: credentials file not found")
		}
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	return &creds, nil
}

// SaveCredentials saves credentials to the default location
func SaveCredentials(creds *Credentials) error {
	path, err := GetCredentialsPath()
	if err != nil {
		return err
	}

	return SaveCredentialsToPath(creds, path)
}

// SaveCredentialsToPath saves credentials to a specific path
func SaveCredentialsToPath(creds *Credentials, path string) error {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Write with restricted permissions (owner read/write only)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}

	return nil
}

// DeleteCredentials removes the credentials file
func DeleteCredentials() error {
	path, err := GetCredentialsPath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete credentials: %w", err)
	}

	return nil
}

// IsValid checks if the credentials are valid and not expired
func (c *Credentials) IsValid() bool {
	if c == nil {
		return false
	}
	if c.APIKey == "" {
		return false
	}
	// Check expiration (with 5 minute buffer)
	if !c.ExpiresAt.IsZero() && c.ExpiresAt.Before(time.Now().Add(5*time.Minute)) {
		return false
	}
	return true
}

// GetProxyURL returns the proxy URL for token injection
func (c *Credentials) GetProxyURL() string {
	if c == nil || c.CloudURL == "" {
		return ""
	}
	return c.CloudURL + "/proxy"
}

// IsLoggedIn checks if the user is logged in with valid credentials
func IsLoggedIn() bool {
	creds, err := LoadCredentials()
	if err != nil {
		return false
	}
	return creds.IsValid()
}
