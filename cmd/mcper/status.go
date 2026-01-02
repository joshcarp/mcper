package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show mcper-cloud connection status",
	Long: `Show the current mcper-cloud login and connection status.

Displays:
- Login status and user email
- Cloud server URL
- Connected OAuth providers
- Local plugin count

Example:
  mcper status`,
	RunE: runStatus,
}

type ProviderStatus struct {
	Provider    string     `json:"provider"`
	DisplayName string     `json:"display_name"`
	Connected   bool       `json:"connected"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	// Load credentials
	creds, err := mcper.LoadCredentials()
	if err != nil {
		fmt.Println("Status: Not logged in")
		fmt.Println("\nUse 'mcper login' to connect to mcper-cloud")
		return nil
	}

	if !creds.IsValid() {
		fmt.Println("Status: Logged in (credentials expired)")
		fmt.Printf("User: %s\n", creds.UserEmail)
		fmt.Println("\nUse 'mcper login' to refresh your credentials")
		return nil
	}

	fmt.Println("Status: Logged in")
	fmt.Printf("User: %s\n", creds.UserEmail)
	fmt.Printf("Cloud: %s\n", creds.CloudURL)

	if !creds.ExpiresAt.IsZero() {
		remaining := time.Until(creds.ExpiresAt)
		if remaining > 0 {
			fmt.Printf("API Key expires: %s (%s remaining)\n",
				creds.ExpiresAt.Format(time.RFC3339),
				formatDuration(remaining))
		}
	}

	// Try to get connected services from cloud
	providers, err := getConnectedProviders(creds)
	if err != nil {
		fmt.Printf("\nCould not fetch connected services: %v\n", err)
	} else {
		fmt.Println("\nConnected Services:")
		for _, p := range providers {
			status := "Not connected"
			if p.Connected {
				status = "Connected"
				if p.ExpiresAt != nil {
					remaining := time.Until(*p.ExpiresAt)
					status = fmt.Sprintf("Connected (expires in %s)", formatDuration(remaining))
				}
			}
			fmt.Printf("  %s: %s\n", p.DisplayName, status)
		}
	}

	// Count cached plugins
	cachedPlugins, err := mcper.ListCachedPlugins()
	if err == nil {
		fmt.Printf("\nCached plugins: %d\n", len(cachedPlugins))
	}

	return nil
}

func getConnectedProviders(creds *mcper.Credentials) ([]ProviderStatus, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("GET", creds.CloudURL+"/api/v1/providers/status", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var providers []ProviderStatus
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, err
	}

	return providers, nil
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
