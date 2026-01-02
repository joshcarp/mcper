package main

import (
	"fmt"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out of mcper-cloud",
	Long: `Log out of mcper-cloud and remove stored credentials.

This removes your API key from ~/.mcper/credentials.json

Example:
  mcper logout`,
	RunE: runLogout,
}

func runLogout(cmd *cobra.Command, args []string) error {
	// Check if logged in
	creds, err := mcper.LoadCredentials()
	if err != nil {
		fmt.Println("Not logged in.")
		return nil
	}

	// Delete credentials
	if err := mcper.DeleteCredentials(); err != nil {
		return fmt.Errorf("failed to delete credentials: %w", err)
	}

	if creds.UserEmail != "" {
		fmt.Printf("Logged out from %s\n", creds.UserEmail)
	} else {
		fmt.Println("Logged out successfully.")
	}

	return nil
}
