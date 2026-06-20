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

var matchTerraformRefsCmd = &cobra.Command{
	Use:   "match-terraform-refs",
	Short: "Cross-reference Terraform doc references against ACK CRD string fields",
	Long: `Uses the AI agent to cross-reference Terraform documentation-discovered
cross-resource references against ACK CRD string fields to determine which ACK
fields should have references: configuration.

This tool uses the reference pipeline's Terraform analysis results (from
analyze-terraform-refs) and matches them against each ACK resource's unannotated
string fields. Fields already marked as is_document, is_iam_policy, or having
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

		// Invalidate match_terraform_refs cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating match_terraform_refs cache")
			}
			if err := resultCache.Invalidate("match_terraform_refs"); err != nil {
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

		// Discover Terraform resources
		tfResult, err := tools.DiscoverTerraform(ctx, repoCache, log)
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

		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}

		// Map controllers to Terraform docs for references
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}
		mapResult, err := tools.MapAllControllersToTerraformRefs(ctx, ag, controllers, tfResult.Resources, resultCache, mapValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to terraform refs: %w", err)
		}

		// Ensure Terraform repo is available for analysis
		repoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
		if err != nil {
			return fmt.Errorf("ensuring terraform repo: %w", err)
		}

		// Analyze Terraform docs for references
		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"resource_type", "references"},
		}
		analysisResult, err := tools.AnalyzeAllTerraformRefs(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing terraform refs: %w", err)
		}

		// Match fields
		matchValidator := &agent.JSONValidator{
			RequiredFields: []string{"matches", "unmatched_tf_fields"},
		}
		result, err := tools.MatchAllResourcesTerraformRefs(ctx, ag, controllers, analysisResult.Results, mapResult.Mappings, resultCache, matchValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("matching terraform refs: %w", err)
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
	matchTerraformRefsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-match terraform refs")
	matchTerraformRefsCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(matchTerraformRefsCmd)
}
