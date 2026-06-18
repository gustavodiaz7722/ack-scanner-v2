// Package deploy manages AWS resource deployment for ack-scanner-v2.
// It creates the IAM role and verifies Bedrock model access needed for scanning.
package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

const (
	// RoleName is the IAM role created for ack-scanner-v2 Bedrock access.
	RoleName = "ack-scanner-v2-bedrock-role"

	// PolicyName is the inline policy attached to the role.
	PolicyName = "ack-scanner-v2-bedrock-invoke"
)

// DeployResult contains the outcome of a Deploy operation.
type DeployResult struct {
	RoleARN string
	Region  string
	ModelID string
	Created bool // true if newly created, false if already existed
}

// Deploy creates the required IAM role with Bedrock invoke permissions
// and verifies the specified model is accessible in the given region.
// It is idempotent: if the role already exists, it skips creation.
func Deploy(ctx context.Context, region, modelID string) (*DeployResult, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	// Get account ID for trust policy
	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, wrapPermissionError(err, "sts:GetCallerIdentity")
	}
	accountID := aws.ToString(identity.Account)

	iamClient := iam.NewFromConfig(cfg)

	// Check if the role already exists
	getRoleOutput, err := iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(RoleName),
	})
	if err == nil {
		// Role exists — verify model and return
		roleARN := aws.ToString(getRoleOutput.Role.Arn)
		if err := verifyModelAccess(ctx, cfg, modelID); err != nil {
			return nil, err
		}
		return &DeployResult{
			RoleARN: roleARN,
			Region:  region,
			ModelID: modelID,
			Created: false,
		}, nil
	}

	// If error is not NoSuchEntity, it's an unexpected error
	var noSuchEntity *iamtypes.NoSuchEntityException
	if !errors.As(err, &noSuchEntity) {
		return nil, wrapPermissionError(err, "iam:GetRole")
	}

	// Role doesn't exist — create it
	trustPolicy, err := buildTrustPolicy(accountID)
	if err != nil {
		return nil, fmt.Errorf("building trust policy: %w", err)
	}

	createRoleOutput, err := iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(RoleName),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
		Description:              aws.String("IAM role for ack-scanner-v2 Bedrock model invocation"),
		Tags: []iamtypes.Tag{
			{Key: aws.String("ManagedBy"), Value: aws.String("ack-scanner-v2")},
		},
	})
	if err != nil {
		return nil, wrapPermissionError(err, "iam:CreateRole")
	}

	// Attach inline policy for bedrock:InvokeModel
	permissionPolicy := buildPermissionPolicy()
	_, err = iamClient.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       aws.String(RoleName),
		PolicyName:     aws.String(PolicyName),
		PolicyDocument: aws.String(permissionPolicy),
	})
	if err != nil {
		return nil, wrapPermissionError(err, "iam:PutRolePolicy")
	}

	// Verify model accessibility
	if err := verifyModelAccess(ctx, cfg, modelID); err != nil {
		return nil, err
	}

	return &DeployResult{
		RoleARN: aws.ToString(createRoleOutput.Role.Arn),
		Region:  region,
		ModelID: modelID,
		Created: true,
	}, nil
}

// Teardown removes the IAM role and its inline policy created by Deploy.
// It is idempotent: if the role doesn't exist, it reports nothing to tear down.
func Teardown(ctx context.Context, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Check if the role exists
	_, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(RoleName),
	})
	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if errors.As(err, &noSuchEntity) {
			fmt.Println("Nothing to tear down: role does not exist.")
			return nil
		}
		return wrapPermissionError(err, "iam:GetRole")
	}

	// Delete inline policy (ignore NoSuchEntity if policy was already removed)
	_, err = iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
		RoleName:   aws.String(RoleName),
		PolicyName: aws.String(PolicyName),
	})
	if err != nil {
		var noSuchEntity *iamtypes.NoSuchEntityException
		if !errors.As(err, &noSuchEntity) {
			return wrapPermissionError(err, "iam:DeleteRolePolicy")
		}
	}

	// Delete the role
	_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(RoleName),
	})
	if err != nil {
		return wrapPermissionError(err, "iam:DeleteRole")
	}

	fmt.Printf("Torn down: deleted role %s and policy %s\n", RoleName, PolicyName)
	return nil
}

// verifyModelAccess checks that the specified model is available in the region.
func verifyModelAccess(ctx context.Context, cfg aws.Config, modelID string) error {
	bedrockClient := bedrock.NewFromConfig(cfg)

	_, err := bedrockClient.GetFoundationModel(ctx, &bedrock.GetFoundationModelInput{
		ModelIdentifier: aws.String(modelID),
	})
	if err != nil {
		return fmt.Errorf("model %q is not accessible in region %s: %w\n"+
			"Ensure the model is enabled in your account via the AWS Bedrock console", modelID, cfg.Region, err)
	}
	return nil
}

// buildTrustPolicy creates the IAM trust policy allowing the account root to assume the role.
func buildTrustPolicy(accountID string) (string, error) {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect": "Allow",
				"Principal": map[string]string{
					"AWS": fmt.Sprintf("arn:aws:iam::%s:root", accountID),
				},
				"Action": "sts:AssumeRole",
			},
		},
	}
	b, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// buildPermissionPolicy creates the inline policy granting bedrock:InvokeModel.
func buildPermissionPolicy() string {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":   "Allow",
				"Action":   "bedrock:InvokeModel",
				"Resource": "arn:aws:bedrock:*:*:foundation-model/*",
			},
		},
	}
	b, _ := json.Marshal(policy)
	return string(b)
}

// wrapPermissionError wraps an AWS error with a descriptive message about
// required IAM permissions when the error indicates access denial.
func wrapPermissionError(err error, action string) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "AccessDenied" || code == "AccessDeniedException" ||
			code == "UnauthorizedAccess" || code == "AuthorizationError" {
			return fmt.Errorf("insufficient permissions for %s: %w\n"+
				"Required IAM permissions:\n"+
				"  - sts:GetCallerIdentity\n"+
				"  - iam:GetRole\n"+
				"  - iam:CreateRole\n"+
				"  - iam:PutRolePolicy\n"+
				"  - iam:DeleteRole\n"+
				"  - iam:DeleteRolePolicy\n"+
				"  - bedrock:GetFoundationModel",
				action, err)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
