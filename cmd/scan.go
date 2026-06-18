package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/reporter"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// DefaultMaxParallel is the default bounded concurrency for agent calls.
const DefaultMaxParallel = 3

// Orchestrator manages the full scan workflow with per-item agent calls
// and bounded concurrency.
type Orchestrator struct {
	agent       *agent.Agent
	repoCache   *cache.RepoCache
	resultCache *cache.ResultCache
	verbose     bool
	maxParallel int
}

// RunFullScan executes the complete workflow:
// Phase 1: Discover controllers (local)
// Phase 2: Discover Terraform (local, sparse clone)
// Phase 3: Map controllers (agent, per-controller, cached)
// Phase 4: Analyze TF docs (agent, per-doc, cached)
// Phase 5: Match resources (agent, per-resource, cached)
// Phase 6: Generate report (local)
func (o *Orchestrator) RunFullScan(ctx context.Context) (*types.GapReport, error) {
	// Phase 1: Discover controllers
	o.logPhase(1, "Discovering ACK controllers...")
	ghDiscoverer := discovery.NewGitHubDiscoverer(githubToken, o.repoCache)
	controllers, err := ghDiscoverer.DiscoverControllers(ctx)
	if err != nil {
		return nil, fmt.Errorf("phase 1: discovering controllers: %w", err)
	}
	o.logProgress(1, "Discovered %d controllers", len(controllers))

	// Phase 2: Discover Terraform resources
	o.logPhase(2, "Discovering Terraform resources...")
	tfResult, err := tools.DiscoverTerraform(ctx, o.repoCache)
	if err != nil {
		return nil, fmt.Errorf("phase 2: discovering terraform resources: %w", err)
	}
	o.logProgress(2, "Discovered %d Terraform resources", len(tfResult.Resources))

	// Phase 3: Map controllers to Terraform docs (bounded concurrency)
	o.logPhase(3, "Mapping controllers to Terraform docs...")
	mapValidator := &agent.JSONValidator{
		RequiredFields: []string{"mapping"},
	}
	mapResult, err := o.mapControllersConcurrent(ctx, controllers, tfResult.Resources, mapValidator)
	if err != nil {
		return nil, fmt.Errorf("phase 3: mapping controllers: %w", err)
	}
	o.logProgress(3, "Mapped %d controllers (%d skipped)", len(mapResult.Mappings), len(mapResult.Skipped))

	// Phase 4: Analyze TF docs (bounded concurrency)
	o.logPhase(4, "Analyzing Terraform documentation for JSON fields...")
	repoDir, err := o.repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
	if err != nil {
		return nil, fmt.Errorf("phase 4: ensuring terraform repo: %w", err)
	}
	analyzeValidator := &agent.JSONValidator{
		RequiredFields: []string{"resource_type", "json_fields"},
	}
	analysisResult, err := o.analyzeDocsConcurrent(ctx, mapResult.Mappings, repoDir, analyzeValidator)
	if err != nil {
		return nil, fmt.Errorf("phase 4: analyzing fields: %w", err)
	}
	o.logProgress(4, "Analyzed %d docs (%d skipped)", len(analysisResult.Results), len(analysisResult.Skipped))

	// Phase 5: Match resources (bounded concurrency)
	o.logPhase(5, "Matching ACK fields against Terraform JSON fields...")
	matchValidator := &agent.JSONValidator{
		RequiredFields: []string{"matches", "unmatched_tf_fields"},
	}
	matchResult, err := o.matchResourcesConcurrent(ctx, controllers, analysisResult, mapResult.Mappings, matchValidator)
	if err != nil {
		return nil, fmt.Errorf("phase 5: matching fields: %w", err)
	}
	o.logProgress(5, "Matched %d resources (%d skipped)", len(matchResult.Results), len(matchResult.Skipped))

	// Phase 6: Generate report
	o.logPhase(6, "Generating gap report...")
	generatorConfigs := o.loadGeneratorConfigs(controllers)
	report := tools.GenerateReport(matchResult.Results, controllers, generatorConfigs)
	o.logProgress(6, "Report generated: %d entries, %d gaps", len(report.Entries), report.Summary.GapCount)

	// Report skipped items summary
	o.reportSkipped(mapResult.Skipped, analysisResult.Skipped, matchResult.Skipped)

	return report, nil
}

