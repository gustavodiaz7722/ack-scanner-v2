//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// TestFullScan_SNSController runs a focused end-to-end scan for the
// sns-controller. This exercises the full pipeline: discovery, terraform
// lookup, mapping, field analysis, matching, and report generation.
//
// This test requires:
// - ACK_SCANNER_INTEGRATION=1 environment variable
// - GITHUB_TOKEN (recommended for rate limits)
// - Valid AWS credentials with bedrock:InvokeModel permissions
func TestFullScan_SNSController(t *testing.T) {
	if os.Getenv("ACK_SCANNER_INTEGRATION") == "" {
		t.Skip("skipping full scan integration test: ACK_SCANNER_INTEGRATION not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tmpDir := t.TempDir()

	// Set up caches
	repoCache, err := cache.NewRepoCache(tmpDir + "/repos")
	if err != nil {
		t.Fatalf("creating repo cache: %v", err)
	}
	resultCache, err := cache.NewResultCache(tmpDir + "/results")
	if err != nil {
		t.Fatalf("creating result cache: %v", err)
	}

	// Phase 1: Discover controllers (filter to sns only)
	t.Log("Phase 1: Discovering controllers...")
	token := os.Getenv("GITHUB_TOKEN")
	ghDiscoverer := discovery.NewGitHubDiscoverer(token, repoCache)
	allControllers, err := ghDiscoverer.DiscoverControllers(ctx)
	if err != nil {
		t.Fatalf("DiscoverControllers failed: %v", err)
	}

	// Filter to just sns-controller
	var snsController *types.ControllerInfo
	for i := range allControllers {
		if allControllers[i].ServiceName == "sns" {
			snsController = &allControllers[i]
			break
		}
	}
	if snsController == nil {
		t.Fatal("sns-controller not found in discovered controllers")
	}
	t.Logf("  Found sns-controller with %d resources", len(snsController.Resources))

	// Phase 2: Discover Terraform resources
	t.Log("Phase 2: Discovering Terraform resources...")
	tfResult, err := tools.DiscoverTerraform(ctx, repoCache)
	if err != nil {
		t.Fatalf("DiscoverTerraform failed: %v", err)
	}
	t.Logf("  Found %d Terraform resources", len(tfResult.Resources))

	// Phase 3: Map sns-controller to TF docs via agent
	t.Log("Phase 3: Mapping sns-controller to Terraform docs...")
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	modelID := os.Getenv("ACK_SCANNER_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-sonnet-4-20250514-v1:0"
	}

	bedrockClient, err := agent.NewBedrockClient(ctx, region)
	if err != nil {
		t.Fatalf("NewBedrockClient failed: %v", err)
	}

	ag, err := agent.NewAgent(bedrockClient, modelID)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{
		RequiredFields: []string{"mapping"},
	}

	mapping, err := tools.MapController(ctx, ag, *snsController, tfResult.Resources, resultCache, validator)
	if err != nil {
		t.Fatalf("MapController failed: %v", err)
	}
	t.Logf("  Mapped to %d TF doc files", len(mapping.TFDocFiles))

	// Verify mapping has expected structure
	if mapping.ServiceName != "sns" {
		t.Errorf("expected mapping ServiceName='sns', got %q", mapping.ServiceName)
	}

	// Phase 4: Analyze TF docs for JSON fields
	t.Log("Phase 4: Analyzing Terraform docs for JSON fields...")
	repoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
	if err != nil {
		t.Fatalf("EnsureRepoSparse failed: %v", err)
	}

	analyzeValidator := &agent.JSONValidator{
		RequiredFields: []string{"resource_type", "json_fields"},
	}

	var allJSONFields []types.JSONFieldInfo
	for _, entry := range mapping.TFDocFiles {
		if entry.DocFilePath == "" {
			continue
		}
		fullPath := repoDir + "/" + entry.DocFilePath
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Logf("  Warning: could not read %s: %v", entry.DocFilePath, err)
			continue
		}

		analysisResult, err := tools.AnalyzeDoc(ctx, ag, entry.DocFilePath, string(content), resultCache, analyzeValidator)
		if err != nil {
			t.Logf("  Warning: AnalyzeDoc failed for %s: %v", entry.DocFilePath, err)
			continue
		}
		if analysisResult != nil {
			allJSONFields = append(allJSONFields, analysisResult.JSONFields...)
			t.Logf("  %s: found %d JSON fields", entry.DocFilePath, len(analysisResult.JSONFields))
		}
	}
	t.Logf("  Total JSON fields found: %d", len(allJSONFields))

	// Phase 5: Match ACK fields against TF JSON fields
	t.Log("Phase 5: Matching fields...")
	matchValidator := &agent.JSONValidator{
		RequiredFields: []string{"matches", "unmatched_tf_fields"},
	}

	matchResults := make(map[string]*tools.MatchFieldsOutput)
	for _, resource := range snsController.Resources {
		matchResult, err := tools.MatchResource(ctx, ag, resource, allJSONFields, "sns", resultCache, matchValidator)
		if err != nil {
			t.Logf("  Warning: MatchResource failed for %s: %v", resource.Kind, err)
			continue
		}
		if matchResult != nil {
			matchResults["sns_"+resource.Kind] = matchResult
			t.Logf("  %s: %d matches, %d unmatched",
				resource.Kind, len(matchResult.Matches), len(matchResult.Unmatched))
		}
	}

	// Phase 6: Generate report
	t.Log("Phase 6: Generating report...")
	report := tools.GenerateReport(matchResults, []types.ControllerInfo{*snsController}, nil)

	// Verify report structure
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	t.Logf("Report summary:")
	t.Logf("  Total matches: %d", report.Summary.TotalMatches)
	t.Logf("  Gaps: %d", report.Summary.GapCount)
	t.Logf("  Annotated: %d", report.Summary.AnnotatedCount)
	t.Logf("  Incorrect: %d", report.Summary.IncorrectCount)
	t.Logf("  Entries: %d", len(report.Entries))

	// Basic structural assertions
	for _, entry := range report.Entries {
		if entry.ServiceName == "" {
			t.Error("report entry has empty ServiceName")
		}
		if entry.ResourceName == "" {
			t.Error("report entry has empty ResourceName")
		}
		if entry.ACKFieldName == "" {
			t.Error("report entry has empty ACKFieldName")
		}
		if entry.RecommendedAnnotation == "" {
			t.Error("report entry has empty RecommendedAnnotation")
		}
		if entry.CurrentStatus == "" {
			t.Error("report entry has empty CurrentStatus")
		}
	}
}
