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

var analyzeFieldsCmd = &cobra.Command{
	Use:   "analyze-fields",
	Short: "Analyze Terraform documentation files to identify JSON-accepting fields",
	Long: `Uses the AI agent to semantically analyze each mapped Terraform documentation file
and identify fields that accept JSON-encoded values (JSON documents or IAM policies).`,
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

		// Invalidate analyze_fields cache if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating analyze_fields cache")
			}
			if err := resultCache.Invalidate("analyze_fields"); err != nil {
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

		// Get mapping results (needed to know which docs to analyze)
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}
		mapResult, err := tools.MapAllControllers(ctx, ag, controllers, tfResult.Resources, resultCache, mapValidator, log)
		if err != nil {
			return fmt.Errorf("mapping controllers: %w", err)
		}

		// Get the repo directory for reading doc content
		repoDir, err := repoCache.EnsureRepoSparse("hashicorp", "terraform-provider-aws", []string{"website/docs/r"})
		if err != nil {
			return fmt.Errorf("ensuring terraform repo: %w", err)
		}

		// Analyze all mapped docs
		maxParallel, _ := cmd.Flags().GetInt("max-parallel")
		if maxParallel <= 0 {
			maxParallel = tools.DefaultMaxParallel
		}
		analyzeValidator := &agent.JSONValidator{
			RequiredFields: []string{"resource_type", "json_fields"},
		}
		result, err := tools.AnalyzeAllDocsParallel(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator, maxParallel, log)
		if err != nil {
			return fmt.Errorf("analyzing fields: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "DOC FILE\tJSON FIELDS\n")
			for docPath, analysis := range result.Results {
				fmt.Fprintf(w, "%s\t%d\n", docPath, len(analysis.JSONFields))
			}
			if len(result.Skipped) > 0 {
				fmt.Fprintf(w, "\nSkipped: %d docs\n", len(result.Skipped))
			}
			return w.Flush()
		}
	},
}

func init() {
	analyzeFieldsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-analyze fields")
	analyzeFieldsCmd.Flags().Int("max-parallel", tools.DefaultMaxParallel, "Maximum number of concurrent agent calls")
	rootCmd.AddCommand(analyzeFieldsCmd)
}
