//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
)

// TestDiscoverTerraform_RealSparseClone tests that DiscoverTerraform
// successfully sparse-clones the terraform-provider-aws repository and
// finds .html.markdown documentation files.
func TestDiscoverTerraform_RealSparseClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping terraform sparse clone test in short mode (requires network I/O)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()
	repoCache, err := cache.NewRepoCache(tmpDir)
	if err != nil {
		t.Fatalf("creating repo cache: %v", err)
	}

	result, err := tools.DiscoverTerraform(ctx, repoCache)
	if err != nil {
		t.Fatalf("DiscoverTerraform failed: %v", err)
	}

	if len(result.Resources) == 0 {
		t.Fatal("expected Terraform resources to be discovered, got 0")
	}

	t.Logf("Discovered %d Terraform resources", len(result.Resources))

	// Verify known Terraform resources are present
	knownResources := map[string]bool{
		"s3_bucket":       false,
		"iam_role":        false,
		"lambda_function": false,
		"ec2_instance":    false,
		"sqs_queue":       false,
	}

	for _, docFile := range result.Resources {
		// Check structure
		if docFile == "" {
			t.Errorf("resource has empty doc file path")
			continue
		}
		if !strings.HasSuffix(docFile, ".html.markdown") {
			t.Errorf("resource has unexpected suffix: %s", docFile)
		}

		// Derive service/resource from path for validation
		base := filepath.Base(docFile)
		service, resource, ok := tools.ExtractTerraformFilenameComponents(base)
		if !ok {
			t.Errorf("cannot extract service/resource from: %s", docFile)
			continue
		}
		if service == "" {
			t.Errorf("derived service is empty for: %s", docFile)
		}
		if resource == "" {
			t.Errorf("derived resource is empty for: %s", docFile)
		}

		// Check for known resources
		fullName := service + "_" + resource
		if _, ok := knownResources[fullName]; ok {
			knownResources[fullName] = true
		}
	}

	// Verify at least some known resources were found
	found := 0
	for name, present := range knownResources {
		if present {
			found++
			t.Logf("  Found known TF resource: aws_%s", name)
		}
	}

	if found < 3 {
		t.Errorf("expected at least 3 well-known TF resources to be found, got %d", found)
		for name, present := range knownResources {
			if !present {
				t.Logf("  Missing: aws_%s", name)
			}
		}
	}
}

// TestExtractTerraformFilenameComponents verifies the filename parsing
// with real filename patterns from the terraform-provider-aws repo.
func TestExtractTerraformFilenameComponents(t *testing.T) {
	testCases := []struct {
		filename       string
		expectService  string
		expectResource string
		expectOK       bool
	}{
		{"s3_bucket.html.markdown", "s3", "bucket", true},
		{"iam_role.html.markdown", "iam", "role", true},
		{"lambda_function.html.markdown", "lambda", "function", true},
		{"ec2_instance.html.markdown", "ec2", "instance", true},
		{"appautoscaling_target.html.markdown", "appautoscaling", "target", true},
		{"s3_bucket_policy.html.markdown", "s3", "bucket_policy", true},
		{"invalid.html.markdown", "", "", false},
		{"no_suffix.txt", "", "", false},
	}

	for _, tc := range testCases {
		service, resource, ok := tools.ExtractTerraformFilenameComponents(tc.filename)
		if ok != tc.expectOK {
			t.Errorf("ExtractTerraformFilenameComponents(%q): got ok=%v, want %v", tc.filename, ok, tc.expectOK)
			continue
		}
		if ok {
			if service != tc.expectService {
				t.Errorf("ExtractTerraformFilenameComponents(%q): got service=%q, want %q", tc.filename, service, tc.expectService)
			}
			if resource != tc.expectResource {
				t.Errorf("ExtractTerraformFilenameComponents(%q): got resource=%q, want %q", tc.filename, resource, tc.expectResource)
			}
		}
	}
}
