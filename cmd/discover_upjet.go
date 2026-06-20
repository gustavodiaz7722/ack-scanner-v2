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

var discoverUpjetCmd = &cobra.Command{
	Use:   "discover-upjet",
	Short: "Discover all Upjet/Crossplane AWS provider resource configuration files",
	Long: `Clones (or fetches) the upbound/provider-aws repository using sparse
checkout and enumerates all config.go files under config/*/.`,
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
				fmt.Fprintln(os.Stderr, "refreshing: invalidating upjet discovery cache")
			}
			if err := repoCache.Invalidate("upbound", "provider-aws"); err != nil {
				return fmt.Errorf("invalidating upjet repo cache: %w", err)
			}
		}

		// Discover Upjet configs
		log := newCmdLogger()
		result, err := tools.DiscoverUpjet(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering upjet configs: %w", err)
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
			for _, cfg := range result.Configs {
				fmt.Fprintf(w, "%s\t%s\n", cfg.ServiceName, cfg.FilePath)
			}
			return w.Flush()
		}
	},
}

func init() {
	discoverUpjetCmd.Flags().Bool("refresh", false, "Invalidate cache and re-discover Upjet configs")
	rootCmd.AddCommand(discoverUpjetCmd)
}
