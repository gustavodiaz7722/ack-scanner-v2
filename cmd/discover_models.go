package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
)

var discoverModelsCmd = &cobra.Command{
	Use:   "discover-models",
	Short: "Discover all AWS Smithy JSON API model files",
	Long: `Clones (or fetches) the aws/aws-sdk-go-v2 repository using sparse checkout
and enumerates all JSON model files under codegen/sdk-codegen/aws-models/.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		refresh, _ := cmd.Flags().GetBool("refresh")

		// Create repo cache
		repoCache, err := cache.NewRepoCache(cacheDir + "/repos")
		if err != nil {
			return fmt.Errorf("creating repo cache: %w", err)
		}

		// Invalidate if refresh requested
		if refresh {
			if verbose {
				fmt.Fprintln(os.Stderr, "refreshing: invalidating aws-sdk-go-v2 discovery cache")
			}
			if err := repoCache.Invalidate("aws", "aws-sdk-go-v2"); err != nil {
				return fmt.Errorf("invalidating aws-sdk-go-v2 repo cache: %w", err)
			}
		}

		// Discover API models
		log := newCmdLogger()
		result, err := tools.DiscoverModels(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering AWS API models: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "SERVICE\tFILE PATH\n")
			for _, model := range result.Models {
				fmt.Fprintf(w, "%s\t%s\n", model.ServiceName, model.FilePath)
			}
			return w.Flush()
		}
	},
}

func init() {
	discoverModelsCmd.Flags().Bool("refresh", false, "Invalidate cache and re-discover AWS API models")
	rootCmd.AddCommand(discoverModelsCmd)
}
