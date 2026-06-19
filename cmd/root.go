// Package cmd implements the CLI commands for ack-scanner-v2.
package cmd

import (
	"fmt"
	"os"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	githubToken string
	cacheDir    string
	verbose     bool
	output      string
	modelID     string
	region      string
	invalidate  string
)

// rootCmd is the base command for ack-scanner-v2.
var rootCmd = &cobra.Command{
	Use:   "ack-scanner-v2",
	Short: "ACK Scanner v2 - Agentic gap detection for ACK controllers",
	Long: `ack-scanner-v2 uses AWS Bedrock (Claude) to semantically analyze ACK controller
fields and identify those needing is_document or is_iam_policy annotations.

It replaces v1's regex-based approach with an agentic architecture that
achieves higher accuracy through semantic understanding of Terraform documentation.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Resolve GitHub token from flag or environment variable
		if githubToken == "" {
			githubToken = os.Getenv("GITHUB_TOKEN")
		}

		// Set default cache directory if not specified
		if cacheDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("unable to determine home directory: %w", err)
			}
			cacheDir = home + "/.ack-scanner-v2/cache"
		}

		// Warn about unauthenticated GitHub access
		if githubToken == "" && verbose {
			fmt.Fprintln(os.Stderr, "warning: no GitHub token provided; using unauthenticated access (rate limits apply)")
		}

		// Handle --invalidate flag
		if invalidate != "" {
			resultCache, err := cache.NewResultCache(cacheDir)
			if err != nil {
				return fmt.Errorf("creating result cache: %w", err)
			}

			if invalidate == "all" {
				if verbose {
					fmt.Fprintln(os.Stderr, "invalidating all cached results")
				}
				if err := resultCache.InvalidateAll(); err != nil {
					return fmt.Errorf("invalidating all cache: %w", err)
				}
				fmt.Fprintln(os.Stderr, "all cached results invalidated")
			} else {
				if verbose {
					fmt.Fprintf(os.Stderr, "invalidating cache for tool: %s\n", invalidate)
				}
				if err := resultCache.Invalidate(invalidate); err != nil {
					return fmt.Errorf("invalidating cache for %s: %w", invalidate, err)
				}
				fmt.Fprintf(os.Stderr, "cache invalidated for tool: %s\n", invalidate)
			}

			// If no subcommand is being run (just --invalidate on root), exit cleanly
			if cmd.Name() == "ack-scanner-v2" {
				os.Exit(0)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&githubToken, "github-token", "", "GitHub personal access token (or set GITHUB_TOKEN env var)")
	rootCmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: $HOME/.ack-scanner-v2/cache)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable detailed progress logging to stderr")
	rootCmd.PersistentFlags().StringVar(&output, "output", "table", "Output format: table, json, markdown")
	rootCmd.PersistentFlags().StringVar(&modelID, "model-id", "us.anthropic.claude-sonnet-4-20250514-v1:0", "AWS Bedrock model ID to use")
	rootCmd.PersistentFlags().StringVar(&region, "region", "us-east-1", "AWS region for Bedrock")
	rootCmd.PersistentFlags().StringVar(&invalidate, "invalidate", "", "Invalidate cache for specified tool name (use 'all' to invalidate everything)")
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
