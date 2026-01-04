package main

import (
	"bufio"
	"bytes"
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

This command prompts you to enter a code generated from the mcper-cloud website.

Steps:
  1. Visit the mcper-cloud website and log in
  2. Go to Dashboard > CLI Login Code
  3. Generate a code and paste it when prompted

Example:
  mcper login
  mcper login --code ABCD-1234
  mcper login --server https://api.mcper.com`,
	RunE: runLogin,
}

var (
	loginServer string
	loginCode   string
)

func init() {
	loginCmd.Flags().StringVar(&loginServer, "server", mcper.DefaultCloudURL, "mcper-cloud server URL")
	loginCmd.Flags().StringVar(&loginCode, "code", "", "Login code from website (skip interactive prompt)")
}

func runLogin(cmd *cobra.Command, args []string) error {
	// Check if already logged in
	if creds, err := mcper.LoadCredentials(); err == nil && creds.IsValid() {
		fmt.Printf("Already logged in as %s\n", creds.UserEmail)
		fmt.Println("Use 'mcper logout' to log out first.")
		return nil
	}

	// If code provided via flag, use it directly
	if loginCode != "" {
		return claimCode(loginCode)
	}

	// Interactive flow
	fmt.Println("To log in to mcper-cloud:")
	fmt.Println()
	fmt.Printf("  1. Visit: %s/login\n", loginServer)
	fmt.Println("  2. Log in with GitHub")
	fmt.Printf("  3. Go to: %s/dashboard/cli-code\n", loginServer)
	fmt.Println("  4. Generate a login code")
	fmt.Println("  5. Paste the code below")
	fmt.Println()

	// Try to open browser
	dashboardURL := loginServer + "/dashboard/cli-code"
	fmt.Printf("Opening browser to: %s\n", dashboardURL)
	if err := openBrowser(dashboardURL); err != nil {
		fmt.Println("Could not open browser automatically.")
	}
	fmt.Println()

	// Prompt for code
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter code from website: ")
	code, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("code cannot be empty")
	}

	return claimCode(code)
}

// claimCode exchanges a login code for an API key
func claimCode(code string) error {
	fmt.Println("Claiming login code...")

	// Normalize code (uppercase, handle with/without dash)
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) == 8 && !strings.Contains(code, "-") {
		code = code[:4] + "-" + code[4:]
	}

	// Get hostname for client info
	hostname, _ := os.Hostname()
	clientInfo := fmt.Sprintf("mcper-cli/%s on %s (%s)", mcper.Version, hostname, runtime.GOOS)

	// Prepare request body
	reqBody := struct {
		Code       string `json:"code"`
		ClientInfo string `json:"client_info"`
	}{
		Code:       code,
		ClientInfo: clientInfo,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make request to claim endpoint
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(
		loginServer+"/api/cli/claim",
		"application/json",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return fmt.Errorf("failed to claim code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("failed to claim code: %s", errResp.Error)
		}
		return fmt.Errorf("invalid or expired code (status %d)", resp.StatusCode)
	}

	// Parse response
	var result struct {
		APIKey    string `json:"api_key"`
		UserID    string `json:"user_id"`
		Email     string `json:"email"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Parse expiry time
	var expiresAt time.Time
	if result.ExpiresAt != "" {
		expiresAt, _ = time.Parse(time.RFC3339, result.ExpiresAt)
	}

	// Save credentials
	creds := &mcper.Credentials{
		APIKey:    result.APIKey,
		UserEmail: result.Email,
		UserID:    result.UserID,
		CloudURL:  loginServer,
		ExpiresAt: expiresAt,
	}

	if err := mcper.SaveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println()
	fmt.Printf("Logged in successfully as %s\n", creds.UserEmail)
	fmt.Println("Your API key has been saved to ~/.mcper/credentials.json")
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

// loginWithAPIKey allows direct API key login (for CI/CD)
var loginAPIKeyCmd = &cobra.Command{
	Use:   "login-api-key",
	Short: "Log in with an API key directly",
	Long: `Log in to mcper-cloud using an API key directly.

This is useful for CI/CD environments where interactive login is not possible.
Generate an API token from the dashboard at /dashboard/tokens.

Example:
  mcper login-api-key --api-key YOUR_API_KEY

  # In CI/CD (GitHub Actions):
  mcper login-api-key --api-key ${{ secrets.MCPER_API_KEY }}`,
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
