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

var analyzeUpjetCmd = &cobra.Command{
	Use:   "analyze-upjet",
	Short: "Analyze Upjet config files to extract cross-resource reference declarations",
	Long: `Uses the AI agent to analyze each mapped Upjet/Crossplane AWS provider configuration
file and extract all r.References[...] declarations and delete(r.References, ...) patterns.
This identifies the ground-truth mapping of fields to their referenced Terraform resource types.`,
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

		// Invalidate analyze_upjet cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating analyze_upjet cache")
			}
			if err := resultCache.Invalidate("analyze_upjet"); err != nil {
				return fmt.Errorf("invalidating cache: %w", err)
			}
		}

		log := newCmdLogger()

		// Discover controllers
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

		// Get mapping results (needed to know which configs to analyze)
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"service_name"},
		}
		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}
		mapResult, err := tools.MapAllControllersToUpjet(ctx, ag, controllers, upjetResult.Configs, resultCache, mapValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to upjet: %w", err)
		}

		// Get the repo directory for reading config content
		repoDir, err := repoCache.EnsureRepoSparse("upbound", "provider-aws", []string{"config"})
		if err != nil {
			return fmt.Errorf("ensuring upjet repo: %w", err)
		}

		// Analyze all mapped configs
		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"service_name", "references"},
		}
		result, err := tools.AnalyzeAllUpjetConfigs(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing upjet configs: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "SERVICE\tREFERENCES\tAMBIGUOUS\n")
			for service, analysis := range result.Results {
				ambiguous := 0
				for _, ref := range analysis.References {
					if ref.IsAmbiguous {
						ambiguous++
					}
				}
				fmt.Fprintf(w, "%s\t%d\t%d\n", service, len(analysis.References), ambiguous)
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d configs\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	analyzeUpjetCmd.Flags().Bool("refresh", false, "Invalidate cache and re-analyze Upjet configs")
	analyzeUpjetCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(analyzeUpjetCmd)
}
