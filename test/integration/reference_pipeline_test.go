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

// TestReferencePipeline_ElastiCacheController runs an end-to-end reference
// detection pipeline for a single controller (elasticache). This exercises the
// full reference pipeline: discovery of Upjet configs and API models, mapping,
// analysis, matching, and report generation.
//
// This test requires:
// - ACK_SCANNER_INTEGRATION=1 environment variable
// - GITHUB_TOKEN (recommended for rate limits)
// - Valid AWS credentials with bedrock:InvokeModel permissions
func TestReferencePipeline_ElastiCacheController(t *testing.T) {
	if os.Getenv("ACK_SCANNER_INTEGRATION") == "" {
		t.Skip("skipping reference pipeline integration test: ACK_SCANNER_INTEGRATION not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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

	// Phase 1: Discover controllers (filter to elasticache only)
	t.Log("Phase 1: Discovering controllers...")
	token := os.Getenv("GITHUB_TOKEN")
	ghDiscoverer := discovery.NewGitHubDiscoverer(token, repoCache)
	allControllers, err := ghDiscoverer.DiscoverControllers(ctx)
	if err != nil {
		t.Fatalf("DiscoverControllers failed: %v", err)
	}

	var targetController *types.ControllerInfo
	for i := range allControllers {
		if allControllers[i].ServiceName == "elasticache" {
			targetController = &allControllers[i]
			break
		}
	}
	if targetController == nil {
		t.Fatal("elasticache-controller not found in discovered controllers")
	}
	t.Logf("  Found elasticache-controller with %d resources", len(targetController.Resources))
	controllers := []types.ControllerInfo{*targetController}

	// Phase 3: Discover Upjet configs
	t.Log("Phase 3: Discovering Upjet configs...")
	upjetResult, err := tools.DiscoverUpjet(ctx, repoCache)
	if err != nil {
		t.Fatalf("DiscoverUpjet failed: %v", err)
	}
	t.Logf("  Found %d Upjet config files", len(upjetResult.Configs))
	if len(upjetResult.Configs) == 0 {
		t.Fatal("expected at least one Upjet config file")
	}

	// Phase 4: Discover API models
	t.Log("Phase 4: Discovering AWS API models...")
	modelsResult, err := tools.DiscoverModels(ctx, repoCache)
	if err != nil {
		t.Fatalf("DiscoverModels failed: %v", err)
	}
	t.Logf("  Found %d API model files", len(modelsResult.Models))
	if len(modelsResult.Models) == 0 {
		t.Fatal("expected at least one API model file")
	}

	// Set up agent
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

	// Phase 7: Map controller → Upjet configs
	t.Log("Phase 7: Mapping elasticache → Upjet configs...")
	upjetMapValidator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}
	upjetMapResult, err := tools.MapAllControllersToUpjet(
		ctx, ag, controllers, upjetResult.Configs, resultCache, upjetMapValidator, 1)
	if err != nil {
		t.Fatalf("MapAllControllersToUpjet failed: %v", err)
	}
	t.Logf("  Upjet mappings: %d, skipped: %d", len(upjetMapResult.Mappings), len(upjetMapResult.Skipped))

	// Phase 8: Map controller → API models
	t.Log("Phase 8: Mapping elasticache → API models...")
	modelMapValidator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}
	modelMapResult, err := tools.MapAllControllersToModels(
		ctx, ag, controllers, modelsResult.Models, resultCache, modelMapValidator, 1)
	if err != nil {
		t.Fatalf("MapAllControllersToModels failed: %v", err)
	}
	t.Logf("  Model mappings: %d, skipped: %d", len(modelMapResult.Mappings), len(modelMapResult.Skipped))

	// Phase 11: Analyze Upjet configs for references
	t.Log("Phase 11: Analyzing Upjet configs for references...")
	upjetRepoDir, err := repoCache.EnsureRepoSparse("upbound", "provider-aws", []string{"config"})
	if err != nil {
		t.Fatalf("EnsureRepoSparse (upjet) failed: %v", err)
	}
	upjetAnalyzeValidator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}
	upjetAnalysisResult, err := tools.AnalyzeAllUpjetConfigs(
		ctx, ag, upjetMapResult.Mappings, upjetRepoDir, resultCache, upjetAnalyzeValidator, 1)
	if err != nil {
		t.Fatalf("AnalyzeAllUpjetConfigs failed: %v", err)
	}
	totalUpjetRefs := 0
	for _, r := range upjetAnalysisResult.Results {
		totalUpjetRefs += len(r.References)
	}
	t.Logf("  Analyzed %d configs, found %d references, skipped: %d",
		len(upjetAnalysisResult.Results), totalUpjetRefs, len(upjetAnalysisResult.Skipped))

	// Phase 12: Analyze API models for references
	t.Log("Phase 12: Analyzing API models for references...")
	modelRepoDir, err := repoCache.EnsureRepoSparse("aws", "aws-sdk-go-v2", []string{"codegen/sdk-codegen/aws-models"})
	if err != nil {
		t.Fatalf("EnsureRepoSparse (models) failed: %v", err)
	}
	modelAnalyzeValidator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}
	modelAnalysisResult, err := tools.AnalyzeAllModels(
		ctx, ag, modelMapResult.Mappings, modelRepoDir, controllers, resultCache, modelAnalyzeValidator, 1)
	if err != nil {
		t.Fatalf("AnalyzeAllModels failed: %v", err)
	}
	totalModelRefs := 0
	for _, r := range modelAnalysisResult.Results {
		totalModelRefs += len(r.References)
	}
	t.Logf("  Analyzed %d models, found %d references, skipped: %d",
		len(modelAnalysisResult.Results), totalModelRefs, len(modelAnalysisResult.Skipped))

	// Phase 15: Match ACK fields ↔ Upjet references
	t.Log("Phase 15: Matching ACK fields ↔ Upjet references...")
	upjetMatchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}
	upjetMatchResult, err := tools.MatchAllResourcesUpjet(
		ctx, ag, controllers, upjetAnalysisResult.Results, upjetMapResult.Mappings,
		resultCache, upjetMatchValidator, 1)
	if err != nil {
		t.Fatalf("MatchAllResourcesUpjet failed: %v", err)
	}
	totalUpjetMatches := 0
	for _, r := range upjetMatchResult.Results {
		totalUpjetMatches += len(r.Matches)
	}
	t.Logf("  Upjet matches: %d across %d resources, skipped: %d",
		totalUpjetMatches, len(upjetMatchResult.Results), len(upjetMatchResult.Skipped))

	// Phase 16: Match ACK fields ↔ API model references
	t.Log("Phase 16: Matching ACK fields ↔ API model references...")
	modelMatchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}
	modelMatchResult, err := tools.MatchAllResourcesModel(
		ctx, ag, controllers, modelAnalysisResult.Results, modelMapResult.Mappings,
		resultCache, modelMatchValidator, 1)
	if err != nil {
		t.Fatalf("MatchAllResourcesModel failed: %v", err)
	}
	totalModelMatches := 0
	for _, r := range modelMatchResult.Results {
		totalModelMatches += len(r.Matches)
	}
	t.Logf("  Model matches: %d across %d resources, skipped: %d",
		totalModelMatches, len(modelMatchResult.Results), len(modelMatchResult.Skipped))

	// Phase 18: Generate reference gap report
	t.Log("Phase 18: Generating reference gap report...")
	refReport := tools.GenerateReferenceReport(
		upjetMatchResult, modelMatchResult, nil, // no TF ref matches in this focused test
		controllers, nil)

	// Verify report structure
	if refReport == nil {
		t.Fatal("expected non-nil reference report")
	}

	t.Logf("Reference report summary:")
	t.Logf("  Total references: %d", refReport.Summary.TotalReferences)
	t.Logf("  Gaps: %d", refReport.Summary.GapCount)
	t.Logf("  Annotated: %d", refReport.Summary.AnnotatedCount)
	t.Logf("  Ambiguous: %d", refReport.Summary.AmbiguousCount)
	t.Logf("  Entries: %d", len(refReport.Entries))

	// Structural assertions on report entries
	for _, entry := range refReport.Entries {
		if entry.ServiceName == "" {
			t.Error("report entry has empty ServiceName")
		}
		if entry.ResourceName == "" {
			t.Error("report entry has empty ResourceName")
		}
		if entry.ACKFieldName == "" {
			t.Error("report entry has empty ACKFieldName")
		}
		if entry.CurrentStatus == "" {
			t.Error("report entry has empty CurrentStatus")
		}
		if len(entry.Sources) == 0 {
			t.Error("report entry has empty Sources")
		}
		if entry.Confidence <= 0 {
			t.Errorf("report entry for %s/%s.%s has non-positive confidence: %f",
				entry.ServiceName, entry.ResourceName, entry.ACKFieldName, entry.Confidence)
		}
	}

	// ElastiCache should have at least some reference fields
	// (e.g., KMSKeyID, SubnetGroupName, ParameterGroupName are common references)
	if refReport.Summary.TotalReferences == 0 {
		t.Error("expected at least some references for elasticache controller")
	}
}
