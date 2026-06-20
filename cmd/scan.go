package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/reporter"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// DefaultMaxParallel is the default bounded concurrency for agent calls.
const DefaultMaxParallel = 3

// TotalPhases is the total number of scan phases when both pipelines run.
const TotalPhases = 18

// ScanResult holds both pipeline reports from a full scan.
type ScanResult struct {
	JSONFieldReport *types.GapReport          `json:"json_field_report"`
	ReferenceReport *types.ReferenceGapReport `json:"reference_report"`
}

// Orchestrator manages the full scan workflow with per-item agent calls
// and bounded concurrency.
type Orchestrator struct {
	agent          *agent.Agent
	repoCache      *cache.RepoCache
	resultCache    *cache.ResultCache
	log            *logger.Logger
	maxParallel    int
	skipReferences bool
	skipJSONFields bool
	outputDir      string
}

// RunFullScan executes the complete workflow with both pipelines.
func (o *Orchestrator) RunFullScan(ctx context.Context) (*ScanResult, error) {
	scanStart := time.Now()
	o.log.SetMaxPhase(TotalPhases)

	result := &ScanResult{}

	// Phase 1: Discover controllers (shared across both pipelines)
	o.log.PhaseStart(1, "Discovering ACK controllers")
	phaseStart := time.Now()
	ghDiscoverer := discovery.NewGitHubDiscoverer(githubToken, o.repoCache, o.log)
	controllers, err := ghDiscoverer.DiscoverControllers(ctx)
	if err != nil {
		o.log.Error("discovery failed: %v", err)
		return nil, fmt.Errorf("phase 1: discovering controllers: %w", err)
	}
	totalFields := 0
	for _, c := range controllers {
		for _, r := range c.Resources {
			totalFields += len(r.StringFields)
		}
	}
	o.log.PhaseComplete(1, "Found %d controllers, %d resources, %d string fields (%s)",
		len(controllers), countResources(controllers), totalFields, formatDur(time.Since(phaseStart)))

	// Phase 2: Discover Terraform resources (shared local operation)
	o.log.PhaseStart(2, "Discovering Terraform resources")
	phaseStart = time.Now()
	tfResult, err := tools.DiscoverTerraform(ctx, o.repoCache, o.log)
	if err != nil {
		o.log.Error("terraform discovery failed: %v", err)
		return nil, fmt.Errorf("phase 2: discovering terraform resources: %w", err)
	}
	o.log.PhaseComplete(2, "Found %d Terraform resource docs (%s)",
		len(tfResult.Resources), formatDur(time.Since(phaseStart)))

	// Phase 3: Discover Upjet configs (local operation)
	var upjetResult *tools.DiscoverUpjetOutput
	if !o.skipReferences {
		o.log.PhaseStart(3, "Discovering Upjet configs")
		phaseStart = time.Now()
		upjetResult, err = tools.DiscoverUpjet(ctx, o.repoCache, o.log)
		if err != nil {
			o.log.Error("upjet discovery failed: %v", err)
			return nil, fmt.Errorf("phase 3: discovering upjet configs: %w", err)
		}
		o.log.PhaseComplete(3, "Found %d Upjet config files (%s)",
			len(upjetResult.Configs), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(3, "Discovering Upjet configs (skipped)")
		o.log.PhaseComplete(3, "Skipped (--skip-references)")
	}

	// Phase 4: Discover AWS API models (local operation)
	var modelsResult *tools.DiscoverModelsOutput
	if !o.skipReferences {
		o.log.PhaseStart(4, "Discovering AWS API models")
		phaseStart = time.Now()
		modelsResult, err = tools.DiscoverModels(ctx, o.repoCache, o.log)
		if err != nil {
			o.log.Error("api model discovery failed: %v", err)
			return nil, fmt.Errorf("phase 4: discovering api models: %w", err)
		}
		o.log.PhaseComplete(4, "Found %d API model files (%s)",
			len(modelsResult.Models), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(4, "Discovering AWS API models (skipped)")
		o.log.PhaseComplete(4, "Skipped (--skip-references)")
	}

	// Phase 5: Map controllers → Terraform docs for JSON fields
	var jsonMapResult *tools.MapAllControllersOutput
	if !o.skipJSONFields {
		o.log.PhaseStart(5, "Mapping controllers → Terraform docs for JSON fields (agent)")
		phaseStart = time.Now()
		mapValidator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}
		jsonMapResult, err = o.mapControllersConcurrent(ctx, controllers, tfResult.Resources, mapValidator)
		if err != nil {
			o.log.Error("mapping failed: %v", err)
			return nil, fmt.Errorf("phase 5: mapping controllers for json fields: %w", err)
		}
		o.log.PhaseComplete(5, "Mapped %d controllers, %d skipped (%s)",
			len(jsonMapResult.Mappings), len(jsonMapResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(5, "Mapping controllers → Terraform docs for JSON fields (skipped)")
		o.log.PhaseComplete(5, "Skipped (--skip-json-fields)")
	}

	// Phase 6: Map controllers → Terraform docs for references (separate agent calls)
	var tfRefMapResult *tools.MapAllTerraformRefsOutput
	if !o.skipReferences {
		o.log.PhaseStart(6, "Mapping controllers → Terraform docs for references (agent)")
		phaseStart = time.Now()
		tfRefMapValidator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}
		tfRefMapResult, err = tools.MapAllControllersToTerraformRefs(
			ctx, o.agent, controllers, tfResult.Resources, o.resultCache, tfRefMapValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("terraform ref mapping failed: %v", err)
			return nil, fmt.Errorf("phase 6: mapping controllers for tf refs: %w", err)
		}
		o.log.PhaseComplete(6, "Mapped %d controllers, %d skipped (%s)",
			len(tfRefMapResult.Mappings), len(tfRefMapResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(6, "Mapping controllers → Terraform docs for references (skipped)")
		o.log.PhaseComplete(6, "Skipped (--skip-references)")
	}

	// Phase 7: Map controllers → Upjet configs (agent)
	var upjetMapResult *tools.MapAllUpjetOutput
	if !o.skipReferences {
		o.log.PhaseStart(7, "Mapping controllers → Upjet configs (agent)")
		phaseStart = time.Now()
		upjetMapValidator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}
		upjetMapResult, err = tools.MapAllControllersToUpjet(
			ctx, o.agent, controllers, upjetResult.Configs, o.resultCache, upjetMapValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("upjet mapping failed: %v", err)
			return nil, fmt.Errorf("phase 7: mapping controllers to upjet: %w", err)
		}
		o.log.PhaseComplete(7, "Mapped %d controllers, %d skipped (%s)",
			len(upjetMapResult.Mappings), len(upjetMapResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(7, "Mapping controllers → Upjet configs (skipped)")
		o.log.PhaseComplete(7, "Skipped (--skip-references)")
	}

	// Phase 8: Map controllers → API models (agent)
	var modelMapResult *tools.MapAllModelsOutput
	if !o.skipReferences {
		o.log.PhaseStart(8, "Mapping controllers → API models (agent)")
		phaseStart = time.Now()
		modelMapValidator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}
		modelMapResult, err = tools.MapAllControllersToModels(
			ctx, o.agent, controllers, modelsResult.Models, o.resultCache, modelMapValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("model mapping failed: %v", err)
			return nil, fmt.Errorf("phase 8: mapping controllers to models: %w", err)
		}
		o.log.PhaseComplete(8, "Mapped %d controllers, %d skipped (%s)",
			len(modelMapResult.Mappings), len(modelMapResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(8, "Mapping controllers → API models (skipped)")
		o.log.PhaseComplete(8, "Skipped (--skip-references)")
	}

	// Phase 9: Analyze TF docs for JSON fields
	var jsonAnalysisResult *tools.AnalyzeAllDocsOutput
	var repoDir string
	if !o.skipJSONFields {
		o.log.PhaseStart(9, "Analyzing Terraform docs for JSON fields (agent)")
		phaseStart = time.Now()
		repoDir, err = o.repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
		if err != nil {
			o.log.Error("terraform repo clone failed: %v", err)
			return nil, fmt.Errorf("phase 9: ensuring terraform repo: %w", err)
		}
		analyzeValidator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "json_fields"}}
		jsonAnalysisResult, err = o.analyzeDocsConcurrent(ctx, jsonMapResult.Mappings, repoDir, analyzeValidator)
		if err != nil {
			o.log.Error("analysis failed: %v", err)
			return nil, fmt.Errorf("phase 9: analyzing json fields: %w", err)
		}
		totalJSONFields := 0
		for _, r := range jsonAnalysisResult.Results {
			totalJSONFields += len(r.JSONFields)
		}
		o.log.PhaseComplete(9, "Analyzed %d docs, found %d JSON fields, %d skipped (%s)",
			len(jsonAnalysisResult.Results), totalJSONFields, len(jsonAnalysisResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(9, "Analyzing Terraform docs for JSON fields (skipped)")
		o.log.PhaseComplete(9, "Skipped (--skip-json-fields)")
	}

	// Phase 10: Analyze Terraform docs for resource references (separate agent calls)
	var tfRefAnalysisResult *tools.AnalyzeAllTerraformRefsOutput
	if !o.skipReferences {
		o.log.PhaseStart(10, "Analyzing Terraform docs for resource references (agent)")
		phaseStart = time.Now()
		if repoDir == "" {
			repoDir, err = o.repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
			if err != nil {
				o.log.Error("terraform repo clone failed: %v", err)
				return nil, fmt.Errorf("phase 10: ensuring terraform repo: %w", err)
			}
		}
		tfRefAnalyzeValidator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}
		tfRefAnalysisResult, err = tools.AnalyzeAllTerraformRefs(
			ctx, o.agent, tfRefMapResult.Mappings, repoDir, o.resultCache, tfRefAnalyzeValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("terraform ref analysis failed: %v", err)
			return nil, fmt.Errorf("phase 10: analyzing terraform refs: %w", err)
		}
		totalTFRefs := 0
		for _, r := range tfRefAnalysisResult.Results {
			totalTFRefs += len(r.References)
		}
		o.log.PhaseComplete(10, "Analyzed %d docs, found %d references, %d skipped (%s)",
			len(tfRefAnalysisResult.Results), totalTFRefs, len(tfRefAnalysisResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(10, "Analyzing Terraform docs for resource references (skipped)")
		o.log.PhaseComplete(10, "Skipped (--skip-references)")
	}

	// Phase 11: Analyze Upjet configs for references (agent)
	var upjetAnalysisResult *tools.AnalyzeAllUpjetOutput
	if !o.skipReferences {
		o.log.PhaseStart(11, "Analyzing Upjet configs for references (agent)")
		phaseStart = time.Now()
		upjetRepoDir, repoErr := o.repoCache.EnsureRepoSparse("upbound", "provider-aws", []string{"config"})
		if repoErr != nil {
			o.log.Error("upjet repo access failed: %v", repoErr)
			return nil, fmt.Errorf("phase 11: ensuring upjet repo: %w", repoErr)
		}
		upjetAnalyzeValidator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}
		upjetAnalysisResult, err = tools.AnalyzeAllUpjetConfigs(
			ctx, o.agent, upjetMapResult.Mappings, upjetRepoDir, o.resultCache, upjetAnalyzeValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("upjet analysis failed: %v", err)
			return nil, fmt.Errorf("phase 11: analyzing upjet configs: %w", err)
		}
		totalUpjetRefs := 0
		for _, r := range upjetAnalysisResult.Results {
			totalUpjetRefs += len(r.References)
		}
		o.log.PhaseComplete(11, "Analyzed %d configs, found %d references, %d skipped (%s)",
			len(upjetAnalysisResult.Results), totalUpjetRefs, len(upjetAnalysisResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(11, "Analyzing Upjet configs for references (skipped)")
		o.log.PhaseComplete(11, "Skipped (--skip-references)")
	}

	// Phase 12: Analyze API models for references (agent)
	var modelAnalysisResult *tools.AnalyzeAllModelsOutput
	if !o.skipReferences {
		o.log.PhaseStart(12, "Analyzing API models for references (agent)")
		phaseStart = time.Now()
		modelRepoDir, repoErr := o.repoCache.EnsureRepoSparse("aws", "aws-sdk-go-v2", []string{"codegen/sdk-codegen/aws-models"})
		if repoErr != nil {
			o.log.Error("api model repo access failed: %v", repoErr)
			return nil, fmt.Errorf("phase 12: ensuring api model repo: %w", repoErr)
		}
		modelAnalyzeValidator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}
		modelAnalysisResult, err = tools.AnalyzeAllModels(
			ctx, o.agent, modelMapResult.Mappings, modelRepoDir, controllers, o.resultCache, modelAnalyzeValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("model analysis failed: %v", err)
			return nil, fmt.Errorf("phase 12: analyzing api models: %w", err)
		}
		totalModelRefs := 0
		for _, r := range modelAnalysisResult.Results {
			totalModelRefs += len(r.References)
		}
		o.log.PhaseComplete(12, "Analyzed %d models, found %d references, %d skipped (%s)",
			len(modelAnalysisResult.Results), totalModelRefs, len(modelAnalysisResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(12, "Analyzing API models for references (skipped)")
		o.log.PhaseComplete(12, "Skipped (--skip-references)")
	}

	// Phase 13: Match ACK fields ↔ Terraform JSON fields
	var jsonMatchResult *tools.MatchAllResourcesOutput
	if !o.skipJSONFields {
		o.log.PhaseStart(13, "Matching ACK fields ↔ Terraform JSON fields (agent)")
		phaseStart = time.Now()
		matchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}
		jsonMatchResult, err = o.matchResourcesConcurrent(ctx, controllers, jsonAnalysisResult, jsonMapResult.Mappings, matchValidator)
		if err != nil {
			o.log.Error("matching failed: %v", err)
			return nil, fmt.Errorf("phase 13: matching json fields: %w", err)
		}
		totalMatches := 0
		for _, r := range jsonMatchResult.Results {
			totalMatches += len(r.Matches)
		}
		o.log.PhaseComplete(13, "Matched %d resources, %d field matches, %d skipped (%s)",
			len(jsonMatchResult.Results), totalMatches, len(jsonMatchResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(13, "Matching ACK fields ↔ Terraform JSON fields (skipped)")
		o.log.PhaseComplete(13, "Skipped (--skip-json-fields)")
	}

	// Phase 14: Match ACK fields ↔ Terraform doc references
	var tfRefMatchResult *tools.MatchAllTerraformRefsOutput
	if !o.skipReferences {
		o.log.PhaseStart(14, "Matching ACK fields ↔ Terraform doc references (agent)")
		phaseStart = time.Now()
		tfRefMatchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}
		tfRefMatchResult, err = tools.MatchAllResourcesTerraformRefs(
			ctx, o.agent, controllers, convertTFRefAnalysisResults(tfRefAnalysisResult),
			tfRefMapResult.Mappings, o.resultCache, tfRefMatchValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("tf ref matching failed: %v", err)
			return nil, fmt.Errorf("phase 14: matching terraform refs: %w", err)
		}
		totalTFRefMatches := 0
		for _, r := range tfRefMatchResult.Results {
			totalTFRefMatches += len(r.Matches)
		}
		o.log.PhaseComplete(14, "Matched %d resources, %d field matches, %d skipped (%s)",
			len(tfRefMatchResult.Results), totalTFRefMatches, len(tfRefMatchResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(14, "Matching ACK fields ↔ Terraform doc references (skipped)")
		o.log.PhaseComplete(14, "Skipped (--skip-references)")
	}

	// Phase 15: Match ACK fields ↔ Upjet references
	var upjetMatchResult *tools.MatchAllUpjetOutput
	if !o.skipReferences {
		o.log.PhaseStart(15, "Matching ACK fields ↔ Upjet references (agent)")
		phaseStart = time.Now()
		upjetMatchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}
		upjetMatchResult, err = tools.MatchAllResourcesUpjet(
			ctx, o.agent, controllers, convertUpjetAnalysisResults(upjetAnalysisResult),
			upjetMapResult.Mappings, o.resultCache, upjetMatchValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("upjet matching failed: %v", err)
			return nil, fmt.Errorf("phase 15: matching upjet refs: %w", err)
		}
		totalUpjetMatches := 0
		for _, r := range upjetMatchResult.Results {
			totalUpjetMatches += len(r.Matches)
		}
		o.log.PhaseComplete(15, "Matched %d resources, %d field matches, %d skipped (%s)",
			len(upjetMatchResult.Results), totalUpjetMatches, len(upjetMatchResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(15, "Matching ACK fields ↔ Upjet references (skipped)")
		o.log.PhaseComplete(15, "Skipped (--skip-references)")
	}

	// Phase 16: Match ACK fields ↔ API model references
	var modelMatchResult *tools.MatchAllModelOutput
	if !o.skipReferences {
		o.log.PhaseStart(16, "Matching ACK fields ↔ API model references (agent)")
		phaseStart = time.Now()
		modelMatchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}
		modelMatchResult, err = tools.MatchAllResourcesModel(
			ctx, o.agent, controllers, convertModelAnalysisResults(modelAnalysisResult),
			modelMapResult.Mappings, o.resultCache, modelMatchValidator, o.maxParallel, o.log)
		if err != nil {
			o.log.Error("model matching failed: %v", err)
			return nil, fmt.Errorf("phase 16: matching model refs: %w", err)
		}
		totalModelMatches := 0
		for _, r := range modelMatchResult.Results {
			totalModelMatches += len(r.Matches)
		}
		o.log.PhaseComplete(16, "Matched %d resources, %d field matches, %d skipped (%s)",
			len(modelMatchResult.Results), totalModelMatches, len(modelMatchResult.Skipped), formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(16, "Matching ACK fields ↔ API model references (skipped)")
		o.log.PhaseComplete(16, "Skipped (--skip-references)")
	}

	// Phase 17: Generate JSON field gap report
	if !o.skipJSONFields {
		o.log.PhaseStart(17, "Generating JSON field gap report")
		phaseStart = time.Now()
		generatorConfigs := o.loadGeneratorConfigs(controllers)
		jsonReport := tools.GenerateReport(jsonMatchResult.Results, controllers, generatorConfigs, o.log)
		result.JSONFieldReport = jsonReport
		o.log.PhaseComplete(17, "Report: %d entries, %d gaps, %d annotated, %d incorrect (%s)",
			len(jsonReport.Entries), jsonReport.Summary.GapCount, jsonReport.Summary.AnnotatedCount,
			jsonReport.Summary.IncorrectCount, formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(17, "Generating JSON field gap report (skipped)")
		o.log.PhaseComplete(17, "Skipped (--skip-json-fields)")
	}

	// Phase 18: Generate reference gap report
	if !o.skipReferences {
		o.log.PhaseStart(18, "Generating reference gap report")
		phaseStart = time.Now()
		generatorConfigs := o.loadGeneratorConfigs(controllers)
		refReport := tools.GenerateReferenceReport(
			upjetMatchResult, modelMatchResult, tfRefMatchResult,
			controllers, generatorConfigs, o.log)
		result.ReferenceReport = refReport
		o.log.PhaseComplete(18, "Report: %d entries, %d gaps, %d annotated, %d ambiguous (%s)",
			len(refReport.Entries), refReport.Summary.GapCount, refReport.Summary.AnnotatedCount,
			refReport.Summary.AmbiguousCount, formatDur(time.Since(phaseStart)))
	} else {
		o.log.PhaseStart(18, "Generating reference gap report (skipped)")
		o.log.PhaseComplete(18, "Skipped (--skip-references)")
	}

	// Final summary
	summaryStats := map[string]int{
		"Controllers discovered": len(controllers),
		"Terraform resources":    len(tfResult.Resources),
	}
	if !o.skipJSONFields && jsonMatchResult != nil {
		totalMatches := 0
		for _, r := range jsonMatchResult.Results {
			totalMatches += len(r.Matches)
		}
		summaryStats["JSON field matches"] = totalMatches
		summaryStats["JSON gaps (need annotation)"] = result.JSONFieldReport.Summary.GapCount
	}
	if !o.skipReferences && result.ReferenceReport != nil {
		summaryStats["Reference gaps"] = result.ReferenceReport.Summary.GapCount
		summaryStats["Reference total"] = result.ReferenceReport.Summary.TotalReferences
	}
	o.log.Summary(time.Since(scanStart), summaryStats)

	return result, nil
}

// convertTFRefAnalysisResults extracts the inner results map for the matching tool.
func convertTFRefAnalysisResults(r *tools.AnalyzeAllTerraformRefsOutput) map[string]*tools.AnalyzeTerraformRefsOutput {
	if r == nil {
		return nil
	}
	return r.Results
}

// convertUpjetAnalysisResults extracts the inner results map for the matching tool.
func convertUpjetAnalysisResults(r *tools.AnalyzeAllUpjetOutput) map[string]*tools.AnalyzeUpjetOutput {
	if r == nil {
		return nil
	}
	return r.Results
}

// convertModelAnalysisResults extracts the inner results map for the matching tool.
func convertModelAnalysisResults(r *tools.AnalyzeAllModelsOutput) map[string]*tools.AnalyzeModelOutput {
	if r == nil {
		return nil
	}
	return r.Results
}

// mapControllersConcurrent maps controllers with bounded concurrency.
func (o *Orchestrator) mapControllersConcurrent(
	ctx context.Context,
	controllers []types.ControllerInfo,
	tfResources []types.TerraformResourceInfo,
	validator agent.ResponseValidator,
) (*tools.MapAllControllersOutput, error) {
	output := &tools.MapAllControllersOutput{}
	total := len(controllers)
	var completed atomic.Int32

	type result struct {
		mapping *types.ControllerMapping
		skipped string
		err     error
		index   int
	}

	results := make([]result, total)
	sem := make(chan struct{}, o.maxParallel)
	var wg sync.WaitGroup

	for i, ctrl := range controllers {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(idx int, controller types.ControllerInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			o.log.AgentCall("map", controller.ServiceName)

			mapping, err := tools.MapController(ctx, o.agent, controller, tfResources, o.resultCache, validator, o.log)
			done := int(completed.Add(1))

			if err != nil {
				if err == agent.ErrSkipItem {
					o.log.Skip(controller.ServiceName, "validation failed after retries")
					results[idx] = result{skipped: controller.ServiceName, index: idx}
				} else {
					o.log.Error("mapping %s: %v", controller.ServiceName, err)
					results[idx] = result{err: err, index: idx}
				}
				o.log.Progress(done, total, "mapping controllers")
				return
			}

			numDocs := len(mapping.TFDocFiles)
			o.log.AgentResult(controller.ServiceName, time.Since(start), numDocs)
			o.log.Progress(done, total, "mapping controllers")
			results[idx] = result{mapping: mapping, index: idx}
		}(i, ctrl)
	}

	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			output.Skipped = append(output.Skipped, controllers[i].ServiceName)
		} else if r.skipped != "" {
			output.Skipped = append(output.Skipped, r.skipped)
		} else if r.mapping != nil {
			output.Mappings = append(output.Mappings, *r.mapping)
		}
	}

	return output, nil
}

// analyzeDocsConcurrent analyzes TF docs with bounded concurrency.
func (o *Orchestrator) analyzeDocsConcurrent(
	ctx context.Context,
	mappings []types.ControllerMapping,
	repoDir string,
	validator agent.ResponseValidator,
) (*tools.AnalyzeAllDocsOutput, error) {
	output := &tools.AnalyzeAllDocsOutput{
		Results: make(map[string]*tools.AnalyzeFieldsOutput),
	}

	seen := make(map[string]bool)
	var docPaths []string
	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			if entry.DocFilePath != "" && !seen[entry.DocFilePath] {
				seen[entry.DocFilePath] = true
				docPaths = append(docPaths, entry.DocFilePath)
			}
		}
	}

	total := len(docPaths)
	var completed atomic.Int32

	type result struct {
		docPath string
		output  *tools.AnalyzeFieldsOutput
		skipped bool
		err     error
	}

	results := make([]result, total)
	sem := make(chan struct{}, o.maxParallel)
	var wg sync.WaitGroup

	for i, docPath := range docPaths {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(idx int, dp string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			docName := filepath.Base(dp)
			o.log.AgentCall("analyze", docName)

			fullPath := filepath.Join(repoDir, dp)
			contentBytes, err := os.ReadFile(fullPath)
			if err != nil {
				o.log.Skip(docName, fmt.Sprintf("read error: %v", err))
				results[idx] = result{docPath: dp, err: err}
				completed.Add(1)
				return
			}

			analyzeResult, err := tools.AnalyzeDoc(ctx, o.agent, dp, string(contentBytes), o.resultCache, validator, o.log)
			done := int(completed.Add(1))

			if err != nil {
				if err == agent.ErrSkipItem {
					o.log.Skip(docName, "validation failed")
					results[idx] = result{docPath: dp, skipped: true}
				} else {
					o.log.Error("analyzing %s: %v", docName, err)
					results[idx] = result{docPath: dp, err: err}
				}
				o.log.Progress(done, total, "analyzing docs")
				return
			}

			numFields := len(analyzeResult.JSONFields)
			o.log.AgentResult(docName, time.Since(start), numFields)
			o.log.Progress(done, total, "analyzing docs")
			results[idx] = result{docPath: dp, output: analyzeResult}
		}(i, docPath)
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			output.Skipped = append(output.Skipped, r.docPath)
		} else if r.skipped {
			output.Skipped = append(output.Skipped, r.docPath)
		} else if r.output != nil {
			output.Results[r.docPath] = r.output
		}
	}

	return output, nil
}

// matchResourcesConcurrent matches resources with bounded concurrency.
func (o *Orchestrator) matchResourcesConcurrent(
	ctx context.Context,
	controllers []types.ControllerInfo,
	analysisResults *tools.AnalyzeAllDocsOutput,
	mappings []types.ControllerMapping,
	validator agent.ResponseValidator,
) (*tools.MatchAllResourcesOutput, error) {
	output := &tools.MatchAllResourcesOutput{
		Results: make(map[string]*tools.MatchFieldsOutput),
	}

	docFieldsMap := make(map[string][]types.JSONFieldInfo)
	for docPath, analysis := range analysisResults.Results {
		if analysis != nil {
			docFieldsMap[docPath] = analysis.JSONFields
		}
	}

	serviceMappings := make(map[string][]string)
	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			serviceMappings[mapping.ServiceName] = append(serviceMappings[mapping.ServiceName], entry.DocFilePath)
		}
	}

	type matchItem struct {
		controller types.ControllerInfo
		resource   types.ResourceInfo
		tfFields   []types.JSONFieldInfo
		itemKey    string
	}

	var items []matchItem
	for _, controller := range controllers {
		docPaths := serviceMappings[controller.ServiceName]
		var tfJSONFields []types.JSONFieldInfo
		for _, docPath := range docPaths {
			if fields, ok := docFieldsMap[docPath]; ok {
				tfJSONFields = append(tfJSONFields, fields...)
			}
		}
		if len(tfJSONFields) == 0 {
			continue
		}
		for _, resource := range controller.Resources {
			items = append(items, matchItem{
				controller: controller,
				resource:   resource,
				tfFields:   tfJSONFields,
				itemKey:    controller.ServiceName + "_" + resource.Kind,
			})
		}
	}

	total := len(items)
	var completed atomic.Int32

	type result struct {
		itemKey string
		output  *tools.MatchFieldsOutput
		skipped bool
		err     error
	}

	results := make([]result, total)
	sem := make(chan struct{}, o.maxParallel)
	var wg sync.WaitGroup

	for i, item := range items {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(idx int, mi matchItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			label := mi.controller.ServiceName + "/" + mi.resource.Kind
			o.log.AgentCall("match", label)

			matchResult, err := tools.MatchResource(ctx, o.agent, mi.resource, mi.tfFields, mi.controller.ServiceName, o.resultCache, validator, o.log)
			done := int(completed.Add(1))

			if err != nil {
				if err == agent.ErrSkipItem {
					o.log.Skip(label, "validation failed")
					results[idx] = result{itemKey: mi.itemKey, skipped: true}
				} else {
					o.log.Error("matching %s: %v", label, err)
					results[idx] = result{itemKey: mi.itemKey, err: err}
				}
				o.log.Progress(done, total, "matching resources")
				return
			}

			numMatches := len(matchResult.Matches)
			o.log.AgentResult(label, time.Since(start), numMatches)
			o.log.Progress(done, total, "matching resources")
			results[idx] = result{itemKey: mi.itemKey, output: matchResult}
		}(i, item)
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			output.Skipped = append(output.Skipped, r.itemKey)
		} else if r.skipped {
			output.Skipped = append(output.Skipped, r.itemKey)
		} else if r.output != nil {
			output.Results[r.itemKey] = r.output
		}
	}

	return output, nil
}

// loadGeneratorConfigs loads generator.yaml configs for all controllers.
func (o *Orchestrator) loadGeneratorConfigs(controllers []types.ControllerInfo) map[string]*parser.GeneratorConfig {
	generatorConfigs := make(map[string]*parser.GeneratorConfig)
	for _, ctrl := range controllers {
		repoDir, err := o.repoCache.EnsureRepo(discovery.ACKOrg, ctrl.RepoName)
		if err != nil {
			o.log.Debug("could not access repo %s: %v", ctrl.RepoName, err)
			continue
		}
		genPath := filepath.Join(repoDir, "generator.yaml")
		genConfig, err := parser.ParseGeneratorConfig(genPath)
		if err != nil {
			o.log.Debug("could not parse generator.yaml for %s: %v", ctrl.ServiceName, err)
			continue
		}
		generatorConfigs[ctrl.ServiceName] = genConfig
	}
	return generatorConfigs
}

// --- Helpers ---

func countResources(controllers []types.ControllerInfo) int {
	n := 0
	for _, c := range controllers {
		n += len(c.Resources)
	}
	return n
}

func formatDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// scanCmd is the scan subcommand.
var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Run a full gap detection scan with bounded concurrency",
	Long: `Orchestrates the full gap detection workflow with 18 phases:
  Phase 1:  Discover ACK controllers (local, shared)
  Phase 2:  Discover Terraform resources (local, sparse clone, shared)
  Phase 3:  Discover Upjet configs (local, sparse clone)
  Phase 4:  Discover AWS API models (local, sparse clone)
  Phase 5:  Map controllers → Terraform docs for JSON fields (agent)
  Phase 6:  Map controllers → Terraform docs for references (agent, separate)
  Phase 7:  Map controllers → Upjet configs (agent)
  Phase 8:  Map controllers → API models (agent)
  Phase 9:  Analyze Terraform docs for JSON fields (agent)
  Phase 10: Analyze Terraform docs for resource references (agent, separate)
  Phase 11: Analyze Upjet configs for references (agent)
  Phase 12: Analyze API models for references (agent)
  Phase 13: Match ACK fields ↔ Terraform JSON fields (agent)
  Phase 14: Match ACK fields ↔ Terraform doc references (agent)
  Phase 15: Match ACK fields ↔ Upjet references (agent)
  Phase 16: Match ACK fields ↔ API model references (agent)
  Phase 17: Generate JSON field gap report (local)
  Phase 18: Generate reference gap report (local)

Uses bounded concurrency (--max-parallel) for agent phases. Each pipeline
(JSON fields and references) invokes its own agent tools — no shared agent
results between pipelines. Local deterministic results (discovery file lists)
may be shared.

Use --skip-references to run only the JSON field pipeline.
Use --skip-json-fields to run only the reference detection pipeline.
Use --output-dir to write reports to separate files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = DefaultMaxParallel
		}

		skipReferences, _ := cmd.Flags().GetBool("skip-references")
		skipJSONFields, _ := cmd.Flags().GetBool("skip-json-fields")
		outputDir, _ := cmd.Flags().GetString("output-dir")

		// Set up logger
		logLevel := logger.LevelWarn
		if verbose {
			logLevel = logger.LevelInfo
		}
		debug, _ := cmd.Flags().GetBool("debug")
		if debug {
			logLevel = logger.LevelDebug
		}
		log := logger.New(logLevel, true)

		// Create caches
		repoCache, err := cache.NewRepoCache(cacheDir + "/repos")
		if err != nil {
			return fmt.Errorf("creating repo cache: %w", err)
		}

		resultCache, err := cache.NewResultCache(cacheDir)
		if err != nil {
			return fmt.Errorf("creating result cache: %w", err)
		}

		// Create Bedrock client and agent
		log.Info("Connecting to AWS Bedrock in %s...", region)
		bedrockClient, err := agent.NewBedrockClient(ctx, region)
		if err != nil {
			return fmt.Errorf("creating bedrock client: %w", err)
		}

		ag, err := agent.NewAgent(bedrockClient, modelID)
		if err != nil {
			return fmt.Errorf("creating agent: %w", err)
		}

		log.Info("Using model: %s", modelID)
		log.Info("Max parallel agent calls: %d", maxParallel)

		// Create orchestrator
		orch := &Orchestrator{
			agent:          ag,
			repoCache:      repoCache,
			resultCache:    resultCache,
			log:            log,
			maxParallel:    maxParallel,
			skipReferences: skipReferences,
			skipJSONFields: skipJSONFields,
			outputDir:      outputDir,
		}

		// Run full scan
		scanResult, err := orch.RunFullScan(ctx)
		if err != nil {
			return err
		}

		// Output reports
		if outputDir != "" {
			// Write reports to separate files in outputDir
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return fmt.Errorf("creating output directory: %w", err)
			}

			if scanResult.JSONFieldReport != nil {
				outPath := filepath.Join(outputDir, "json-field-report."+outputFileExtension(output))
				f, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("creating json field report file: %w", err)
				}
				if err := reporter.Format(scanResult.JSONFieldReport, output, f); err != nil {
					f.Close()
					return fmt.Errorf("writing json field report: %w", err)
				}
				f.Close()
				log.Info("Wrote JSON field report to %s", outPath)
			}

			if scanResult.ReferenceReport != nil {
				outPath := filepath.Join(outputDir, "reference-report."+outputFileExtension(output))
				f, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("creating reference report file: %w", err)
				}
				if err := reporter.FormatReference(scanResult.ReferenceReport, output, f); err != nil {
					f.Close()
					return fmt.Errorf("writing reference report: %w", err)
				}
				f.Close()
				log.Info("Wrote reference report to %s", outPath)
			}
		} else {
			// Write to stdout
			if scanResult.JSONFieldReport != nil {
				if err := reporter.Format(scanResult.JSONFieldReport, output, os.Stdout); err != nil {
					return fmt.Errorf("formatting json field report: %w", err)
				}
			}

			if scanResult.ReferenceReport != nil {
				// Add a separator between reports when writing both to stdout
				if scanResult.JSONFieldReport != nil {
					fmt.Fprintln(os.Stdout)
					fmt.Fprintln(os.Stdout, "---")
					fmt.Fprintln(os.Stdout)
				}
				if err := reporter.FormatReference(scanResult.ReferenceReport, output, os.Stdout); err != nil {
					return fmt.Errorf("formatting reference report: %w", err)
				}
			}
		}

		return nil
	},
}

// outputFileExtension returns the file extension for the given output format.
func outputFileExtension(format string) string {
	switch format {
	case "json":
		return "json"
	case "markdown", "md":
		return "md"
	case "table", "text":
		return "txt"
	default:
		return "txt"
	}
}

func init() {
	scanCmd.Flags().Int("max-parallel", DefaultMaxParallel, "Maximum number of concurrent agent calls (default 3)")
	scanCmd.Flags().Bool("debug", false, "Enable debug-level logging (includes cache hits, token counts)")
	scanCmd.Flags().Bool("skip-references", false, "Skip reference detection pipeline (run only JSON field detection)")
	scanCmd.Flags().Bool("skip-json-fields", false, "Skip JSON field detection pipeline (run only reference detection)")
	scanCmd.Flags().String("output-dir", "", "Write reports to separate files in this directory instead of stdout")
	rootCmd.AddCommand(scanCmd)
}
