package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/deploy"
)

var teardown bool

// deployCmd is the deploy subcommand for AWS resource management.
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy or tear down AWS resources required by ack-scanner-v2",
	Long: `Creates the IAM role and verifies Bedrock model access needed for scanning.

By default, deploys the required resources:
  - IAM role (ack-scanner-v2-bedrock-role) with bedrock:InvokeModel permission
  - Verifies the configured Bedrock model is accessible in the target region

Use --teardown to remove previously created resources.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if teardown {
			if err := deploy.Teardown(ctx, region); err != nil {
				return fmt.Errorf("teardown failed: %w", err)
			}
			return nil
		}

		result, err := deploy.Deploy(ctx, region, modelID)
		if err != nil {
			return fmt.Errorf("deploy failed: %w", err)
		}

		if result.Created {
			fmt.Println("Successfully deployed AWS resources:")
		} else {
			fmt.Println("Resources already exist (no changes made):")
		}
		fmt.Printf("  Role ARN: %s\n", result.RoleARN)
		fmt.Printf("  Region:   %s\n", result.Region)
		fmt.Printf("  Model ID: %s\n", result.ModelID)

		return nil
	},
}

func init() {
	deployCmd.Flags().BoolVar(&teardown, "teardown", false, "Remove previously created AWS resources")
	rootCmd.AddCommand(deployCmd)
}
