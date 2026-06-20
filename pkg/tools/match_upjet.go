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

const matchUpjetTool = "match_upjet"

// UpjetFieldMatch maps an Upjet reference field to its corresponding ACK CRD field.
type UpjetFieldMatch struct {
	UpjetFieldName string   `json:"upjet_field_name"`
	ACKFieldName   string   `json:"ack_field_name"`
	ACKFieldPath   string   `json:"ack_field_path"`
	TargetResource string   `json:"target_resource"` // e.g., "aws_kms_key"
	IsAmbiguous    bool     `json:"is_ambiguous"`
	Confidence     float64  `json:"confidence"`
	Alternatives   []string `json:"alternatives,omitempty"`
}

// MatchUpjetOutput is the agent's match result for a single ACK resource
// against Upjet reference fields.
type MatchUpjetOutput struct {
	Matches   []UpjetFieldMatch `json:"matches"`
	Unmatched []string          `json:"unmatched_upjet_fields"`
}

// MatchAllUpjetOutput is the aggregated match result for all resources.
type MatchAllUpjetOutput struct {
	Results map[string]*MatchUpjetOutput `json:"results"`
	Skipped []string                     `json:"skipped,omitempty"`
}

// MatchResourceUpjet invokes the agent to cross-reference Upjet reference fields
// against an ACK resource's string fields. Each call processes a single resource
// to minimize context. Fields already annotated as is_document, is_iam_policy,
// or having existing references are excluded before matching.
func MatchResourceUpjet(
	ctx context.Context,
	ag *agent.Agent,
	resource types.ResourceInfo,
	upjetRefs []UpjetReferenceInfo,
	serviceName string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*MatchUpjetOutput, error) {
	l := resolveLogger(log)

	config := buildMatchUpjetConfig()

	result, err := framework.MatchOne(ctx, config, ag, resource, upjetRefs, serviceName, resultCache, validator, l)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// MatchAllResourcesUpjet orchestrates matching all resources across all controllers
// against Upjet reference data with bounded concurrency. It uses the framework's
// MatchAll function to handle caching, validation, retry, and aggregation.
func MatchAllResourcesUpjet(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	analysisResults map[string]*AnalyzeUpjetOutput,
	mappings []UpjetMapping,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MatchAllUpjetOutput, error) {
	l := resolveLogger(log)

	config := buildMatchUpjetConfig()

	// Build sourceData: map from upjet service key → list of UpjetReferenceInfo
	sourceData := make(map[string][]UpjetReferenceInfo)
	for key, analysis := range analysisResults {
		if analysis != nil && len(analysis.References) > 0 {
			sourceData[key] = analysis.References
		}
	}

	// Build serviceMappings: map ACK service name → list of upjet service keys
	serviceMappings := make(map[string][]string)
	for _, mapping := range mappings {
		for _, entry := range mapping.UpjetConfigs {
			// The analysis results are keyed by the upjet service name
			serviceMappings[mapping.ServiceName] = append(serviceMappings[mapping.ServiceName], entry.UpjetService)
		}
	}

	frameworkResult, err := framework.MatchAll(ctx, config, ag, controllers, sourceData, serviceMappings, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type
	output := &MatchAllUpjetOutput{
		Results: make(map[string]*MatchUpjetOutput, len(frameworkResult.Results)),
		Skipped: frameworkResult.Skipped,
	}

	for key, result := range frameworkResult.Results {
		r := result // avoid aliasing loop variable
		output.Results[key] = &r
	}

	return output, nil
}

// buildMatchUpjetConfig returns the framework.MatchConfig for ACK-to-Upjet field matching.
func buildMatchUpjetConfig() framework.MatchConfig[UpjetReferenceInfo, MatchUpjetOutput] {
	return framework.MatchConfig[UpjetReferenceInfo, MatchUpjetOutput]{
		ToolName:     matchUpjetTool,
		BuildPrompt:  buildMatchUpjetPrompt,
		ParseResult:  parseMatchUpjetResult,
		ItemKey:      matchUpjetItemKey,
		InputParams:  buildMatchUpjetInputParams,
		FilterFields: filterFieldsForReferenceMatching,
	}
}

// MatchUpjetConfig returns the framework.MatchConfig for external use (e.g., tests).
func MatchUpjetConfig() framework.MatchConfig[UpjetReferenceInfo, MatchUpjetOutput] {
	return buildMatchUpjetConfig()
}

// filterFieldsForReferenceMatching excludes fields that already have annotations
// (is_document, is_iam_policy, or has_reference) from the matching process.
// It also excludes the "Name" field which is a resource's own identifier, never
// a cross-resource reference.
// This function is shared across all reference matching tools (Upjet, API model,
// Terraform refs).
func filterFieldsForReferenceMatching(fields []types.FieldInfo) []types.FieldInfo {
	var filtered []types.FieldInfo
	for _, f := range fields {
		if f.IsDocument || f.IsIAMPolicy || f.HasReference {
			continue
		}
		if strings.EqualFold(f.Name, "Name") {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}

// matchUpjetItemKey returns the cache key for a resource match result.
func matchUpjetItemKey(serviceName string, resource types.ResourceInfo) string {
	return serviceName + "_" + resource.Kind
}

// buildMatchUpjetInputParams creates the input parameters used for cache hashing.
func buildMatchUpjetInputParams(resource types.ResourceInfo, upjetRefs []UpjetReferenceInfo, serviceName string) map[string]any {
	fieldNames := make([]string, 0, len(resource.StringFields))
	for _, f := range resource.StringFields {
		fieldNames = append(fieldNames, f.Name)
	}

	upjetFieldNames := make([]string, 0, len(upjetRefs))
	for _, ref := range upjetRefs {
		upjetFieldNames = append(upjetFieldNames, ref.FieldName)
	}

	return map[string]any{
		"service_name":      serviceName,
		"resource_kind":     resource.Kind,
		"ack_field_count":   len(resource.StringFields),
		"upjet_field_count": len(upjetRefs),
		"ack_field_names":   fieldNames,
		"upjet_field_names": upjetFieldNames,
	}
}

// buildMatchUpjetPrompt constructs the prompt sent to the agent for matching
// a single ACK resource's string fields against Upjet reference declarations.
func buildMatchUpjetPrompt(resource types.ResourceInfo, upjetRefs []UpjetReferenceInfo, serviceName string) string {
	var sb strings.Builder

	sb.WriteString("You are cross-referencing Upjet/Crossplane reference declarations against ACK (AWS Controllers for Kubernetes) CRD string fields to determine which ACK fields should have `references:` configuration.\n\n")

	sb.WriteString("## ACK Resource\n")
	fmt.Fprintf(&sb, "Service: %s\n", serviceName)
	fmt.Fprintf(&sb, "Resource Kind: %s\n", resource.Kind)
	sb.WriteString("String Fields (already filtered — these do NOT have is_document, is_iam_policy, or existing references):\n")
	for _, field := range resource.StringFields {
		fmt.Fprintf(&sb, "  - Name: %s, Path: %s, JSON Tag: %s\n", field.Name, field.Path, field.JSONTag)
	}

	sb.WriteString("\n## Upjet Reference Declarations\n")
	sb.WriteString("These fields have been identified as cross-resource references in the corresponding Upjet/Crossplane AWS provider config:\n")
	for _, ref := range upjetRefs {
		ambiguousStr := ""
		if ref.IsAmbiguous {
			ambiguousStr = " [AMBIGUOUS - can reference multiple resource types]"
		}
		fmt.Fprintf(&sb, "  - Field: %s, Target: %s, Confidence: %.2f%s\n", ref.FieldName, ref.TargetResource, ref.Confidence, ambiguousStr)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Match each Upjet reference field to its corresponding ACK CRD string field.\n")
	sb.WriteString("Use semantic understanding to resolve naming convention differences between Terraform/Upjet snake_case and ACK PascalCase:\n")
	sb.WriteString("  - Upjet 'parameter_group_name' → ACK 'ParameterGroupName' or 'CacheParameterGroupName'\n")
	sb.WriteString("  - Upjet 'kms_key_id' → ACK 'KMSKeyID' or 'KmsKeyId'\n")
	sb.WriteString("  - Upjet 'subnet_group_name' → ACK 'SubnetGroupName' or 'CacheSubnetGroupName'\n")
	sb.WriteString("  - Upjet 'vpc_id' → ACK 'VPCID' or 'VpcId'\n")
	sb.WriteString("Consider that ACK field names may have a service-specific prefix (e.g., 'Cache' for elasticache).\n")
	sb.WriteString("For each match, provide the ACK field name, its full path, the target Terraform resource, and a confidence score.\n")
	sb.WriteString("If an Upjet reference is marked as ambiguous, preserve that flag in the output.\n")
	sb.WriteString("If multiple ACK fields could match, select the highest-confidence one and list alternatives.\n")
	sb.WriteString("If an Upjet reference has no corresponding ACK field, include it in the unmatched list.\n")
	sb.WriteString("Every Upjet reference field must appear either in matches or in unmatched_upjet_fields.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"matches":[{"upjet_field_name":"<Upjet field name>","ack_field_name":"<matched ACK field name>","ack_field_path":"<full dot-separated path of the ACK field>","target_resource":"<terraform resource type, e.g. aws_kms_key>","is_ambiguous":<true or false>,"confidence":<0.0 to 1.0>,"alternatives":["<optional alternative ACK fields>"]}],"unmatched_upjet_fields":["<Upjet field names with no ACK match>"]}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseMatchUpjetResult parses the agent's JSON response into a MatchUpjetOutput.
func parseMatchUpjetResult(response string) (MatchUpjetOutput, error) {
	var result MatchUpjetOutput
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return result, fmt.Errorf("parsing upjet match response: %w", err)
	}
	return result, nil
}

// ValidateMatchUpjetOutput checks that a MatchUpjetOutput conforms to the
// expected schema: each match entry must have upjet_field_name, ack_field_name,
// ack_field_path, target_resource (unless ambiguous), and confidence between 0 and 1.
func ValidateMatchUpjetOutput(output *MatchUpjetOutput, validACKFields map[string]bool) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	for i, match := range output.Matches {
		if match.UpjetFieldName == "" {
			return fmt.Errorf("matches[%d]: upjet_field_name is empty", i)
		}
		if match.ACKFieldName == "" {
			return fmt.Errorf("matches[%d]: ack_field_name is empty", i)
		}
		if match.ACKFieldPath == "" {
			return fmt.Errorf("matches[%d]: ack_field_path is empty", i)
		}
		if !match.IsAmbiguous && match.TargetResource == "" {
			return fmt.Errorf("matches[%d]: non-ambiguous match must have a target_resource", i)
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

// ValidateMatchUpjetCompleteness checks that every Upjet reference provided to
// matching appears either in the matches list or in the unmatched list —
// none are silently dropped.
func ValidateMatchUpjetCompleteness(output *MatchUpjetOutput, upjetRefs []UpjetReferenceInfo) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	// Build sets of Upjet field names that appear in matches and unmatched
	matchedFields := make(map[string]bool)
	for _, match := range output.Matches {
		matchedFields[match.UpjetFieldName] = true
	}
	unmatchedFields := make(map[string]bool)
	for _, fieldName := range output.Unmatched {
		unmatchedFields[fieldName] = true
	}

	// Every input Upjet reference must appear in one of the two sets
	for _, ref := range upjetRefs {
		if !matchedFields[ref.FieldName] && !unmatchedFields[ref.FieldName] {
			return fmt.Errorf("Upjet field %q is neither matched nor listed as unmatched", ref.FieldName)
		}
	}

	return nil
}
