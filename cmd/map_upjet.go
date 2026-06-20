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

var mapUpjetCmd = &cobra.Command{
	Use:   "map-upjet",
	Short: "Map ACK controllers to corresponding Upjet configuration files",
	Long: `Uses the AI agent to semantically map each ACK controller to its corresponding
Upjet/Crossplane AWS provider configuration files, resolving naming convention differences
(e.g., ACK 'applicationautoscaling' → Upjet 'autoscaling').`,
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

		// Invalidate map_upjet cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating map_upjet cache")
			}
			if err := resultCache.Invalidate("map_upjet"); err != nil {
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

		// Discover Upjet configs
		upjetResult, err := tools.DiscoverUpjet(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering upjet configs: %w", err)
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
			RequiredFields: []string{"service_name"},
		}

		// Map all controllers to Upjet configs
		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}
		result, err := tools.MapAllControllersToUpjet(ctx, ag, controllers, upjetResult.Configs, resultCache, validator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to upjet: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "ACK SERVICE\tUPJET CONFIGS\tNO MATCH REASON\n")
			for _, mapping := range result.Mappings {
				fmt.Fprintf(w, "%s\t%d\t%s\n",
					mapping.ServiceName, len(mapping.UpjetConfigs), mapping.NoMatchReason)
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d controllers\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	mapUpjetCmd.Flags().Bool("refresh", false, "Invalidate cache and re-map controllers to Upjet configs")
	mapUpjetCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(mapUpjetCmd)
}
