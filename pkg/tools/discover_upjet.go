package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
)

// UpjetConfigInfo describes a discovered Upjet config file with its service name.
type UpjetConfigInfo struct {
	ServiceName string `json:"service_name"`
	FilePath    string `json:"file_path"`
}

// DiscoverUpjetOutput is the result of Upjet config discovery.
type DiscoverUpjetOutput struct {
	Configs []UpjetConfigInfo `json:"configs"`
}

// DiscoverUpjet discovers all Upjet/Crossplane AWS provider resource
// configuration files by sparse-cloning the upbound/provider-aws repository
// and enumerating config files. The repo may have either:
//   - Old layout: config/<service>/config.go
//   - New layout: config/{cluster,namespaced}/<service>/config.go
//
// Both patterns are tried; the first one that yields results is used.
func DiscoverUpjet(ctx context.Context, repoCache *cache.RepoCache, log ...*logger.Logger) (*DiscoverUpjetOutput, error) {
	l := resolveLogger(log)

	// Try new layout first (config/{cluster,namespaced}/<service>/config.go)
	// since that's the current upstream structure
	patterns := []string{
		filepath.Join("config", "*", "*", "config.go"), // new: config/{cluster,namespaced}/<service>/config.go
		filepath.Join("config", "*", "config.go"),      // old: config/<service>/config.go
	}

	var allConfigs []UpjetConfigInfo

	for _, pattern := range patterns {
		config := framework.DiscoveryConfig{
			Org:         "upbound",
			Repo:        "provider-aws",
			SparsePaths: []string{"config"},
			FilePattern: pattern,
			ExtractName: ExtractUpjetServiceName,
			ToolName:    "discover_upjet",
		}

		result, err := framework.Discover(ctx, config, repoCache, l)
		if err != nil {
			return nil, fmt.Errorf("discovering upjet configs: %w", err)
		}

		for _, f := range result.Files {
			allConfigs = append(allConfigs, UpjetConfigInfo{
				ServiceName: f.Name,
				FilePath:    f.FilePath,
			})
		}
	}

	// Deduplicate by service name (in case both patterns match)
	seen := make(map[string]bool)
	var dedupConfigs []UpjetConfigInfo
	for _, cfg := range allConfigs {
		if !seen[cfg.ServiceName] {
			seen[cfg.ServiceName] = true
			dedupConfigs = append(dedupConfigs, cfg)
		}
	}

	return &DiscoverUpjetOutput{
		Configs: dedupConfigs,
	}, nil
}

// ExtractUpjetServiceName extracts the service name from an Upjet config file
// path. Supported patterns:
//   - config/<service>/config.go (old layout) → service name from parts[1]
//   - config/{cluster,namespaced}/<service>/config.go (new layout) → service name from parts[2]
//
// Returns empty string if the path does not match either expected pattern.
func ExtractUpjetServiceName(path string) string {
	// Normalize to forward slashes for consistency
	path = filepath.ToSlash(path)

	parts := strings.Split(path, "/")

	// New layout: config/{cluster,namespaced}/<service>/config.go → 4 parts
	if len(parts) == 4 && parts[0] == "config" && parts[3] == "config.go" {
		// Skip intermediate directories that aren't service names
		if parts[1] == "cluster" || parts[1] == "namespaced" || parts[1] == "templates" || parts[1] == "test" {
			service := parts[2]
			if service == "" {
				return ""
			}
			return service
		}
	}

	// Old layout: config/<service>/config.go → 3 parts
	if len(parts) == 3 && parts[0] == "config" && parts[2] == "config.go" {
		service := parts[1]
		if service == "" {
			return ""
		}
		return service
	}

	return ""
}
