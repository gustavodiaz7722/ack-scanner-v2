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

const matchTerraformRefsTool = "match_terraform_refs"

// TerraformRefFieldMatch maps a Terraform reference field to an ACK CRD field.
type TerraformRefFieldMatch struct {
	TFFieldName    string   `json:"terraform_field_name"`
	ACKFieldName   string   `json:"ack_field_name"`
	ACKFieldPath   string   `json:"ack_field_path"`
	TargetResource string   `json:"target_resource"`
	ResolutionAttr string   `json:"resolution_attr"`
	Confidence     float64  `json:"confidence"`
	Alternatives   []string `json:"alternatives,omitempty"`
}

// MatchTerraformRefsOutput is the agent's cross-reference result for a single
// resource matching Terraform doc references against ACK fields.
type MatchTerraformRefsOutput struct {
	Matches   []TerraformRefFieldMatch `json:"matches"`
	Unmatched []string                 `json:"unmatched_tf_fields"`
}

// MatchAllTerraformRefsOutput is the aggregated match result for all resources.
type MatchAllTerraformRefsOutput struct {
	Results map[string]*MatchTerraformRefsOutput `json:"results"`
	Skipped []string                             `json:"skipped,omitempty"`
}

// TerraformRefsMatchConfig returns the framework.MatchConfig for the
// ACK-to-Terraform-reference field matching tool.
func TerraformRefsMatchConfig() framework.MatchConfig[TerraformReferenceInfo, MatchTerraformRefsOutput] {
	return framework.MatchConfig[TerraformReferenceInfo, MatchTerraformRefsOutput]{
		ToolName:     matchTerraformRefsTool,
		BuildPrompt:  buildMatchTerraformRefsPrompt,
		ParseResult:  parseMatchTerraformRefsResponse,
		ItemKey:      matchTerraformRefsItemKey,
		InputParams:  buildMatchTerraformRefsInputParams,
		FilterFields: filterFieldsForReferenceMatching,
	}
}

// MatchResourceTerraformRefs invokes the agent to cross-reference Terraform
// doc-discovered reference fields against a single ACK resource's string fields.
// Fields already annotated as is_document, is_iam_policy, or having an existing
// reference config are excluded from matching.
func MatchResourceTerraformRefs(
	ctx context.Context,
	ag *agent.Agent,
	resource types.ResourceInfo,
	tfRefs []TerraformReferenceInfo,
	serviceName string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*MatchTerraformRefsOutput, error) {
	l := resolveLogger(log)
	config := TerraformRefsMatchConfig()

	result, err := framework.MatchOne(ctx, config, ag, resource, tfRefs, serviceName, resultCache, validator, l)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// MatchAllResourcesTerraformRefs orchestrates matching all resources across all
// controllers against Terraform doc reference signals. For each controller, it
// collects the TF reference signals from analysis results corresponding to the
// controller's mapped Terraform docs, then invokes per-resource agent calls.
func MatchAllResourcesTerraformRefs(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	analysisResults map[string]*AnalyzeTerraformRefsOutput,
	mappings []types.ControllerMapping,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MatchAllTerraformRefsOutput, error) {
	l := resolveLogger(log)
	config := TerraformRefsMatchConfig()

	// Build a lookup from doc item key to its analyzed TF references
	docRefsMap := make(map[string][]TerraformReferenceInfo)
	for key, analysis := range analysisResults {
		if analysis != nil {
			docRefsMap[key] = analysis.References
		}
	}

	// Build sourceData: service name → aggregated TF reference signals
	sourceData := make(map[string][]TerraformReferenceInfo)
	// Build serviceMappings: service name → list of doc item keys
	serviceMappings := make(map[string][]string)

	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			docKey := deriveTerraformRefsItemKey(entry.DocFilePath)
			serviceMappings[mapping.ServiceName] = append(serviceMappings[mapping.ServiceName], docKey)
		}
	}

	// Populate sourceData from docRefsMap keyed by doc item key
	for key, refs := range docRefsMap {
		sourceData[key] = refs
	}

	l.Info("%s: processing %d controllers with %d analysis results",
		matchTerraformRefsTool, len(controllers), len(analysisResults))

	frameworkResult, err := framework.MatchAll(ctx, config, ag, controllers, sourceData, serviceMappings, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type
	output := &MatchAllTerraformRefsOutput{
		Results: make(map[string]*MatchTerraformRefsOutput, len(frameworkResult.Results)),
		Skipped: frameworkResult.Skipped,
	}
	for key, result := range frameworkResult.Results {
		r := result // avoid aliasing
		output.Results[key] = &r
	}

	return output, nil
}

