package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/reporter"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
)

var reportReferencesCmd = &cobra.Command{
	Use:   "report-references",
	Short: "Generate a reference gap report identifying ACK fields needing references: configuration",
	Long: `Generates a comprehensive report of all ACK controller fields that should have
references: configuration but don't, by merging results from three sources:
Upjet/Crossplane configs, AWS API models, and Terraform documentation.

The report prioritizes fields by confidence and source agreement.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		refresh, _ := cmd.Flags().GetBool("refresh")

		// Create caches
		repoCache, err := cache.NewRepoCache(cacheDir + "/repos")
		if err != nil {
			return fmt.Errorf("creating repo cache: %w", err)
		}

		resultCache, err := cache.NewResultCache(cacheDir)
		if err != nil {
			return fmt.Errorf("creating result cache: %w", err)
		}

		// Invalidate reference-related caches if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating reference match caches")
			}
			for _, toolName := range []string{"match_upjet", "match_models", "match_terraform_refs"} {
				if err := resultCache.Invalidate(toolName); err != nil {
					return fmt.Errorf("invalidating %s cache: %w", toolName, err)
				}
			}
		}

		// Discover controllers
		log := newCmdLogger()
		ghDiscoverer := discovery.NewGitHubDiscoverer(githubToken, repoCache, log)
		controllers, err := ghDiscoverer.DiscoverControllers(ctx)
		if err != nil {
			return fmt.Errorf("discovering controllers: %w", err)
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

		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = 5
		}

		// --- Upjet pipeline ---
		upjetResult, err := tools.DiscoverUpjet(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering upjet configs: %w", err)
		}

		mapUpjetValidator := &agent.JSONValidator{RequiredFields: []string{"upjet_configs"}}
		upjetMappings, err := tools.MapAllControllersToUpjet(ctx, ag, controllers, upjetResult.Configs, resultCache, mapUpjetValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to upjet: %w", err)
		}

		upjetRepoDir, err := repoCache.EnsureRepoSparse("upbound", "provider-aws", []string{"config"})
		if err != nil {
			return fmt.Errorf("ensuring upjet repo: %w", err)
		}

		analyzeUpjetValidator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}
		upjetAnalysis, err := tools.AnalyzeAllUpjetConfigs(ctx, ag, upjetMappings.Mappings, upjetRepoDir, resultCache, analyzeUpjetValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing upjet configs: %w", err)
		}

		matchUpjetValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}
		upjetMatches, err := tools.MatchAllResourcesUpjet(ctx, ag, controllers, upjetAnalysis.Results, upjetMappings.Mappings, resultCache, matchUpjetValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("matching upjet references: %w", err)
		}

		// --- API model pipeline ---
		modelsResult, err := tools.DiscoverModels(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering API models: %w", err)
		}

		mapModelsValidator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}
		modelMappings, err := tools.MapAllControllersToModels(ctx, ag, controllers, modelsResult.Models, resultCache, mapModelsValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to models: %w", err)
		}

		modelsRepoDir, err := repoCache.EnsureRepoSparse("aws", "aws-sdk-go-v2", []string{"codegen/sdk-codegen/aws-models"})
		if err != nil {
			return fmt.Errorf("ensuring aws-sdk-go-v2 repo: %w", err)
		}

		analyzeModelsValidator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}
		modelAnalysis, err := tools.AnalyzeAllModels(ctx, ag, modelMappings.Mappings, modelsRepoDir, controllers, resultCache, analyzeModelsValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing API models: %w", err)
		}

		matchModelsValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}
		modelMatches, err := tools.MatchAllResourcesModel(ctx, ag, controllers, modelAnalysis.Results, modelMappings.Mappings, resultCache, matchModelsValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("matching model references: %w", err)
		}

		// --- Terraform reference pipeline ---
		tfResult, err := tools.DiscoverTerraform(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering terraform resources: %w", err)
		}

		mapTFRefsValidator := &agent.JSONValidator{RequiredFields: []string{"terraform_doc_files"}}
		tfRefMappings, err := tools.MapAllControllersParallel(ctx, ag, controllers, tfResult.Resources, resultCache, mapTFRefsValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to terraform refs: %w", err)
		}

		tfRepoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
		if err != nil {
			return fmt.Errorf("ensuring terraform repo: %w", err)
		}

		analyzeTFRefsValidator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}
		tfRefAnalysis, err := tools.AnalyzeAllTerraformRefs(ctx, ag, tfRefMappings.Mappings, tfRepoDir, resultCache, analyzeTFRefsValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing terraform refs: %w", err)
		}

		matchTFRefsValidator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}
		tfRefMatches, err := tools.MatchAllResourcesTerraformRefs(ctx, ag, controllers, tfRefAnalysis.Results, tfRefMappings.Mappings, resultCache, matchTFRefsValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("matching terraform ref fields: %w", err)
		}

		// --- Load generator configs ---
		generatorConfigs := make(map[string]*parser.GeneratorConfig)
		for _, ctrl := range controllers {
			repoDir, err := repoCache.EnsureRepo(discovery.ACKOrg, ctrl.RepoName)
			if err != nil {
				if verbose {
					fmt.Fprintf(os.Stderr, "warning: could not access repo %s: %v\n", ctrl.RepoName, err)
				}
				continue
			}
			genPath := filepath.Join(repoDir, "generator.yaml")
			genConfig, err := parser.ParseGeneratorConfig(genPath)
			if err != nil {
				if verbose {
					fmt.Fprintf(os.Stderr, "warning: could not parse generator.yaml for %s: %v\n", ctrl.ServiceName, err)
				}
				continue
			}
			generatorConfigs[ctrl.ServiceName] = genConfig
		}

		// --- Generate report ---
		report := tools.GenerateReferenceReport(
			upjetMatches, modelMatches, tfRefMatches,
			controllers, generatorConfigs, log,
		)

		// Output
		switch output {
		case "json":
			return reporter.FormatReferenceJSON(report, os.Stdout)
		case "markdown", "md":
			return reporter.FormatReferenceMarkdown(report, os.Stdout)
		default:
			// Default to JSON for report-references since there's no table format
			return reporter.FormatReferenceJSON(report, os.Stdout)
		}
	},
}

func init() {
	reportReferencesCmd.Flags().Bool("refresh", false, "Invalidate match caches and regenerate report")
	reportReferencesCmd.Flags().Int("max-parallel", 5, "Maximum parallel agent calls")
	rootCmd.AddCommand(reportReferencesCmd)
}
