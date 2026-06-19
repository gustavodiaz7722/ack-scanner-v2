// Package tools implements the deterministic tool functions for ack-scanner-v2.
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// DiscoverTerraformOutput is the result of Terraform resource discovery.
type DiscoverTerraformOutput struct {
	Resources []types.TerraformResourceInfo `json:"resources"`
}

// DiscoverTerraform discovers all AWS Terraform resources by sparse-cloning
// the hashicorp/terraform-provider-aws repository and enumerating the
// documentation files under website/docs/r/.
func DiscoverTerraform(ctx context.Context, repoCache *cache.RepoCache, log ...*logger.Logger) (*DiscoverTerraformOutput, error) {
	l := resolveLogger(log)

	l.Info("discover_terraform: ensuring sparse clone of terraform-provider-aws")

	// Sparse clone only the website/docs/r/ directory
	repoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
	if err != nil {
		l.Error("discover_terraform: sparse clone failed: %v", err)
		return nil, fmt.Errorf("ensuring terraform-provider-aws sparse clone: %w", err)
	}

	l.Debug("discover_terraform: repo available at %s", repoDir)

	docsDir := filepath.Join(repoDir, "website", "docs", "r")

	entries, err := os.ReadDir(docsDir)
	if err != nil {
		l.Error("discover_terraform: failed to read docs directory: %v", err)
		return nil, fmt.Errorf("reading terraform docs directory %s: %w", docsDir, err)
	}

	var resources []types.TerraformResourceInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".html.markdown") {
			continue
		}

		service, resource, ok := ExtractTerraformFilenameComponents(name)
		if !ok {
			continue
		}

		resources = append(resources, types.TerraformResourceInfo{
			ServiceName:  service,
			ResourceType: resource,
			DocFilePath:  filepath.Join("website", "docs", "r", name),
		})
	}

	l.Info("discover_terraform: found %d resource docs across %d files scanned", len(resources), len(entries))

	return &DiscoverTerraformOutput{
		Resources: resources,
	}, nil
}

// ExtractTerraformFilenameComponents extracts the service name and resource type
// from a Terraform documentation filename. The filename pattern is:
// {service}_{resource}.html.markdown
// where the split occurs on the FIRST underscore.
// Returns (service, resource, ok).
func ExtractTerraformFilenameComponents(filename string) (service, resource string, ok bool) {
	// Strip the .html.markdown suffix
	const suffix = ".html.markdown"
	if !strings.HasSuffix(filename, suffix) {
		return "", "", false
	}
	base := strings.TrimSuffix(filename, suffix)

	// Split on the first underscore
	idx := strings.Index(base, "_")
	if idx <= 0 || idx >= len(base)-1 {
		// No underscore, or underscore at start/end — invalid
		return "", "", false
	}

	service = base[:idx]
	resource = base[idx+1:]
	return service, resource, true
}
