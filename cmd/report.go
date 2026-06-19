package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/reporter"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate a gap report identifying ACK fields needing annotation",
	Long: `Generates a comprehensive report of all ACK controller fields that should be
annotated as is_document or is_iam_policy, based on cross-referencing with
Terraform documentation analysis.`,
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

		// Invalidate report-related data if refresh
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating match_fields cache for report regeneration")
			}
			if err := resultCache.Invalidate("match_fields"); err != nil {
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

		// Map controllers
		mapValidator := &agent.JSONValidator{
			RequiredFields: []string{"mapping"},
		}
		mapResult, err := tools.MapAllControllers(ctx, ag, controllers, tfResult.Resources, resultCache, mapValidator, log)
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
		analysisResult, err := tools.AnalyzeAllDocs(ctx, ag, mapResult.Mappings, repoDir, resultCache, analyzeValidator, log)
		if err != nil {
			return fmt.Errorf("analyzing fields: %w", err)
		}

		// Match fields
		matchValidator := &agent.JSONValidator{
			RequiredFields: []string{"matches", "unmatched_tf_fields"},
		}
		matchResult, err := tools.MatchAllResources(ctx, ag, controllers, analysisResult.Results, mapResult.Mappings, resultCache, matchValidator, log)
		if err != nil {
			return fmt.Errorf("matching fields: %w", err)
		}

		// Load generator configs for each controller
		generatorConfigs := make(map[string]*parser.GeneratorConfig)
		for _, ctrl := range controllers {
			repoDir, err := repoCache.EnsureRepo(discovery.ACKOrg, ctrl.RepoName)
			if err != nil {
				if verbose {
					fmt.Fprintf(os.Stderr, "warning: could not access repo %s: %v\n", ctrl.RepoName, err)
				}
				continue
			}
			genPath := filepath.Join(repoDir, "generator.yaml")
			genConfig, err := parser.ParseGeneratorConfig(genPath)
			if err != nil {
				if verbose {
					fmt.Fprintf(os.Stderr, "warning: could not parse generator.yaml for %s: %v\n", ctrl.ServiceName, err)
				}
				continue
			}
			generatorConfigs[ctrl.ServiceName] = genConfig
		}

		// Generate report
		report := tools.GenerateReport(matchResult.Results, controllers, generatorConfigs, log)

		// Format output using reporter
		return reporter.Format(report, output, os.Stdout)
	},
}

func init() {
	reportCmd.Flags().Bool("refresh", false, "Invalidate cache and regenerate report")
	rootCmd.AddCommand(reportCmd)
}
