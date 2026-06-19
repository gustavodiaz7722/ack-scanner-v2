package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
)

var discoverControllersCmd = &cobra.Command{
	Use:   "discover-controllers",
	Short: "Discover all ACK controllers and their CRD resources with string fields",
	Long: `Discovers all repositories matching the pattern {service_name}-controller in the
aws-controllers-k8s GitHub organization, parses their CRDs, and extracts string fields.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		refresh, _ := cmd.Flags().GetBool("refresh")

		log := newCmdLogger()

		// Create repo cache
		repoCache, err := cache.NewRepoCache(cacheDir + "/repos")
		if err != nil {
			return fmt.Errorf("creating repo cache: %w", err)
		}

		// Invalidate repo cache if refresh requested
		if refresh {
			log.Info("Invalidating controller discovery cache...")
			if err := repoCache.InvalidateAll(); err != nil {
				return fmt.Errorf("invalidating repo cache: %w", err)
			}
		}

		// Create discoverer
		discoverer := discovery.NewGitHubDiscoverer(githubToken, repoCache, log)

		log.Info("Discovering ACK controllers from GitHub...")
		controllers, err := discoverer.DiscoverControllers(ctx)
		if err != nil {
			return fmt.Errorf("discovering controllers: %w", err)
		}

		totalFields := 0
		for _, ctrl := range controllers {
			for _, res := range ctrl.Resources {
				totalFields += len(res.StringFields)
			}
		}
		log.PhaseComplete(1, "Found %d controllers with %d string fields", len(controllers), totalFields)

		// Format output
		switch output {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(controllers)
		default:
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "SERVICE\tREPO\tRESOURCES\tSTRING FIELDS\n")
			for _, ctrl := range controllers {
				totalFields := 0
				for _, res := range ctrl.Resources {
					totalFields += len(res.StringFields)
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%d\n",
					ctrl.ServiceName, ctrl.RepoName, len(ctrl.Resources), totalFields)
			}
			return w.Flush()
		}
	},
}

func init() {
	discoverControllersCmd.Flags().Bool("refresh", false, "Invalidate cache and re-discover controllers")
	rootCmd.AddCommand(discoverControllersCmd)
}
