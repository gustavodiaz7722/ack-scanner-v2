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

var mapModelsCmd = &cobra.Command{
	Use:   "map-models",
	Short: "Map ACK controllers to corresponding AWS Smithy API model files",
	Long: `Uses the AI agent to semantically map each ACK controller to its corresponding
AWS Smithy API model file, resolving naming convention differences
(e.g., ACK 'applicationautoscaling' → model 'application-auto-scaling.json').`,
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

		// Invalidate map_models cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating map_models cache")
			}
			if err := resultCache.Invalidate("map_models"); err != nil {
				return fmt.Errorf("invalidating cache: %w", err)
			}
		}

		// Discover controllers first
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

		// Create validator
		validator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}

		// Map all controllers to models
		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}
		result, err := tools.MapAllControllersToModels(ctx, ag, controllers, modelsResult.Models, resultCache, validator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to models: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "ACK SERVICE\tMODEL FILE\tCONFIDENCE\tNO MATCH REASON\n")
			for _, mapping := range result.Mappings {
				modelFile := mapping.ModelFile
				if modelFile == "" {
					modelFile = "(none)"
				}
				fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\n",
					mapping.ServiceName, modelFile, mapping.Confidence, mapping.NoMatchReason)
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d controllers\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	mapModelsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-map controllers to models")
	mapModelsCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(mapModelsCmd)
}
