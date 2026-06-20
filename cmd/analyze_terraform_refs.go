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

var analyzeTerraformRefsCmd = &cobra.Command{
	Use:   "analyze-terraform-refs",
	Short: "Analyze Terraform documentation files to identify cross-resource references",
	Long: `Uses the AI agent to semantically analyze each mapped Terraform documentation file
and identify fields that reference other AWS resources via HCL examples, list patterns,
backtick mentions, and argument descriptions.

This tool uses the reference pipeline's own controller-to-Terraform mapping (separate
from the JSON field pipeline) and caches results under "analyze_terraform_refs/".`,
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

		// Invalidate analyze_terraform_refs cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating analyze_terraform_refs cache")
			}
			if err := resultCache.Invalidate("analyze_terraform_refs"); err != nil {
				return fmt.Errorf("invalidating cache: %w", err)
			}
		}

		// Need discovery results for mappings
		log := newCmdLogger()
		ghDiscoverer := discovery.NewGitHubDiscoverer(githubToken, repoCache, log)
		controllers, err := ghDiscoverer.DiscoverControllers(ctx)
		if err != nil {
			return fmt.Errorf("discovering controllers: %w", err)
		}

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

		// Get the reference pipeline's controller-to-Terraform mapping
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}
		mapResult, err := tools.MapAllControllersToTerraformRefs(ctx, ag, controllers, tfResult.Resources, resultCache, mapValidator, 1, log)
		if err != nil {
			return fmt.Errorf("mapping controllers to terraform refs: %w", err)
		}

		// Get the repo directory for reading doc content
		repoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
		if err != nil {
			return fmt.Errorf("ensuring terraform repo: %w", err)
		}

		// Analyze all mapped docs for cross-resource references
		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}
		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"resource_type", "references"},
		}
		result, err := tools.AnalyzeAllTerraformRefs(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing terraform refs: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "RESOURCE TYPE\tREFERENCES\n")
			for key, analysis := range result.Results {
				fmt.Fprintf(w, "%s\t%d\n", key, len(analysis.References))
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d docs\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	analyzeTerraformRefsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-analyze references")
	analyzeTerraformRefsCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(analyzeTerraformRefsCmd)
}
