//go:build integration

// Package integration contains integration tests that exercise real external services.
// These tests require network access and are not run by default.
// Run with: go test -tags integration ./test/integration/...
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
)

// TestDiscoverControllers_RealGitHub tests that DiscoverControllers finds
// known ACK controllers from the real GitHub API.
func TestDiscoverControllers_RealGitHub(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Use a temp directory for the repo cache (we won't actually clone repos here;
	// discovery only lists repos from the GitHub API and then processes them).
	tmpDir := t.TempDir()
	repoCache, err := cache.NewRepoCache(tmpDir)
	if err != nil {
		t.Fatalf("creating repo cache: %v", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	discoverer := discovery.NewGitHubDiscoverer(token, repoCache)

	controllers, err := discoverer.DiscoverControllers(ctx)
	if err != nil {
		t.Fatalf("DiscoverControllers failed: %v", err)
	}

	// We expect at least some well-known controllers to be discovered.
	// The actual count depends on how many successfully clone + parse CRDs,
	// but the list from GitHub should include many controllers.
	if len(controllers) == 0 {
		t.Fatal("expected at least some controllers to be discovered, got 0")
	}

	t.Logf("Discovered %d controllers", len(controllers))

	// Verify well-known controllers are present
	knownControllers := map[string]bool{
		"s3":  false,
		"iam": false,
		"ec2": false,
	}

	for _, ctrl := range controllers {
		if _, ok := knownControllers[ctrl.ServiceName]; ok {
			knownControllers[ctrl.ServiceName] = true
		}

		// Verify structure: each controller must have service name and repo name
		if ctrl.ServiceName == "" {
			t.Errorf("controller has empty ServiceName")
		}
		if ctrl.RepoName == "" {
			t.Errorf("controller %s has empty RepoName", ctrl.ServiceName)
		}

		// Each controller should have at least one resource with string fields
		// (controllers with no resources are excluded by design)
		if len(ctrl.Resources) == 0 {
			t.Errorf("controller %s has no resources (should have been excluded)", ctrl.ServiceName)
		}

		for _, res := range ctrl.Resources {
			if res.Kind == "" {
				t.Errorf("controller %s has a resource with empty Kind", ctrl.ServiceName)
			}
		}
	}

	// Check that at least some well-known controllers were found
	found := 0
	for name, present := range knownControllers {
		if present {
			found++
			t.Logf("  Found known controller: %s", name)
		}
	}
	if found == 0 {
		t.Error("none of the well-known controllers (s3, iam, ec2) were discovered")
	}
}

// TestNewGitHubDiscoverer_RealGitHub tests that a GitHubDiscoverer can be
// successfully created with a real token/no token.
func TestNewGitHubDiscoverer_RealGitHub(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	tmpDir := t.TempDir()
	repoCache, err := cache.NewRepoCache(tmpDir)
	if err != nil {
		t.Fatalf("creating repo cache: %v", err)
	}

	discoverer := discovery.NewGitHubDiscoverer(token, repoCache)

	// Verify the discoverer was created successfully
	if discoverer == nil {
		t.Fatal("expected non-nil discoverer")
	}

	t.Log("GitHub discoverer created successfully")
}
