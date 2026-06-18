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

var matchFieldsCmd = &cobra.Command{
	Use:   "match-fields",
	Short: "Cross-reference Terraform JSON fields against ACK CRD string fields",
	Long: `Uses the AI agent to cross-reference Terraform-discovered JSON fields against
ACK CRD string fields to determine which ACK fields are JSON documents.`,
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

		// Invalidate match_fields cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating match_fields cache")
			}
			if err := resultCache.Invalidate("match_fields"); err != nil {
				return fmt.Errorf("invalidating cache: %w", err)
			}
		}

		// Discover controllers
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

		// Map controllers
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}
		mapResult, err := tools.MapAllControllers(ctx, ag, controllers, tfResult.Resources, resultCache, mapValidator)
		if err != nil {
			return fmt.Errorf("mapping controllers: %w", err)
		}

		// Analyze fields
		repoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
		if err != nil {
			return fmt.Errorf("ensuring terraform repo: %w", err)
		}

		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"resource_type", "json_fields"},
		}
		analysisResult, err := tools.AnalyzeAllDocs(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator)
		if err != nil {
			return fmt.Errorf("analyzing fields: %w", err)
		}

		// Match fields
		matchValidator := &agent.JSONValidator{
			RequiredFields: []string{"matches", "unmatched_tf_fields"},
		}
		result, err := tools.MatchAllResources(ctx, ag, controllers, analysisResult.Results, mapResult.Mappings, resultCache, matchValidator)
		if err != nil {
			return fmt.Errorf("matching fields: %w", err)
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
	matchFieldsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-match fields")
	rootCmd.AddCommand(matchFieldsCmd)
}
