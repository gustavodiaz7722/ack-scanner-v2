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

const matchModelsTool = "match_models"

// ModelFieldMatch maps an API model reference field to an ACK CRD field.
type ModelFieldMatch struct {
	ModelFieldName string   `json:"model_field_name"`
	ACKFieldName   string   `json:"ack_field_name"`
	ACKFieldPath   string   `json:"ack_field_path"`
	TargetService  string   `json:"target_service,omitempty"`
	TargetResource string   `json:"target_resource,omitempty"`
	SignalType     string   `json:"signal_type"`
	Confidence     float64  `json:"confidence"`
	Alternatives   []string `json:"alternatives,omitempty"`
}

// MatchModelOutput is the agent's cross-reference result for a single resource
// matched against API model reference signals.
type MatchModelOutput struct {
	Matches   []ModelFieldMatch `json:"matches"`
	Unmatched []string          `json:"unmatched_model_fields"`
}

// MatchAllModelOutput is the aggregated match result for all resources against
// API model reference signals.
type MatchAllModelOutput struct {
	Results map[string]*MatchModelOutput `json:"results"`
	Skipped []string                     `json:"skipped,omitempty"`
}

// MatchResourceModel invokes the agent to cross-reference API model reference
// signals against an ACK resource's string fields to determine which ACK fields
// correspond to cross-resource references. Each call processes a single resource.
func MatchResourceModel(
	ctx context.Context,
	ag *agent.Agent,
	resource types.ResourceInfo,
	modelRefs []ModelReferenceInfo,
	serviceName string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*MatchModelOutput, error) {
	l := resolveLogger(log)

	config := buildMatchModelsConfig()
	result, err := framework.MatchOne(ctx, config, ag, resource, modelRefs, serviceName, resultCache, validator, l)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// MatchAllResourcesModel orchestrates matching all resources across all controllers
// against API model reference signals with bounded concurrency.
func MatchAllResourcesModel(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	analysisResults map[string]*AnalyzeModelOutput,
	mappings []ModelMapping,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MatchAllModelOutput, error) {
	l := resolveLogger(log)

	// Build sourceData: map service name → list of ModelReferenceInfo
	sourceData := make(map[string][]ModelReferenceInfo)
	for key, analysis := range analysisResults {
		if analysis != nil && len(analysis.References) > 0 {
			sourceData[key] = analysis.References
		}
	}

	// Build serviceMappings: map ACK service name → list of model keys
	// that contain the analysis results for that controller
	serviceMappings := make(map[string][]string)
	for _, mapping := range mappings {
		if mapping.ModelFile == "" {
			continue
		}
		// The analysis results are keyed by the model item key (derived from file path)
		modelKey := deriveModelItemKey(mapping.ModelFile)
		serviceMappings[mapping.ServiceName] = append(serviceMappings[mapping.ServiceName], modelKey)
	}

	config := buildMatchModelsConfig()

	frameworkResult, err := framework.MatchAll(ctx, config, ag, controllers, sourceData, serviceMappings, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	output := &MatchAllModelOutput{
		Results: make(map[string]*MatchModelOutput, len(frameworkResult.Results)),
		Skipped: frameworkResult.Skipped,
	}

	for key, result := range frameworkResult.Results {
		r := result
		output.Results[key] = &r
	}

	return output, nil
}

// buildMatchModelsConfig creates the framework.MatchConfig for ACK-to-model matching.
func buildMatchModelsConfig() framework.MatchConfig[ModelReferenceInfo, MatchModelOutput] {
	return framework.MatchConfig[ModelReferenceInfo, MatchModelOutput]{
		ToolName:    matchModelsTool,
		BuildPrompt: buildMatchModelPrompt,
		ParseResult: parseMatchModelResult,
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams:  buildMatchModelInputParams,
		FilterFields: filterFieldsForReferenceMatching,
	}
}

// buildMatchModelPrompt constructs the prompt sent to the agent for matching
// a single ACK resource's string fields against API model reference signals.
func buildMatchModelPrompt(resource types.ResourceInfo, modelRefs []ModelReferenceInfo, serviceName string) string {
	var sb strings.Builder

	sb.WriteString("You are cross-referencing AWS Smithy API model reference signals against ACK (AWS Controllers for Kubernetes) CRD string fields to determine which ACK fields are cross-resource references.\n\n")

	sb.WriteString("## ACK Resource\n")
	fmt.Fprintf(&sb, "Service: %s\n", serviceName)
	fmt.Fprintf(&sb, "Resource Kind: %s\n", resource.Kind)
	sb.WriteString("String Fields (excluding already-annotated fields):\n")
	for _, field := range resource.StringFields {
		fmt.Fprintf(&sb, "  - Name: %s, Path: %s\n", field.Name, field.Path)
	}

	sb.WriteString("\n## API Model Reference Signals\n")
	sb.WriteString("These fields have been identified as cross-resource references from the AWS Smithy API model:\n")
	for _, ref := range modelRefs {
		fmt.Fprintf(&sb, "  - Field: %s, Signal: %s, Confidence: %.2f", ref.FieldName, ref.SignalType, ref.Confidence)
		if ref.TargetService != "" {
			fmt.Fprintf(&sb, ", Target Service: %s", ref.TargetService)
		}
		if ref.TargetResource != "" {
			fmt.Fprintf(&sb, ", Target Resource: %s", ref.TargetResource)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Important: PascalCase Correspondence\n")
	sb.WriteString("Smithy model field names are PascalCase and directly correspond to ACK field names.\n")
	sb.WriteString("For example:\n")
	sb.WriteString("  - Smithy `ServiceLinkedRoleARN` → ACK `ServiceLinkedRoleARN` (exact match)\n")
	sb.WriteString("  - Smithy `SubnetGroupName` → ACK `SubnetGroupName` (exact match)\n")
	sb.WriteString("  - Smithy `KmsKeyId` → ACK `KMSKeyID` (minor casing differences)\n")
	sb.WriteString("Most matches will be exact or near-exact due to this direct correspondence.\n")

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Match each API model reference field to its corresponding ACK CRD string field.\n")
	sb.WriteString("Since Smithy model field names are PascalCase and directly correspond to ACK field names, most matches should be exact or near-exact.\n")
	sb.WriteString("For each match, provide the ACK field name, its full path, target service/resource if known, signal type, and a confidence score.\n")
	sb.WriteString("If multiple ACK fields could match, select the highest-confidence one and list alternatives.\n")
	sb.WriteString("If an API model field has no corresponding ACK field, include it in the unmatched list.\n")
	sb.WriteString("Every API model reference field must appear either in matches or in unmatched_model_fields.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"matches":[{"model_field_name":"<name of the model field>","ack_field_name":"<name of the matched ACK field>","ack_field_path":"<full dot-separated path of the ACK field>","target_service":"<target AWS service if known, or empty>","target_resource":"<target resource type if known, or empty>","signal_type":"<signal type from the model analysis>","confidence":<0.0 to 1.0>,"alternatives":["<optional>"]}],"unmatched_model_fields":["<model field names with no ACK match>"]}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseMatchModelResult deserializes the agent's JSON response into a MatchModelOutput.
func parseMatchModelResult(response string) (MatchModelOutput, error) {
	var result MatchModelOutput
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return result, fmt.Errorf("parsing model match response: %w", err)
	}
	return result, nil
}

// buildMatchModelInputParams creates the input parameters used for cache hashing.
func buildMatchModelInputParams(resource types.ResourceInfo, modelRefs []ModelReferenceInfo, serviceName string) map[string]any {
	fieldNames := make([]string, 0, len(resource.StringFields))
	for _, f := range resource.StringFields {
		fieldNames = append(fieldNames, f.Name)
	}

	modelFieldNames := make([]string, 0, len(modelRefs))
	for _, ref := range modelRefs {
		modelFieldNames = append(modelFieldNames, ref.FieldName)
	}

	return map[string]any{
		"service_name":      serviceName,
		"resource_kind":     resource.Kind,
		"ack_field_count":   len(resource.StringFields),
		"model_field_count": len(modelRefs),
		"ack_field_names":   fieldNames,
		"model_field_names": modelFieldNames,
	}
}

// ValidateMatchModelOutput checks that a MatchModelOutput conforms to the
// expected schema: each match entry must have model_field_name, ack_field_name,
// ack_field_path, signal_type, and confidence (between 0 and 1).
func ValidateMatchModelOutput(output *MatchModelOutput, validACKFields map[string]bool) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	validSignalTypes := map[string]bool{
		"arn_trait":   true,
		"arn_suffix":  true,
		"id_suffix":   true,
		"name_suffix": true,
		"doc_mention": true,
	}

	for i, match := range output.Matches {
		if match.ModelFieldName == "" {
			return fmt.Errorf("matches[%d]: model_field_name is empty", i)
		}
		if match.ACKFieldName == "" {
			return fmt.Errorf("matches[%d]: ack_field_name is empty", i)
		}
		if match.ACKFieldPath == "" {
			return fmt.Errorf("matches[%d]: ack_field_path is empty", i)
		}
		if match.SignalType == "" {
			return fmt.Errorf("matches[%d]: signal_type is empty", i)
		}
		if !validSignalTypes[match.SignalType] {
			return fmt.Errorf("matches[%d]: signal_type must be one of arn_trait, arn_suffix, id_suffix, name_suffix, doc_mention; got %q", i, match.SignalType)
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

// ValidateMatchModelCompleteness checks that every model reference field provided
// to matching appears either in the matches list or in the unmatched list.
func ValidateMatchModelCompleteness(output *MatchModelOutput, modelRefs []ModelReferenceInfo) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	matchedFields := make(map[string]bool)
	for _, match := range output.Matches {
		matchedFields[match.ModelFieldName] = true
	}
	unmatchedFields := make(map[string]bool)
	for _, fieldName := range output.Unmatched {
		unmatchedFields[fieldName] = true
	}

	for _, ref := range modelRefs {
		if !matchedFields[ref.FieldName] && !unmatchedFields[ref.FieldName] {
			return fmt.Errorf("model field %q is neither matched nor listed as unmatched", ref.FieldName)
		}
	}

	return nil
}
