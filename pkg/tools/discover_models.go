package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
)

// APIModelInfo holds metadata about a single discovered AWS Smithy API model file.
type APIModelInfo struct {
	ServiceName string `json:"service_name"`
	FilePath    string `json:"file_path"`
}

// DiscoverModelsOutput is the result of AWS API model discovery.
type DiscoverModelsOutput struct {
	Models []APIModelInfo `json:"models"`
}

// DiscoverModels discovers all AWS Smithy JSON API model files by sparse-cloning
// the aws/aws-sdk-go-v2 repository and enumerating the model files under
// codegen/sdk-codegen/aws-models/.
func DiscoverModels(ctx context.Context, repoCache *cache.RepoCache, log ...*logger.Logger) (*DiscoverModelsOutput, error) {
	l := resolveLogger(log)

	l.Info("discover_models: ensuring sparse clone of aws/aws-sdk-go-v2")

	// Sparse clone only the codegen/sdk-codegen/aws-models/ directory
	repoDir, err := repoCache.EnsureRepoSparse("aws", "aws-sdk-go-v2", []string{"codegen/sdk-codegen/aws-models"})
	if err != nil {
		l.Error("discover_models: sparse clone failed: %v", err)
		return nil, fmt.Errorf("ensuring aws/aws-sdk-go-v2 sparse clone: %w", err)
	}

	l.Debug("discover_models: repo available at %s", repoDir)

	modelsDir := filepath.Join(repoDir, "codegen", "sdk-codegen", "aws-models")

	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		l.Error("discover_models: failed to read models directory: %v", err)
		return nil, fmt.Errorf("reading models directory %s: %w", modelsDir, err)
	}

	var models []APIModelInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		serviceName := ExtractModelServiceName(name)
		if serviceName == "" {
			continue
		}

		models = append(models, APIModelInfo{
			ServiceName: serviceName,
			FilePath:    filepath.Join("codegen", "sdk-codegen", "aws-models", name),
		})
	}

	l.Info("discover_models: found %d model files across %d files scanned", len(models), len(entries))

	return &DiscoverModelsOutput{
		Models: models,
	}, nil
}

// ExtractModelServiceName extracts the AWS service name from a Smithy model
// filename. The filename pattern is {service-name}.json where service names
// may contain hyphens for multi-segment names (e.g., "application-auto-scaling.json"
// → "application-auto-scaling", "elasticache.json" → "elasticache").
// Returns the service name, or empty string if the filename is invalid.
func ExtractModelServiceName(filename string) string {
	const suffix = ".json"
	if !strings.HasSuffix(filename, suffix) {
		return ""
	}
	base := strings.TrimSuffix(filename, suffix)
	if base == "" {
		return ""
	}
	return base
}
