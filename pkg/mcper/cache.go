package mcper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CacheMetadata represents the metadata for a cached WASM plugin
type CacheMetadata struct {
	Source       string       `json:"source"`
	SHA256       string       `json:"sha256"`
	Size         int64        `json:"size"`
	DownloadedAt time.Time    `json:"downloaded_at"`
	Permissions  *Permissions `json:"permissions,omitempty"`
	Env          []string     `json:"env,omitempty"`
}

// DefaultCacheDir returns the default cache directory (~/.mcper/cache)
func DefaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".mcper", "cache"), nil
}

// EnsureCacheDir creates the cache directory if it doesn't exist
func EnsureCacheDir() (string, error) {
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	return cacheDir, nil
}

// CacheEntry represents a cached plugin
type CacheEntry struct {
	WASMPath     string
	MetadataPath string
	Metadata     *CacheMetadata
}

// GetCacheEntry retrieves a cache entry for a plugin
func GetCacheEntry(plugin *ParsedPlugin) (*CacheEntry, error) {
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		return nil, err
	}

	wasmPath := plugin.CachePath(cacheDir)
	metadataPath := plugin.MetadataPath(cacheDir)

	// Check if WASM file exists
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		return nil, nil // Not cached
	}

	entry := &CacheEntry{
		WASMPath:     wasmPath,
		MetadataPath: metadataPath,
	}

	// Try to load metadata
	if data, err := os.ReadFile(metadataPath); err == nil {
		var meta CacheMetadata
		if err := json.Unmarshal(data, &meta); err == nil {
			entry.Metadata = &meta
		}
	}

	return entry, nil
}

// SaveToCache saves a WASM file to the cache
func SaveToCache(plugin *ParsedPlugin, wasmData []byte, permissions *Permissions, envVars []string) (*CacheEntry, error) {
	cacheDir, err := EnsureCacheDir()
	if err != nil {
		return nil, err
	}

	wasmPath := plugin.CachePath(cacheDir)
	metadataPath := plugin.MetadataPath(cacheDir)

	// Ensure registry directory exists
	registryDir := filepath.Dir(wasmPath)
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Calculate SHA256
	hash := sha256.Sum256(wasmData)
	hashStr := hex.EncodeToString(hash[:])

	// Write WASM file
	if err := os.WriteFile(wasmPath, wasmData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write WASM file: %w", err)
	}

	// Create metadata
	metadata := &CacheMetadata{
		Source:       plugin.RawURL,
		SHA256:       hashStr,
		Size:         int64(len(wasmData)),
		DownloadedAt: time.Now().UTC(),
		Permissions:  permissions,
		Env:          envVars,
	}

	// Write metadata
	metaBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, metaBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to write metadata file: %w", err)
	}

	return &CacheEntry{
		WASMPath:     wasmPath,
		MetadataPath: metadataPath,
		Metadata:     metadata,
	}, nil
}

// VerifyCache verifies the integrity of a cached plugin
func VerifyCache(entry *CacheEntry) (bool, error) {
	if entry.Metadata == nil {
		return false, nil
	}

	// Read WASM file
	f, err := os.Open(entry.WASMPath)
	if err != nil {
		return false, fmt.Errorf("failed to open WASM file: %w", err)
	}
	defer f.Close()

	// Calculate SHA256
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("failed to read WASM file: %w", err)
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	return actualHash == entry.Metadata.SHA256, nil
}

// ListCachedPlugins lists all plugins in the cache
func ListCachedPlugins() ([]CacheEntry, error) {
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		return nil, err
	}

	var entries []CacheEntry

	// Walk the cache directory
	err = filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only look for .wasm files
		if !info.IsDir() && filepath.Ext(path) == ".wasm" {
			entry := CacheEntry{
				WASMPath:     path,
				MetadataPath: path[:len(path)-5] + ".json", // Replace .wasm with .json
			}

			// Try to load metadata
			if data, err := os.ReadFile(entry.MetadataPath); err == nil {
				var meta CacheMetadata
				if err := json.Unmarshal(data, &meta); err == nil {
					entry.Metadata = &meta
				}
			}

			entries = append(entries, entry)
		}

		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to list cache: %w", err)
	}

	return entries, nil
}

// CleanCache removes all cached plugins
func CleanCache() error {
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		return err
	}

	if err := os.RemoveAll(cacheDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clean cache: %w", err)
	}

	return nil
}
