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

var matchUpjetCmd = &cobra.Command{
	Use:   "match-upjet",
	Short: "Cross-reference Upjet reference fields against ACK CRD string fields",
	Long: `Uses the AI agent to cross-reference Upjet/Crossplane reference declarations
against ACK CRD string fields to determine which ACK fields should have references
configuration. Fields already annotated as is_document, is_iam_policy, or having
existing references are excluded from matching.`,
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

		// Invalidate match_upjet cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating match_upjet cache")
			}
			if err := resultCache.Invalidate("match_upjet"); err != nil {
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

		// Map controllers to Upjet configs
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"service_name"},
		}
		mapResult, err := tools.MapAllControllersToUpjet(ctx, ag, controllers, upjetResult.Configs, resultCache, mapValidator, 1, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to upjet: %w", err)
		}

		// Ensure upjet repo for analysis
		repoDir, err := repoCache.EnsureRepoSparse("upbound", "provider-aws", []string{"config"})
		if err != nil {
			return fmt.Errorf("ensuring upjet repo: %w", err)
		}

		// Analyze Upjet configs for references
		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"service_name", "references"},
		}
		analysisResult, err := tools.AnalyzeAllUpjetConfigs(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator, 1, log)
		if err != nil {
			return fmt.Errorf("analyzing upjet configs: %w", err)
		}

		// Match ACK fields against Upjet references
		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}
		matchValidator := &agent.JSONValidator{
			RequiredFields: []string{"matches", "unmatched_upjet_fields"},
		}
		result, err := tools.MatchAllResourcesUpjet(ctx, ag, controllers, analysisResult.Results, mapResult.Mappings, resultCache, matchValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("matching upjet fields: %w", err)
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
	matchUpjetCmd.Flags().Bool("refresh", false, "Invalidate cache and re-match Upjet fields")
	matchUpjetCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(matchUpjetCmd)
}
