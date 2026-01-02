package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
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

This command opens a browser window for OAuth authentication.
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

func runLogin(cmd *cobra.Command, args []string) error {
	// Check if already logged in
	if creds, err := mcper.LoadCredentials(); err == nil && creds.IsValid() {
		fmt.Printf("Already logged in as %s\n", creds.UserEmail)
		fmt.Println("Use 'mcper logout' to log out first.")
		return nil
	}

	fmt.Println("Logging in to mcper-cloud...")

	// Start local server to receive OAuth callback
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local server: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Channel to receive credentials
	credsChan := make(chan *mcper.Credentials, 1)
	errChan := make(chan error, 1)

	// Handle callback
	server := &http.Server{}
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Parse query params
		apiKey := r.URL.Query().Get("api_key")
		userEmail := r.URL.Query().Get("email")
		userID := r.URL.Query().Get("user_id")
		expiresAtStr := r.URL.Query().Get("expires_at")

		if apiKey == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "No API key received"
			}
			http.Error(w, errMsg, http.StatusBadRequest)
			errChan <- fmt.Errorf("login failed: %s", errMsg)
			return
		}

		var expiresAt time.Time
		if expiresAtStr != "" {
			expiresAt, _ = time.Parse(time.RFC3339, expiresAtStr)
		}

		creds := &mcper.Credentials{
			APIKey:    apiKey,
			UserEmail: userEmail,
			UserID:    userID,
			CloudURL:  loginServer,
			ExpiresAt: expiresAt,
		}

		// Send success response
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Login Successful</title></head>
<body style="font-family: -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);">
<div style="background: white; padding: 2rem; border-radius: 12px; text-align: center; box-shadow: 0 20px 40px rgba(0,0,0,0.1);">
<h1 style="color: #38a169;">Login Successful!</h1>
<p>You can close this window and return to your terminal.</p>
</div>
</body>
</html>`)

		credsChan <- creds
	})

	// Start server in goroutine
	go func() {
		server.Serve(listener)
	}()

	// Build login URL
	loginURL := fmt.Sprintf("%s/cli/login?callback=%s", loginServer, callbackURL)

	// Open browser
	fmt.Printf("Opening browser to: %s\n", loginURL)
	if err := openBrowser(loginURL); err != nil {
		fmt.Printf("Could not open browser automatically.\n")
		fmt.Printf("Please open this URL in your browser:\n%s\n", loginURL)
	}

	fmt.Println("\nWaiting for authentication...")

	// Wait for callback with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	select {
	case creds := <-credsChan:
		// Save credentials
		if err := mcper.SaveCredentials(creds); err != nil {
			return fmt.Errorf("failed to save credentials: %w", err)
		}
		fmt.Printf("\nLogged in successfully as %s\n", creds.UserEmail)
		fmt.Println("Your API key has been saved to ~/.mcper/credentials.json")
		return nil

	case err := <-errChan:
		return err

	case <-ctx.Done():
		return fmt.Errorf("login timed out after 5 minutes")
	}
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
