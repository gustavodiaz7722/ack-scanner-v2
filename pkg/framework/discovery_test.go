package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
)

// setupTestRepo creates a temporary directory structure that simulates a sparse-cloned
// repo for testing discovery. It returns the temp dir and a RepoCache pointed at it.
func setupTestRepo(t *testing.T, org, repo string, files map[string]string) (*cache.RepoCache, string) {
	t.Helper()
	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, org, repo)

	// Create .git directory to signal the repo is "cloned"
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("creating .git dir: %v", err)
	}

	// Create all the test files
	for path, content := range files {
		fullPath := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("creating directory for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("writing file %s: %v", path, err)
		}
	}

	repoCache, err := cache.NewRepoCache(baseDir)
	if err != nil {
		t.Fatalf("creating repo cache: %v", err)
	}

	return repoCache, repoDir
}

func TestDiscover_BasicEnumeration(t *testing.T) {
	files := map[string]string{
		"config/elasticache/config.go": "package elasticache",
		"config/iam/config.go":         "package iam",
		"config/s3/config.go":          "package s3",
		"config/README.md":             "# readme",
	}

	repoCache, _ := setupTestRepo(t, "upbound", "provider-aws", files)

	config := DiscoveryConfig{
		Org:         "upbound",
		Repo:        "provider-aws",
		SparsePaths: []string{"config"},
		FilePattern: "config/*/config.go",
		ExtractName: func(path string) string {
			dir := filepath.Dir(path)
			return filepath.Base(dir)
		},
		ToolName: "discover_upjet",
	}

	result, err := Discover(context.Background(), config, repoCache, logger.Nop())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(result.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(result.Files))
	}

	// Verify names
	names := make(map[string]bool)
	for _, f := range result.Files {
		names[f.Name] = true
	}

	for _, expected := range []string{"elasticache", "iam", "s3"} {
		if !names[expected] {
			t.Errorf("expected to find service %q in results", expected)
		}
	}
}

func TestDiscover_EmptyResult(t *testing.T) {
	files := map[string]string{
		"config/README.md": "# readme",
	}

	repoCache, _ := setupTestRepo(t, "upbound", "provider-aws", files)

	config := DiscoveryConfig{
		Org:         "upbound",
		Repo:        "provider-aws",
		SparsePaths: []string{"config"},
		FilePattern: "config/*/config.go",
		ExtractName: func(path string) string {
			return filepath.Base(filepath.Dir(path))
		},
		ToolName: "discover_upjet",
	}

	result, err := Discover(context.Background(), config, repoCache, logger.Nop())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(result.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(result.Files))
	}
}

func TestDiscover_ExtractNameFilter(t *testing.T) {
	files := map[string]string{
		"models/s3.json":           `{}`,
		"models/iam.json":          `{}`,
		"models/not-a-service.txt": "text file",
	}

	repoCache, _ := setupTestRepo(t, "aws", "sdk", files)

	config := DiscoveryConfig{
		Org:         "aws",
		Repo:        "sdk",
		SparsePaths: []string{"models"},
		FilePattern: "models/*.json",
		ExtractName: func(path string) string {
			base := filepath.Base(path)
			ext := filepath.Ext(base)
			if ext != ".json" {
				return ""
			}
			return base[:len(base)-len(ext)]
		},
		ToolName: "discover_models",
	}

	result, err := Discover(context.Background(), config, repoCache, logger.Nop())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(result.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(result.Files))
	}

	names := make(map[string]bool)
	for _, f := range result.Files {
		names[f.Name] = true
	}
	if !names["s3"] || !names["iam"] {
		t.Errorf("expected s3 and iam, got %v", names)
	}
}

func TestDiscover_SkipsDirectories(t *testing.T) {
	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "org", "repo")

	// Create .git directory
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("creating .git dir: %v", err)
	}

	// Create a directory that matches the glob pattern
	if err := os.MkdirAll(filepath.Join(repoDir, "data", "subdir.json"), 0o755); err != nil {
		t.Fatalf("creating subdir: %v", err)
	}
	// Create a file that matches
	if err := os.WriteFile(filepath.Join(repoDir, "data", "real.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	repoCache, err := cache.NewRepoCache(baseDir)
	if err != nil {
		t.Fatalf("creating repo cache: %v", err)
	}

	config := DiscoveryConfig{
		Org:         "org",
		Repo:        "repo",
		SparsePaths: []string{"data"},
		FilePattern: "data/*.json",
		ExtractName: func(path string) string {
			base := filepath.Base(path)
			return base[:len(base)-len(filepath.Ext(base))]
		},
		ToolName: "test_discover",
	}

	result, err := Discover(context.Background(), config, repoCache, logger.Nop())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Should only find the file, not the directory
	if len(result.Files) != 1 {
		t.Errorf("expected 1 file (skipping directory), got %d", len(result.Files))
	}
	if len(result.Files) > 0 && result.Files[0].Name != "real" {
		t.Errorf("expected name 'real', got %q", result.Files[0].Name)
	}
}

func TestDiscover_RepoDir(t *testing.T) {
	files := map[string]string{
		"config/s3/config.go": "package s3",
	}

	repoCache, expectedRepoDir := setupTestRepo(t, "upbound", "provider-aws", files)

	config := DiscoveryConfig{
		Org:         "upbound",
		Repo:        "provider-aws",
		SparsePaths: []string{"config"},
		FilePattern: "config/*/config.go",
		ExtractName: func(path string) string {
			return filepath.Base(filepath.Dir(path))
		},
		ToolName: "test",
	}

	result, err := Discover(context.Background(), config, repoCache, logger.Nop())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if result.RepoDir != expectedRepoDir {
		t.Errorf("expected RepoDir %q, got %q", expectedRepoDir, result.RepoDir)
	}
}

func TestDiscover_NilLogger(t *testing.T) {
	files := map[string]string{
		"config/s3/config.go": "package s3",
	}

	repoCache, _ := setupTestRepo(t, "upbound", "provider-aws", files)

	config := DiscoveryConfig{
		Org:         "upbound",
		Repo:        "provider-aws",
		SparsePaths: []string{"config"},
		FilePattern: "config/*/config.go",
		ExtractName: func(path string) string {
			return filepath.Base(filepath.Dir(path))
		},
	}

	// Should not panic with nil logger
	result, err := Discover(context.Background(), config, repoCache, nil)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(result.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(result.Files))
	}
}