// mapControllersConcurrent maps controllers to TF docs with bounded concurrency.
func (o *Orchestrator) mapControllersConcurrent(
	ctx context.Context,
	controllers []types.ControllerInfo,
	tfResources []types.TerraformResourceInfo,
	validator agent.ResponseValidator,
) (*tools.MapAllControllersOutput, error) {
	output := &tools.MapAllControllersOutput{}
	total := len(controllers)

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

			o.logItemProgress(3, idx+1, total, "mapping %s", controller.ServiceName)

			mapping, err := tools.MapController(ctx, o.agent, controller, tfResources, o.resultCache, validator)
			if err != nil {
				if err == agent.ErrSkipItem {
					results[idx] = result{skipped: controller.ServiceName, index: idx}
					return
				}
				results[idx] = result{err: fmt.Errorf("mapping controller %q: %w", controller.ServiceName, err), index: idx}
				return
			}
			results[idx] = result{mapping: mapping, index: idx}
		}(i, ctrl)
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			// Log error, skip, continue (partial failure)
			fmt.Fprintf(os.Stderr, "[phase 3/6] error: %v (skipping)\n", r.err)
			output.Skipped = append(output.Skipped, controllers[r.index].ServiceName)
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

	// Collect unique doc file paths from all mappings
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

			o.logItemProgress(4, idx+1, total, "analyzing %s", filepath.Base(dp))

			// Read the doc content
			fullPath := filepath.Join(repoDir, dp)
			contentBytes, err := os.ReadFile(fullPath)
			if err != nil {
				results[idx] = result{docPath: dp, err: fmt.Errorf("reading %s: %w", dp, err)}
				return
			}

			analyzeResult, err := tools.AnalyzeDoc(ctx, o.agent, dp, string(contentBytes), o.resultCache, validator)
			if err != nil {
				if err == agent.ErrSkipItem {
					results[idx] = result{docPath: dp, skipped: true}
					return
				}
				results[idx] = result{docPath: dp, err: err}
				return
			}
			results[idx] = result{docPath: dp, output: analyzeResult}
		}(i, docPath)
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "[phase 4/6] error: %v (skipping %s)\n", r.err, r.docPath)
			output.Skipped = append(output.Skipped, r.docPath)
		} else if r.skipped {
			output.Skipped = append(output.Skipped, r.docPath)
		} else if r.output != nil {
			output.Results[r.docPath] = r.output
		}
	}

	return output, nil
}

// matchResourcesConcurrent matches ACK resources against TF JSON fields with bounded concurrency.
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

	// Build a lookup from doc file path to its analyzed JSON fields
	docFieldsMap := make(map[string][]types.JSONFieldInfo)
	for docPath, analysis := range analysisResults.Results {
		if analysis != nil {
			docFieldsMap[docPath] = analysis.JSONFields
		}
	}

	// Build a lookup from service name to its mapped TF doc paths
	serviceMappings := make(map[string][]string)
	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			serviceMappings[mapping.ServiceName] = append(serviceMappings[mapping.ServiceName], entry.DocFilePath)
		}
	}

	// Collect all (controller, resource) pairs that have TF JSON fields to match
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

			o.logItemProgress(5, idx+1, total, "matching %s/%s", mi.controller.ServiceName, mi.resource.Kind)

			matchResult, err := tools.MatchResource(ctx, o.agent, mi.resource, mi.tfFields, mi.controller.ServiceName, o.resultCache, validator)
			if err != nil {
				if err == agent.ErrSkipItem {
					results[idx] = result{itemKey: mi.itemKey, skipped: true}
					return
				}
				results[idx] = result{itemKey: mi.itemKey, err: err}
				return
			}
			results[idx] = result{itemKey: mi.itemKey, output: matchResult}
		}(i, item)
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "[phase 5/6] error: %v (skipping %s)\n", r.err, r.itemKey)
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
			if o.verbose {
				fmt.Fprintf(os.Stderr, "  warning: could not access repo %s: %v\n", ctrl.RepoName, err)
			}
			continue
		}
		genPath := filepath.Join(repoDir, "generator.yaml")
		genConfig, err := parser.ParseGeneratorConfig(genPath)
		if err != nil {
			if o.verbose {
				fmt.Fprintf(os.Stderr, "  warning: could not parse generator.yaml for %s: %v\n", ctrl.ServiceName, err)
			}
			continue
		}
		generatorConfigs[ctrl.ServiceName] = genConfig
	}
	return generatorConfigs
}

