package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/tools"
)

var discoverTerraformCmd = &cobra.Command{
	Use:   "discover-terraform",
	Short: "Discover all Terraform AWS provider resources and their documentation files",
	Long: `Clones (or fetches) the hashicorp/terraform-provider-aws repository using sparse
checkout and enumerates all resource documentation files under website/docs/r/.`,
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
				fmt.Fprintln(os.Stderr, "refreshing: invalidating terraform discovery cache")
			}
			if err := repoCache.Invalidate("hashicorp", "terraform-provider-aws"); err != nil {
				return fmt.Errorf("invalidating terraform repo cache: %w", err)
			}
		}

		// Discover Terraform resources
		log := newCmdLogger()
		result, err := tools.DiscoverTerraform(ctx, repoCache, log)
		if err != nil {
			return fmt.Errorf("discovering terraform resources: %w", err)
		}

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "SERVICE\tRESOURCE TYPE\tDOC FILE\n")
			for _, docFile := range result.Resources {
				service, resourceType, _ := tools.ExtractTerraformFilenameComponents(filepath.Base(docFile))
				fmt.Fprintf(w, "%s\t%s\t%s\n", service, resourceType, docFile)
			}
			return w.Flush()
		}
	},
}

func init() {
	discoverTerraformCmd.Flags().Bool("refresh", false, "Invalidate cache and re-discover Terraform resources")
	rootCmd.AddCommand(discoverTerraformCmd)
}
