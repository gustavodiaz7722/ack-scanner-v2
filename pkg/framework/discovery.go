// Package framework provides generic, reusable patterns for discovery, mapping,
// analysis, and matching tools in ack-scanner-v2. New detection pipelines
// (Upjet, API models, Terraform references) are thin wrappers around these
// generic implementations.
package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
)

// DiscoveryConfig describes how to discover files from a remote repository.
type DiscoveryConfig struct {
	// Org is the GitHub organization (e.g., "upbound").
	Org string
	// Repo is the GitHub repository name (e.g., "provider-aws").
	Repo string
	// SparsePaths are the paths to sparse-checkout (e.g., ["config"]).
	SparsePaths []string
	// FilePattern is a glob pattern for file enumeration (e.g., "config/*/config.go").
	// The glob is evaluated relative to the repo root directory.
	FilePattern string
	// ExtractName extracts a service/item name from a discovered file path
	// (relative to repo root). Return empty string to skip the file.
	ExtractName func(path string) string
	// ToolName is used for logging context.
	ToolName string
}

// DiscoveredFile represents a single file discovered from a repository.
type DiscoveredFile struct {
	// Name is the extracted service/item name.
	Name string
	// FilePath is the path relative to the repo root.
	FilePath string
}

// DiscoveryResult holds the output of a discovery operation.
type DiscoveryResult struct {
	// Files is the list of discovered files.
	Files []DiscoveredFile
	// RepoDir is the local path to the cloned repository.
	RepoDir string
}

// Discover performs a generic repository discovery: sparse clone → enumerate
// files matching a pattern → extract names. It returns the list of discovered
// files and the local repository directory path.
func Discover(ctx context.Context, config DiscoveryConfig, repoCache *cache.RepoCache, log *logger.Logger) (*DiscoveryResult, error) {
	if log == nil {
		log = logger.Nop()
	}

	toolName := config.ToolName
	if toolName == "" {
		toolName = "discover"
	}

	log.Info("%s: ensuring sparse clone of %s/%s", toolName, config.Org, config.Repo)

	// Sparse clone the repository
	repoDir, err := repoCache.EnsureRepoSparse(config.Org, config.Repo, config.SparsePaths)
	if err != nil {
		log.Error("%s: sparse clone failed: %v", toolName, err)
		return nil, fmt.Errorf("ensuring %s/%s sparse clone: %w", config.Org, config.Repo, err)
	}

	log.Debug("%s: repo available at %s", toolName, repoDir)

	// Enumerate files matching the glob pattern
	pattern := filepath.Join(repoDir, config.FilePattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Error("%s: glob pattern error: %v", toolName, err)
		return nil, fmt.Errorf("glob pattern %q: %w", config.FilePattern, err)
	}

	var files []DiscoveredFile
	for _, match := range matches {
		// Ensure it's a file, not a directory
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}

		// Get the path relative to repo root
		relPath, err := filepath.Rel(repoDir, match)
		if err != nil {
			continue
		}

		// Extract the name using the configured function
		name := config.ExtractName(relPath)
		if name == "" {
			continue
		}

		files = append(files, DiscoveredFile{
			Name:     name,
			FilePath: relPath,
		})
	}

	log.Info("%s: found %d files matching pattern", toolName, len(files))

	return &DiscoveryResult{
		Files:   files,
		RepoDir: repoDir,
	}, nil
}