// reportSkipped logs a summary of skipped items from all phases.
func (o *Orchestrator) reportSkipped(mapSkipped, analyzeSkipped, matchSkipped []string) {
	totalSkipped := len(mapSkipped) + len(analyzeSkipped) + len(matchSkipped)
	if totalSkipped == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\n[scan] %d items skipped due to errors:\n", totalSkipped)
	if len(mapSkipped) > 0 {
		fmt.Fprintf(os.Stderr, "  Phase 3 (map controllers): %d skipped\n", len(mapSkipped))
		for _, s := range mapSkipped {
			fmt.Fprintf(os.Stderr, "    - %s\n", s)
		}
	}
	if len(analyzeSkipped) > 0 {
		fmt.Fprintf(os.Stderr, "  Phase 4 (analyze docs): %d skipped\n", len(analyzeSkipped))
		for _, s := range analyzeSkipped {
			fmt.Fprintf(os.Stderr, "    - %s\n", s)
		}
	}
	if len(matchSkipped) > 0 {
		fmt.Fprintf(os.Stderr, "  Phase 5 (match resources): %d skipped\n", len(matchSkipped))
		for _, s := range matchSkipped {
			fmt.Fprintf(os.Stderr, "    - %s\n", s)
		}
	}
}

// logPhase logs the start of a scan phase.
func (o *Orchestrator) logPhase(phase int, format string, args ...interface{}) {
	if o.verbose {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "[phase %d/6] %s\n", phase, msg)
	}
}

// logProgress logs progress for a scan phase.
func (o *Orchestrator) logProgress(phase int, format string, args ...interface{}) {
	if o.verbose {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "[phase %d/6] %s\n", phase, msg)
	}
}

// logItemProgress logs per-item progress within a phase.
func (o *Orchestrator) logItemProgress(phase, current, total int, format string, args ...interface{}) {
	if o.verbose {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "[phase %d/6] (%d/%d) %s\n", phase, current, total, msg)
	}
}

// scanCmd is the scan subcommand.
var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Run a full gap detection scan with bounded concurrency",
	Long: `Orchestrates the full gap detection workflow:
  Phase 1: Discover ACK controllers (local)
  Phase 2: Discover Terraform resources (local, sparse clone)
  Phase 3: Map controllers to Terraform docs (agent, per-controller, cached)
  Phase 4: Analyze Terraform docs for JSON fields (agent, per-doc, cached)
  Phase 5: Match ACK fields against Terraform JSON fields (agent, per-resource, cached)
  Phase 6: Generate gap report (local)

Uses bounded concurrency (--max-parallel) for agent phases to balance speed
against Bedrock rate limits. Handles partial failure by logging errors,
skipping failed items, and reporting skipped items at the end.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = DefaultMaxParallel
		}

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
		bedrockClient, err := agent.NewBedrockClient(ctx, region)
		if err != nil {
			return fmt.Errorf("creating bedrock client: %w", err)
		}

		ag, err := agent.NewAgent(bedrockClient, modelID)
		if err != nil {
			return fmt.Errorf("creating agent: %w", err)
		}

		// Create orchestrator
		orch := &Orchestrator{
			agent:       ag,
			repoCache:   repoCache,
			resultCache: resultCache,
			verbose:     verbose,
			maxParallel: maxParallel,
		}

		// Run full scan
		report, err := orch.RunFullScan(ctx)
		if err != nil {
			return err
		}

		// Format output
		return reporter.Format(report, output, os.Stdout)
	},
}

func init() {
	scanCmd.Flags().Int("max-parallel", DefaultMaxParallel, "Maximum number of concurrent agent calls (default 3)")
	rootCmd.AddCommand(scanCmd)
}
