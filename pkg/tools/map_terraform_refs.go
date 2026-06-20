package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const mapTerraformRefsTool = "map_terraform_refs"

// TerraformRefMapping maps an ACK controller to Terraform doc files that
// contain cross-resource reference patterns. This is separate from the JSON
// field pipeline's ControllerMapping which maps for document/IAM field analysis.
type TerraformRefMapping struct {
	ServiceName   string                     `json:"service_name"`
	TFDocFiles    []TerraformRefMappingEntry `json:"terraform_doc_files"`
	NoMatchReason string                     `json:"no_match_reason,omitempty"`
}

// TerraformRefMappingEntry is a single controller-to-TF-doc association in the
// reference detection pipeline.
type TerraformRefMappingEntry struct {
	TFResourceType string  `json:"terraform_resource_type"`
	DocFilePath    string  `json:"doc_file_path"`
	Confidence     float64 `json:"confidence"`
}

// MapTerraformRefsOutput wraps the agent's mapping response for a single controller.
type MapTerraformRefsOutput struct {
	Mapping TerraformRefMapping `json:"mapping"`
}

// MapAllTerraformRefsOutput is the aggregated mapping result for all controllers.
type MapAllTerraformRefsOutput struct {
	Mappings []TerraformRefMapping `json:"mappings"`
	Skipped  []string              `json:"skipped,omitempty"`
}

// TerraformRefsMappingConfig returns the framework.MappingConfig for the
// controller-to-Terraform reference mapping tool.
func TerraformRefsMappingConfig() framework.MappingConfig[string, TerraformRefMapping] {
	return framework.MappingConfig[string, TerraformRefMapping]{
		ToolName:    mapTerraformRefsTool,
		BuildPrompt: buildMapTerraformRefsPrompt,
		ParseResult: parseTerraformRefsResponse,
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: buildMapTerraformRefsInputParams,
	}
}

// MapControllerToTerraformRefs invokes the agent to map a single ACK controller
// to Terraform documentation files that contain cross-resource reference patterns.
func MapControllerToTerraformRefs(
	ctx context.Context,
	ag *agent.Agent,
	controller types.ControllerInfo,
	tfDocFiles []string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*TerraformRefMapping, error) {
	l := resolveLogger(log)
	config := TerraformRefsMappingConfig()

	result, err := framework.MapOne(ctx, config, ag, controller, tfDocFiles, resultCache, validator, l)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// MapAllControllersToTerraformRefs orchestrates mapping all controllers to
// Terraform documentation files for cross-resource reference detection.
func MapAllControllersToTerraformRefs(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	tfDocFiles []string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MapAllTerraformRefsOutput, error) {
	l := resolveLogger(log)
	config := TerraformRefsMappingConfig()

	frameworkResult, err := framework.MapAll(ctx, config, ag, controllers, tfDocFiles, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type, preserving controller order
	output := &MapAllTerraformRefsOutput{
		Skipped: frameworkResult.Skipped,
	}
	for _, ctrl := range controllers {
		if mapping, ok := frameworkResult.Results[ctrl.ServiceName]; ok {
			output.Mappings = append(output.Mappings, mapping)
		}
	}

	return output, nil
}

// buildMapTerraformRefsPrompt constructs the prompt sent to the agent for mapping
// a single controller to Terraform documentation files that contain cross-resource
// reference patterns.
func buildMapTerraformRefsPrompt(controller types.ControllerInfo, tfDocFiles []string) string {
	var sb strings.Builder

	sb.WriteString("You are mapping an ACK (AWS Controllers for Kubernetes) controller to Terraform AWS provider documentation files that are likely to contain cross-resource reference patterns.\n\n")

	sb.WriteString("## Goal\n")
	sb.WriteString("Identify Terraform documentation files for this ACK controller's service that contain fields referencing OTHER AWS resources. ")
	sb.WriteString("These are fields like `vpc_id`, `subnet_ids`, `role_arn`, `kms_key_id` — where the value is an identifier (ARN, ID, or name) pointing to a different AWS resource type.\n\n")

	sb.WriteString("## ACK Controller\n")
	fmt.Fprintf(&sb, "Service Name: %s\n", controller.ServiceName)
	sb.WriteString("Resource Kinds:\n")
	for _, r := range controller.Resources {
		fmt.Fprintf(&sb, "  - %s\n", r.Kind)
	}

	sb.WriteString("\n## Terraform Documentation Files\n")
	sb.WriteString("Below is the complete list of Terraform AWS provider resource documentation filenames:\n")
	for _, docFile := range tfDocFiles {
		fmt.Fprintf(&sb, "  - %s\n", docFile)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Map this ACK controller to the Terraform documentation files that:\n")
	sb.WriteString("1. Correspond to the same AWS service and resources as this ACK controller\n")
	sb.WriteString("2. Are likely to contain cross-resource reference patterns such as:\n")
	sb.WriteString("   - HCL examples with `field = aws_other_resource.name.id` patterns\n")
	sb.WriteString("   - Argument descriptions mentioning other AWS resource types (e.g., `aws_iam_role`, `aws_subnet`)\n")
	sb.WriteString("   - Fields ending in `_arn`, `_id`, `_name` that reference other resources\n\n")
	sb.WriteString("Use semantic understanding to resolve naming differences between ACK and Terraform ")
	sb.WriteString("(e.g., ACK 'applicationautoscaling' → Terraform 'appautoscaling').\n")
	sb.WriteString("If there is no corresponding Terraform documentation, leave terraform_doc_files as an empty array and provide a no_match_reason.\n")
	sb.WriteString("Only include Terraform resources that genuinely correspond to this ACK controller's service.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"mapping":{"service_name":"<the ACK controller service name>","terraform_doc_files":[{"terraform_resource_type":"<e.g. aws_elasticache_cluster>","doc_file_path":"<exact path from the list above>","confidence":<0.0 to 1.0>}],"no_match_reason":"<optional: reason if no TF docs match>"}}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseTerraformRefsResponse parses the agent's JSON response into a TerraformRefMapping.
func parseTerraformRefsResponse(response string) (TerraformRefMapping, error) {
	var output MapTerraformRefsOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return TerraformRefMapping{}, fmt.Errorf("parsing terraform refs mapping response: %w", err)
	}
	return output.Mapping, nil
}

// buildMapTerraformRefsInputParams creates the input parameters used for cache hashing.
func buildMapTerraformRefsInputParams(controller types.ControllerInfo, tfDocFiles []string) map[string]any {
	kinds := make([]string, 0, len(controller.Resources))
	for _, r := range controller.Resources {
		kinds = append(kinds, r.Kind)
	}

	return map[string]any{
		"service_name":   controller.ServiceName,
		"resource_kinds": kinds,
		"tf_doc_count":   len(tfDocFiles),
	}
}
