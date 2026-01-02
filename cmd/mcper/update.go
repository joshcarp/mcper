package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

const githubAPIURL = "https://api.github.com/repos/joshcarp/mcper/releases/latest"

// GitHubRelease represents the GitHub releases API response
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update mcper to the latest version",
	Long: `Update mcper to the latest version.

Downloads the latest binary from GitHub releases and replaces
the current installation.

Examples:
  mcper update           Update to latest version
  mcper update --check   Check for updates without installing`,
	RunE: runUpdate,
}

var checkOnly bool

func init() {
	updateCmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	// Fetch latest version info
	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	currentVersion := mcper.Version
	latestVersion := latest

	if !isNewerVersion(latestVersion, currentVersion) {
		fmt.Printf("mcper is up to date (v%s)\n", currentVersion)
		return nil
	}

	fmt.Printf("New version available: v%s (current: v%s)\n", latestVersion, currentVersion)

	if checkOnly {
		fmt.Println("\nRun 'mcper update' to install the latest version.")
		return nil
	}

	// Download and install
	return downloadAndInstall(latestVersion)
}

func fetchLatestVersion() (string, error) {
	resp, err := http.Get(githubAPIURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}

	// Remove 'v' prefix from tag
	version := strings.TrimPrefix(release.TagName, "v")
	return version, nil
}

// isNewerVersion returns true if version a is newer than version b
// Simple semver comparison (assumes format like "0.1.0")
func isNewerVersion(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		var aNum, bNum int
		fmt.Sscanf(aParts[i], "%d", &aNum)
		fmt.Sscanf(bParts[i], "%d", &bNum)
		if aNum > bNum {
			return true
		}
		if aNum < bNum {
			return false
		}
	}
	return len(aParts) > len(bParts)
}

func downloadAndInstall(version string) error {
	// Determine platform
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	platform := fmt.Sprintf("%s-%s", goos, goarch)
	binaryName := "mcper"
	assetName := fmt.Sprintf("mcper-%s", platform)
	if goos == "windows" {
		binaryName = "mcper.exe"
		assetName = fmt.Sprintf("mcper-%s.exe", platform)
	}

	// Download URL from GCS
	downloadURL := fmt.Sprintf("%s/v%s/%s", mcper.GCSBaseURL, version, assetName)
	fmt.Printf("Downloading mcper v%s for %s...\n", version, platform)

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download: HTTP %d (platform %s may not be available)", resp.StatusCode, platform)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "mcper-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download to temp file
	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Replace current binary
	// On Windows, we need to rename the old one first
	if goos == "windows" {
		oldPath := execPath + ".old"
		os.Remove(oldPath)
		if err := os.Rename(execPath, oldPath); err != nil {
			return fmt.Errorf("failed to backup old binary: %w", err)
		}
	}

	// Copy new binary to install location
	newBinary, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read new binary: %w", err)
	}

	if err := os.WriteFile(execPath, newBinary, 0755); err != nil {
		// Try alternative location
		homeDir, _ := os.UserHomeDir()
		altPath := fmt.Sprintf("%s/.mcper/bin/%s", homeDir, binaryName)
		if err := os.WriteFile(altPath, newBinary, 0755); err != nil {
			return fmt.Errorf("failed to install binary: %w", err)
		}
		fmt.Printf("Updated mcper at %s\n", altPath)
	} else {
		fmt.Printf("Updated mcper at %s\n", execPath)
	}

	fmt.Printf("Successfully updated to v%s\n", version)
	return nil
}

// CheckForUpdates checks if a newer version is available and prints a warning
// This is meant to be called from other commands (non-blocking, silent on error)
func CheckForUpdates() {
	latest, err := fetchLatestVersion()
	if err != nil {
		return // Silent fail
	}

	if isNewerVersion(latest, mcper.Version) {
		fmt.Fprintf(os.Stderr, "\n⚠️  A new version of mcper is available: v%s (current: v%s)\n", latest, mcper.Version)
		fmt.Fprintf(os.Stderr, "   Run 'mcper update' to upgrade.\n\n")
	}
}
