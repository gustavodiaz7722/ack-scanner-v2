package agent

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// NewBedrockClient creates a new Bedrock runtime client configured with the given region.
// It detects missing credentials early and returns a descriptive error if credentials
// are not configured.
func NewBedrockClient(ctx context.Context, region string) (*bedrockruntime.Client, error) {
	// Load AWS configuration
	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("agent: failed to load AWS configuration: %w. "+
			"Please configure credentials via environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), "+
			"AWS profiles (~/.aws/credentials), or IAM role. "+
			"Required permissions: bedrock:InvokeModel on the target model resource", err)
	}

	// Attempt to retrieve credentials early to detect missing credentials
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent: AWS credentials not configured: %w. "+
			"Please configure credentials via environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), "+
			"AWS profiles (~/.aws/credentials), or IAM role. "+
			"Required permissions: bedrock:InvokeModel on the target model resource", err)
	}

	if creds.AccessKeyID == "" {
		return nil, ErrMissingCredentials
	}

	client := bedrockruntime.NewFromConfig(cfg)
	return client, nil
}
