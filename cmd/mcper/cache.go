package main

import (
	"fmt"

	"github.com/joshcarp/mcper/pkg/mcper"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the WASM plugin cache",
	Long:  `Commands for managing the global WASM plugin cache (~/.mcper/cache/)`,
}

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := mcper.ListCachedPlugins()
		if err != nil {
			return fmt.Errorf("failed to list cache: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println("No cached plugins found.")
			return nil
		}

		fmt.Println("Cached plugins:")
		for _, entry := range entries {
			if entry.Metadata != nil {
				fmt.Printf("  %s\n", entry.Metadata.Source)
				fmt.Printf("    Size: %d bytes\n", entry.Metadata.Size)
				fmt.Printf("    SHA256: %s\n", entry.Metadata.SHA256[:16]+"...")
				fmt.Printf("    Downloaded: %s\n", entry.Metadata.DownloadedAt.Format("2006-01-02 15:04:05"))
			} else {
				fmt.Printf("  %s (no metadata)\n", entry.WASMPath)
			}
		}

		return nil
	},
}

var cacheCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all cached plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := mcper.CleanCache(); err != nil {
			return fmt.Errorf("failed to clean cache: %w", err)
		}
		fmt.Println("Cache cleaned successfully.")
		return nil
	},
}

var cachePathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the cache directory path",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := mcper.DefaultCacheDir()
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	},
}

func init() {
	cacheCmd.AddCommand(cacheListCmd)
	cacheCmd.AddCommand(cacheCleanCmd)
	cacheCmd.AddCommand(cachePathCmd)
}
