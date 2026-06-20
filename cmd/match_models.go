package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
)

var matchModelsCmd = &cobra.Command{
	Use:   "match-models",
	Short: "Cross-reference API model reference signals against ACK CRD string fields",
	Long: `Uses the AI agent to cross-reference API-model-discovered reference signals
against ACK CRD string fields, providing a second source of reference signals
for fields not covered by Upjet. Smithy model field names are PascalCase and
directly correspond to ACK field names.`,
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

		// Invalidate match_models cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating match_models cache")
			}
			if err := resultCache.Invalidate("match_models"); err != nil {
				return fmt.Errorf("invalidating cache: %w", err)
			}
		}

		// Discover controllers
		log := newCmdLogger()
		ghDiscoverer := discovery.NewGitHubDiscoverer(githubToken, repoCache, log)
		controllers, err := ghDiscoverer.DiscoverControllers(ctx)
		if err != nil {
			return fmt.Errorf("discovering controllers: %w", err)
		}

		// Discover API models
		modelsResult, err := tools.DiscoverModels(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering API models: %w", err)
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
			maxParallel = tools.DefaultMaxParallel
		}

		// Map controllers to model files
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}
		mapResult, err := tools.MapAllControllersToModels(ctx, ag, controllers, modelsResult.Models, resultCache, mapValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to models: %w", err)
		}

		// Get the repo directory for reading model file content
		repoDir, err := repoCache.EnsureRepoSparse("aws", "aws-sdk-go-v2", []string{"codegen/sdk-codegen/aws-models"})
		if err != nil {
			return fmt.Errorf("ensuring aws-sdk-go-v2 repo: %w", err)
		}

		// Analyze all mapped models
		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"service_name", "references"},
		}
		analysisResult, err := tools.AnalyzeAllModels(ctx, ag, mapResult.Mappings, repoDir, controllers, resultCache, analyzeValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing models: %w", err)
		}

		// Match ACK fields against model reference signals
		matchValidator := &agent.JSONValidator{
			RequiredFields: []string{"matches", "unmatched_model_fields"},
		}
		result, err := tools.MatchAllResourcesModel(ctx, ag, controllers, analysisResult.Results, mapResult.Mappings, resultCache, matchValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("matching model fields: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "RESOURCE\tMATCHES\tUNMATCHED\n")
			for key, matchOutput := range result.Results {
				fmt.Fprintf(w, "%s\t%d\t%d\n",
					key, len(matchOutput.Matches), len(matchOutput.Unmatched))
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d resources\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	matchModelsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-match model fields")
	matchModelsCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(matchModelsCmd)
}