// buildMatchTerraformRefsPrompt constructs the prompt sent to the agent for
// matching Terraform doc reference signals against a single ACK resource's fields.
func buildMatchTerraformRefsPrompt(resource types.ResourceInfo, tfRefs []TerraformReferenceInfo, serviceName string) string {
	var sb strings.Builder

	sb.WriteString("You are cross-referencing Terraform documentation-discovered cross-resource references against ACK (AWS Controllers for Kubernetes) CRD string fields to determine which ACK fields should have `references:` configuration.\n\n")

	sb.WriteString("## ACK Resource\n")
	fmt.Fprintf(&sb, "Service: %s\n", serviceName)
	fmt.Fprintf(&sb, "Resource Kind: %s\n", resource.Kind)
	sb.WriteString("String Fields (candidates for reference matching):\n")
	for _, field := range resource.StringFields {
		fmt.Fprintf(&sb, "  - Name: %s, Path: %s, JSON Tag: %s\n", field.Name, field.Path, field.JSONTag)
	}

	sb.WriteString("\n## Terraform Reference Signals\n")
	sb.WriteString("These fields have been identified from Terraform documentation as referencing other AWS resources:\n")
	for _, ref := range tfRefs {
		fmt.Fprintf(&sb, "  - Field: %s → Target: %s (resolution: %s, signal: %s, confidence: %.2f)\n",
			ref.FieldName, ref.TargetResource, ref.ResolutionAttr, ref.SignalType, ref.Confidence)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Match each Terraform reference field to its corresponding ACK CRD string field.\n")
	sb.WriteString("Use semantic understanding to resolve naming convention differences between Terraform snake_case and ACK PascalCase:\n")
	sb.WriteString("- Terraform `volume_id` → ACK `VolumeID` or `EBSVolumeID`\n")
	sb.WriteString("- Terraform `subnet_ids` → ACK `SubnetIDs` or `SubnetIdentifiers`\n")
	sb.WriteString("- Terraform `role_arn` → ACK `RoleARN` or `ServiceRoleARN`\n")
	sb.WriteString("- Terraform `kms_key_id` → ACK `KMSKeyID` or `EncryptionKeyID`\n\n")
	sb.WriteString("For each match, provide:\n")
	sb.WriteString("- The Terraform field name\n")
	sb.WriteString("- The matched ACK field name and its full path\n")
	sb.WriteString("- The target Terraform resource type (from the reference signal)\n")
	sb.WriteString("- The resolution attribute (.id, .arn, or .name)\n")
	sb.WriteString("- A confidence score (0.0 to 1.0)\n")
	sb.WriteString("- Alternatives if multiple ACK fields could match\n\n")
	sb.WriteString("If a Terraform reference field has no corresponding ACK field, include it in the unmatched list.\n")
	sb.WriteString("Every Terraform reference field must appear either in matches or in unmatched_tf_fields.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"matches":[{"terraform_field_name":"<name of the TF reference field>","ack_field_name":"<name of the matched ACK field>","ack_field_path":"<full dot-separated path of the ACK field>","target_resource":"<referenced Terraform resource type, e.g. aws_ebs_volume>","resolution_attr":"<.id, .arn, or .name>","confidence":<0.0 to 1.0>,"alternatives":["<optional alternative ACK field names>"]}],"unmatched_tf_fields":["<TF reference field names with no ACK match>"]}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseMatchTerraformRefsResponse parses the agent's JSON response into a
// MatchTerraformRefsOutput.
func parseMatchTerraformRefsResponse(response string) (MatchTerraformRefsOutput, error) {
	var output MatchTerraformRefsOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return MatchTerraformRefsOutput{}, fmt.Errorf("parsing match terraform refs response: %w", err)
	}
	return output, nil
}

// matchTerraformRefsItemKey returns the cache key for a resource.
func matchTerraformRefsItemKey(serviceName string, resource types.ResourceInfo) string {
	return serviceName + "_" + resource.Kind
}

// buildMatchTerraformRefsInputParams creates the input parameters for cache hashing.
func buildMatchTerraformRefsInputParams(resource types.ResourceInfo, tfRefs []TerraformReferenceInfo, serviceName string) map[string]any {
	fieldNames := make([]string, 0, len(resource.StringFields))
	for _, f := range resource.StringFields {
		fieldNames = append(fieldNames, f.Name)
	}

	tfFieldNames := make([]string, 0, len(tfRefs))
	for _, ref := range tfRefs {
		tfFieldNames = append(tfFieldNames, ref.FieldName)
	}

	return map[string]any{
		"service_name":    serviceName,
		"resource_kind":   resource.Kind,
		"ack_field_count": len(resource.StringFields),
		"tf_ref_count":    len(tfRefs),
		"ack_field_names": fieldNames,
		"tf_field_names":  tfFieldNames,
	}
}

// ValidateMatchTerraformRefsOutput checks that a MatchTerraformRefsOutput
// conforms to the expected schema. Each match entry must have terraform_field_name,
// ack_field_name, ack_field_path, target_resource, resolution_attr, and confidence
// (between 0 and 1). If validACKFields is provided, ack_field_name must be in the set.
func ValidateMatchTerraformRefsOutput(output *MatchTerraformRefsOutput, validACKFields map[string]bool) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	validResolutionAttrs := map[string]bool{
		".id":   true,
		".arn":  true,
		".name": true,
	}

	for i, match := range output.Matches {
		if match.TFFieldName == "" {
			return fmt.Errorf("matches[%d]: terraform_field_name is empty", i)
		}
		if match.ACKFieldName == "" {
			return fmt.Errorf("matches[%d]: ack_field_name is empty", i)
		}
		if match.ACKFieldPath == "" {
			return fmt.Errorf("matches[%d]: ack_field_path is empty", i)
		}
		if match.TargetResource == "" {
			return fmt.Errorf("matches[%d]: target_resource is empty", i)
		}
		if !validResolutionAttrs[match.ResolutionAttr] {
			return fmt.Errorf("matches[%d]: resolution_attr must be \".id\", \".arn\", or \".name\", got %q", i, match.ResolutionAttr)
		}
		if match.Confidence < 0 || match.Confidence > 1 {
			return fmt.Errorf("matches[%d]: confidence must be between 0 and 1, got %f", i, match.Confidence)
		}
		if validACKFields != nil && !validACKFields[match.ACKFieldName] {
			return fmt.Errorf("matches[%d]: ack_field_name %q is not a valid ACK field", i, match.ACKFieldName)
		}
	}

	return nil
}

// ValidateMatchTerraformRefsCompleteness checks that every TF reference field
// provided to matching appears either in the matches list or in the unmatched
// list — none are silently dropped.
func ValidateMatchTerraformRefsCompleteness(output *MatchTerraformRefsOutput, tfRefs []TerraformReferenceInfo) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	// Build sets of TF field names that appear in matches and unmatched
	matchedTFFields := make(map[string]bool)
	for _, match := range output.Matches {
		matchedTFFields[match.TFFieldName] = true
	}
	unmatchedTFFields := make(map[string]bool)
	for _, fieldName := range output.Unmatched {
		unmatchedTFFields[fieldName] = true
	}

	// Every input TF reference field must appear in one of the two sets
	for _, tfRef := range tfRefs {
		if !matchedTFFields[tfRef.FieldName] && !unmatchedTFFields[tfRef.FieldName] {
			return fmt.Errorf("TF reference field %q is neither matched nor listed as unmatched", tfRef.FieldName)
		}
	}

	return nil
}
