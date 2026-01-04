package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to mcper-cloud",
	Long: `Log in to mcper-cloud to enable OAuth token management.

This command displays a code that you enter on the mcper website to authenticate.
After successful login, your API key is stored locally for future use.

Example:
  mcper login
  mcper login --server https://api.mcper.com`,
	RunE: runLogin,
}

var (
	loginServer string
)

func init() {
	loginCmd.Flags().StringVar(&loginServer, "server", mcper.DefaultCloudURL, "mcper-cloud server URL")
}

// DeviceCodeResponse from the server
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceTokenResponse from polling
type DeviceTokenResponse struct {
	AccessToken string `json:"access_token,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	Email       string `json:"email,omitempty"`
	Error       string `json:"error,omitempty"`
}

func runLogin(cmd *cobra.Command, args []string) error {
	// Check if already logged in
	if creds, err := mcper.LoadCredentials(); err == nil && creds.IsValid() {
		fmt.Printf("Already logged in as %s\n", creds.UserEmail)
		fmt.Println("Use 'mcper logout' to log out first.")
		return nil
	}

	fmt.Println("Logging in to mcper-cloud...")

	// Get client name (e.g., "Claude Code", "mcper-cli")
	clientName := getClientName()

	// Request device code from server
	reqBody, _ := json.Marshal(map[string]string{"client_name": clientName})
	resp, err := http.Post(loginServer+"/api/device/code", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to request device code: HTTP %d", resp.StatusCode)
	}

	var deviceCode DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return fmt.Errorf("failed to parse device code response: %w", err)
	}

	// Display the code to the user
	fmt.Println()
	fmt.Println("  ┌────────────────────────────────────────┐")
	fmt.Println("  │                                        │")
	fmt.Printf("  │     Your code is:  %s         │\n", deviceCode.UserCode)
	fmt.Println("  │                                        │")
	fmt.Println("  └────────────────────────────────────────┘")
	fmt.Println()
	fmt.Printf("  Open this URL in your browser:\n")
	fmt.Printf("  %s\n\n", deviceCode.VerificationURI)

	// Try to open browser automatically
	if err := openBrowser(deviceCode.VerificationURI + "?code=" + deviceCode.UserCode); err == nil {
		fmt.Println("  (Browser opened automatically)")
	}
	fmt.Println()
	fmt.Println("Waiting for authentication...")

	// Poll for token
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(deviceCode.ExpiresIn)*time.Second)
	defer cancel()

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("login timed out")
		case <-ticker.C:
			// Poll for token
			tokenReq, _ := json.Marshal(map[string]string{"device_code": deviceCode.DeviceCode})
			tokenResp, err := http.Post(loginServer+"/api/device/token", "application/json", bytes.NewReader(tokenReq))
			if err != nil {
				continue // Retry on network error
			}

			var tokenResult DeviceTokenResponse
			json.NewDecoder(tokenResp.Body).Decode(&tokenResult)
			tokenResp.Body.Close()

			switch tokenResult.Error {
			case "authorization_pending":
				// Still waiting, continue polling
				continue
			case "":
				// Success!
				if tokenResult.AccessToken != "" {
					creds := &mcper.Credentials{
						APIKey:    tokenResult.AccessToken,
						UserEmail: tokenResult.Email,
						UserID:    tokenResult.UserID,
						CloudURL:  loginServer,
						ExpiresAt: time.Now().Add(time.Duration(tokenResult.ExpiresIn) * time.Second),
					}
					if err := mcper.SaveCredentials(creds); err != nil {
						return fmt.Errorf("failed to save credentials: %w", err)
					}
					fmt.Printf("\nLogged in successfully as %s\n", creds.UserEmail)
					fmt.Println("Your API key has been saved to ~/.mcper/credentials.json")
					return nil
				}
			default:
				// Other error (expired, etc.)
				return fmt.Errorf("login failed: %s", tokenResult.Error)
			}
		}
	}
}

// getClientName tries to determine the client name
func getClientName() string {
	// Check if running inside Claude Code
	if os.Getenv("CLAUDE_CODE") != "" {
		return "Claude Code"
	}
	// Check for common IDE environment variables
	if os.Getenv("VSCODE_PID") != "" {
		return "VS Code"
	}
	if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
		return "iTerm"
	}
	// Default to hostname
	hostname, err := os.Hostname()
	if err == nil {
		return "mcper-cli on " + hostname
	}
	return "mcper-cli"
}

// loginManual handles manual API key entry
func loginManual() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter your API key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	fmt.Print("Enter your email (optional): ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)

	creds := &mcper.Credentials{
		APIKey:    apiKey,
		UserEmail: email,
		CloudURL:  loginServer,
	}

	if err := mcper.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println("Credentials saved successfully!")
	return nil
}

// openBrowser opens the default browser to the given URL
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

// loginWithAPIKey allows direct API key login (for testing/CI)
var loginAPIKeyCmd = &cobra.Command{
	Use:   "login-api-key",
	Short: "Log in with an API key directly",
	Long: `Log in to mcper-cloud using an API key directly.

This is useful for CI/CD environments or when browser login is not available.

Example:
  mcper login-api-key --api-key YOUR_API_KEY`,
	RunE: runLoginAPIKey,
}

var (
	loginAPIKey   string
	loginAPIEmail string
)

func init() {
	loginAPIKeyCmd.Flags().StringVar(&loginAPIKey, "api-key", "", "API key (required)")
	loginAPIKeyCmd.Flags().StringVar(&loginAPIEmail, "email", "", "Email address (optional)")
	loginAPIKeyCmd.Flags().StringVar(&loginServer, "server", mcper.DefaultCloudURL, "mcper-cloud server URL")
	loginAPIKeyCmd.MarkFlagRequired("api-key")
}

func runLoginAPIKey(cmd *cobra.Command, args []string) error {
	// Validate API key with server
	fmt.Println("Validating API key...")

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", loginServer+"/api/v1/me", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+loginAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate API key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid API key (status %d)", resp.StatusCode)
	}

	// Parse user info from response
	var userInfo struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		// If we can't parse, just use what we have
		userInfo.Email = loginAPIEmail
	}

	creds := &mcper.Credentials{
		APIKey:    loginAPIKey,
		UserEmail: userInfo.Email,
		UserID:    userInfo.UserID,
		CloudURL:  loginServer,
	}

	if err := mcper.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Printf("Logged in successfully as %s\n", creds.UserEmail)
	return nil
}
