// Package cache provides a file-based result caching system for ack-scanner-v2.
// Tool results are stored as JSON files under a configurable base directory,
// organized by tool name and item key.
package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheEntry represents a cached result with metadata.
type CacheEntry struct {
	ToolName  string          `json:"tool_name"`
	ItemKey   string          `json:"item_key"`
	InputHash string          `json:"input_hash"`
	Timestamp time.Time       `json:"timestamp"`
	Version   string          `json:"version"`
	Result    json.RawMessage `json:"result"`
}

// ResultCache manages cached tool results as JSON files.
// Files are organized as: {baseDir}/{tool_name}/{item_key}.json
type ResultCache struct {
	baseDir string
}

// NewResultCache creates a new ResultCache rooted at the given base directory.
// The directory is created if it does not exist.
func NewResultCache(baseDir string) (*ResultCache, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}
	return &ResultCache{baseDir: baseDir}, nil
}

// hashInput computes a SHA-256 hash of the input parameters serialized as JSON.
func hashInput(inputParams any) (string, error) {
	data, err := json.Marshal(inputParams)
	if err != nil {
		return "", fmt.Errorf("marshaling input params: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// entryPath returns the filesystem path for a cache entry.
func (c *ResultCache) entryPath(toolName, itemKey string) string {
	return filepath.Join(c.baseDir, toolName, itemKey+".json")
}

// Get retrieves a cached result for the given tool, item key, and input parameters.
// It returns nil if no valid cache entry exists, if the input hash does not match,
// or if the cached file is corrupt (in which case the corrupt file is deleted).
func (c *ResultCache) Get(toolName, itemKey string, inputParams any) (*CacheEntry, error) {
	path := c.entryPath(toolName, itemKey)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cache file: %w", err)
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		// Corrupt cache: delete and return miss
		_ = os.Remove(path)
		return nil, nil
	}

	// Verify input hash matches
	hash, err := hashInput(inputParams)
	if err != nil {
		return nil, err
	}
	if entry.InputHash != hash {
		return nil, nil
	}

	return &entry, nil
}

// Put stores a tool result in the cache with metadata.
func (c *ResultCache) Put(toolName, itemKey string, inputParams any, result json.RawMessage) error {
	hash, err := hashInput(inputParams)
	if err != nil {
		return err
	}

	entry := CacheEntry{
		ToolName:  toolName,
		ItemKey:   itemKey,
		InputHash: hash,
		Timestamp: time.Now().UTC(),
		Version:   "1",
		Result:    result,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cache entry: %w", err)
	}

	dir := filepath.Dir(c.entryPath(toolName, itemKey))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache subdirectory: %w", err)
	}

	if err := os.WriteFile(c.entryPath(toolName, itemKey), data, 0o644); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}

	return nil
}

// Invalidate removes all cached results for a specific tool.
func (c *ResultCache) Invalidate(toolName string) error {
	dir := filepath.Join(c.baseDir, toolName)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing tool cache directory: %w", err)
	}
	return nil
}

// InvalidateItem removes a single cached result for a specific tool and item key.
func (c *ResultCache) InvalidateItem(toolName, itemKey string) error {
	path := c.entryPath(toolName, itemKey)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing cache file: %w", err)
	}
	return nil
}

// InvalidateAll removes all cached results.
func (c *ResultCache) InvalidateAll() error {
	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading cache directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(c.baseDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}
