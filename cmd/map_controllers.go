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

var mapControllersCmd = &cobra.Command{
	Use:   "map-controllers",
	Short: "Map ACK controllers to corresponding Terraform documentation files",
	Long: `Uses the AI agent to semantically map each ACK controller to its corresponding
Terraform AWS provider documentation files, resolving naming convention differences.`,
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

		// Invalidate map_controllers cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating map_controllers cache")
			}
			if err := resultCache.Invalidate("map_controllers"); err != nil {
				return fmt.Errorf("invalidating cache: %w", err)
			}
		}

		// Discover controllers first
		ghDiscoverer := discovery.NewGitHubDiscoverer(githubToken, repoCache)
		controllers, err := ghDiscoverer.DiscoverControllers(ctx)
		if err != nil {
			return fmt.Errorf("discovering controllers: %w", err)
		}

		// Discover Terraform resources
		tfResult, err := tools.DiscoverTerraform(ctx, repoCache)
		if err != nil {
			return fmt.Errorf("discovering terraform resources: %w", err)
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

		// Map all controllers
		result, err := tools.MapAllControllers(ctx, ag, controllers, tfResult.Resources, resultCache, validator)
		if err != nil {
			return fmt.Errorf("mapping controllers: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "ACK SERVICE\tTF RESOURCES\tNO MATCH REASON\n")
			for _, mapping := range result.Mappings {
				fmt.Fprintf(w, "%s\t%d\t%s\n",
					mapping.ServiceName, len(mapping.TFDocFiles), mapping.NoMatchReason)
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d controllers\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	mapControllersCmd.Flags().Bool("refresh", false, "Invalidate cache and re-map controllers")
	rootCmd.AddCommand(mapControllersCmd)
}
