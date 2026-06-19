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
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/ignore"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
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
	log         *logger.Logger
	maxParallel int
	ignoreList  *ignore.List
}

// RunFullScan executes the complete workflow.
func (o *Orchestrator) RunFullScan(ctx context.Context) (*types.GapReport, error) {
	scanStart := time.Now()

	// Phase 1: Discover controllers
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

	// Phase 2: Discover Terraform resources
	o.log.PhaseStart(2, "Discovering Terraform resources")
	phaseStart = time.Now()
	tfResult, err := tools.DiscoverTerraform(ctx, o.repoCache, o.log)
	if err != nil {
		o.log.Error("terraform discovery failed: %v", err)
		return nil, fmt.Errorf("phase 2: discovering terraform resources: %w", err)
	}
	o.log.PhaseComplete(2, "Found %d Terraform resource docs (%s)",
		len(tfResult.Resources), formatDur(time.Since(phaseStart)))

	// Phase 3: Map controllers to Terraform docs
	o.log.PhaseStart(3, "Mapping controllers → Terraform docs (agent)")
	phaseStart = time.Now()
	mapValidator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}
	mapResult, err := o.mapControllersConcurrent(ctx, controllers, tfResult.Resources, mapValidator)
	if err != nil {
		o.log.Error("mapping failed: %v", err)
		return nil, fmt.Errorf("phase 3: mapping controllers: %w", err)
	}
	o.log.PhaseComplete(3, "Mapped %d controllers, %d skipped (%s)",
		len(mapResult.Mappings), len(mapResult.Skipped), formatDur(time.Since(phaseStart)))

	// Phase 4: Analyze TF docs for JSON fields
	o.log.PhaseStart(4, "Analyzing Terraform docs for JSON fields (agent)")
	phaseStart = time.Now()
	repoDir, err := o.repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
	if err != nil {
		o.log.Error("terraform repo clone failed: %v", err)
		return nil, fmt.Errorf("phase 4: ensuring terraform repo: %w", err)
	}
	analyzeValidator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "json_fields"}}
	analysisResult, err := o.analyzeDocsConcurrent(ctx, mapResult.Mappings, repoDir, analyzeValidator)
	if err != nil {
		o.log.Error("analysis failed: %v", err)
		return nil, fmt.Errorf("phase 4: analyzing fields: %w", err)
	}
	totalJSONFields := 0
	for _, r := range analysisResult.Results {
		totalJSONFields += len(r.JSONFields)
	}
	o.log.PhaseComplete(4, "Analyzed %d docs, found %d JSON fields, %d skipped (%s)",
		len(analysisResult.Results), totalJSONFields, len(analysisResult.Skipped), formatDur(time.Since(phaseStart)))

	// Phase 5: Match ACK fields against TF JSON fields
	o.log.PhaseStart(5, "Matching ACK fields ↔ Terraform JSON fields (agent)")
	phaseStart = time.Now()
	matchValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}
	matchResult, err := o.matchResourcesConcurrent(ctx, controllers, analysisResult, mapResult.Mappings, matchValidator)
	if err != nil {
		o.log.Error("matching failed: %v", err)
		return nil, fmt.Errorf("phase 5: matching fields: %w", err)
	}
	totalMatches := 0
	for _, r := range matchResult.Results {
		totalMatches += len(r.Matches)
	}
	o.log.PhaseComplete(5, "Matched %d resources, %d field matches, %d skipped (%s)",
		len(matchResult.Results), totalMatches, len(matchResult.Skipped), formatDur(time.Since(phaseStart)))

	// Phase 6: Generate report
	o.log.PhaseStart(6, "Generating gap report")
	phaseStart = time.Now()
	generatorConfigs := o.loadGeneratorConfigs(controllers)
	report := tools.GenerateReport(matchResult.Results, controllers, generatorConfigs, o.ignoreList, o.log)
	o.log.PhaseComplete(6, "Report: %d entries, %d gaps, %d annotated, %d incorrect (%s)",
		len(report.Entries), report.Summary.GapCount, report.Summary.AnnotatedCount,
		report.Summary.IncorrectCount, formatDur(time.Since(phaseStart)))

	// Final summary
	o.log.Summary(time.Since(scanStart), map[string]int{
		"Controllers discovered": len(controllers),
		"Terraform resources":    len(tfResult.Resources),
		"Controllers mapped":     len(mapResult.Mappings),
		"Docs analyzed":          len(analysisResult.Results),
		"Resources matched":      len(matchResult.Results),
		"Total field matches":    totalMatches,
		"Gaps (need annotation)": report.Summary.GapCount,
		"Already annotated":      report.Summary.AnnotatedCount,
		"Incorrect annotations":  report.Summary.IncorrectCount,
		"Items skipped (errors)": len(mapResult.Skipped) + len(analysisResult.Skipped) + len(matchResult.Skipped),
	})

	return report, nil
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

		// Load ignore configuration
		ignoreFile, _ := cmd.Flags().GetString("ignore-file")
		ignoreConfig, err := ignore.LoadConfig(ignoreFile)
		if err != nil {
			return fmt.Errorf("loading ignore config: %w", err)
		}
		ignoreList := ignore.NewList(ignoreConfig)
		if ignoreList.Count() > 0 {
			log.Info("Loaded %d ignore rules from %s", ignoreList.Count(), ignoreFile)
		}

		// Create orchestrator
		orch := &Orchestrator{
			agent:       ag,
			repoCache:   repoCache,
			resultCache: resultCache,
			log:         log,
			maxParallel: maxParallel,
			ignoreList:  ignoreList,
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
	scanCmd.Flags().Bool("debug", false, "Enable debug-level logging (includes cache hits, token counts)")
	scanCmd.Flags().String("ignore-file", "ignore.yaml", "Path to ignore configuration file for excluding fields from the report")
	rootCmd.AddCommand(scanCmd)
}
