package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const matchFieldsTool = "match_fields"

// MatchFieldsOutput is the agent's cross-reference result for a single resource.
type MatchFieldsOutput struct {
	Matches   []types.FieldMatch `json:"matches"`
	Unmatched []string           `json:"unmatched_tf_fields"`
}

// MatchAllResourcesOutput is the aggregated match result for all resources.
type MatchAllResourcesOutput struct {
	Results map[string]*MatchFieldsOutput `json:"results"`
	Skipped []string                      `json:"skipped,omitempty"`
}

// MatchResource invokes the agent to cross-reference Terraform JSON fields
// against an ACK resource's string fields to determine which ACK fields are
// JSON documents. Each call processes a single resource to minimize context.
func MatchResource(
	ctx context.Context,
	ag *agent.Agent,
	resource types.ResourceInfo,
	tfJSONFields []types.JSONFieldInfo,
	serviceName string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*MatchFieldsOutput, error) {
	l := resolveLogger(log)
	itemKey := serviceName + "_" + resource.Kind
	inputParams := buildMatchInputParams(resource, tfJSONFields, serviceName)

	// Check cache first
	if resultCache != nil {
		entry, err := resultCache.Get(matchFieldsTool, itemKey, inputParams)
		if err == nil && entry != nil {
			var output MatchFieldsOutput
			if err := json.Unmarshal(entry.Result, &output); err == nil {
				l.CacheHit(matchFieldsTool + "/" + itemKey)
				return &output, nil
			}
		}
	}

	l.CacheMiss(matchFieldsTool + "/" + itemKey)
	l.AgentCall("match_fields", serviceName+"/"+resource.Kind)

	// Build the prompt
	prompt := buildMatchFieldsPrompt(resource, tfJSONFields, serviceName)

	// Call the agent with validation
	result, err := ag.RunWithValidation(ctx, prompt, validator)
	if err != nil {
		l.Error("match_fields agent call failed for %s: %v", itemKey, err)
		return nil, err
	}

	// Parse the response
	var output MatchFieldsOutput
	if err := json.Unmarshal([]byte(result.FinalResponse), &output); err != nil {
		l.Error("match_fields failed to parse response for %s: %v", itemKey, err)
		return nil, fmt.Errorf("parsing agent response for resource %q in service %q: %w", resource.Kind, serviceName, err)
	}

	// Cache the result
	if resultCache != nil {
		resultJSON, _ := json.Marshal(output)
		if err := resultCache.Put(matchFieldsTool, itemKey, inputParams, resultJSON); err != nil {
			l.Warn("match_fields failed to cache result for %s: %v", itemKey, err)
		} else {
			l.Debug("match_fields cached result for %s (%d matches, %d unmatched)",
				itemKey, len(output.Matches), len(output.Unmatched))
		}
	}

	return &output, nil
}

// MatchAllResources orchestrates matching all resources across all controllers.
// For each controller, for each resource, it finds the TF JSON fields that
// correspond to this controller's mapped docs, checks the cache, calls the
// agent for misses, and aggregates all match results.
//
// NOTE: This function retains its own concurrency implementation rather than using
// framework.MatchAll because it includes complex pre-processing: building a
// doc-fields lookup map from analysis results, constructing service-to-docpath
// mappings, and flattening controllers×resources into a work queue with per-item
// TF field assembly. The framework's MatchAll uses a simpler sourceData map[string][]T
// pattern that doesn't accommodate this multi-step data preparation. The scan
// orchestrator also has its own matchResourcesConcurrent wrapper.
func MatchAllResources(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	analysisResults map[string]*AnalyzeFieldsOutput,
	mappings []types.ControllerMapping,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*MatchAllResourcesOutput, error) {
	return MatchAllResourcesParallel(ctx, ag, controllers, analysisResults, mappings, resultCache, validator, 1, log...)
}

// MatchAllResourcesParallel orchestrates matching all resources with bounded concurrency.
func MatchAllResourcesParallel(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	analysisResults map[string]*AnalyzeFieldsOutput,
	mappings []types.ControllerMapping,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MatchAllResourcesOutput, error) {
	l := resolveLogger(log)
	output := &MatchAllResourcesOutput{
		Results: make(map[string]*MatchFieldsOutput),
	}

	if maxParallel <= 0 {
		maxParallel = 1
	}

	// Build a lookup from doc file path to its analyzed JSON fields
	docFieldsMap := make(map[string][]types.JSONFieldInfo)
	for docPath, analysis := range analysisResults {
		if analysis != nil {
			docFieldsMap[docPath] = analysis.JSONFields
		}
	}

	// Build a lookup from service name to its mapped TF doc paths
	serviceMappings := make(map[string][]string)
	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			serviceMappings[mapping.ServiceName] = append(serviceMappings[mapping.ServiceName], entry.DocFilePath)
		}
	}

	// Build flat list of items to process
	type matchItem struct {
		controller types.ControllerInfo
		resource   types.ResourceInfo
		tfFields   []types.JSONFieldInfo
		itemKey    string
	}

	var items []matchItem
	for _, controller := range controllers {
		docPaths := serviceMappings[controller.ServiceName]
		var tfJSONFields []types.JSONFieldInfo
		for _, docPath := range docPaths {
			if fields, ok := docFieldsMap[docPath]; ok {
				tfJSONFields = append(tfJSONFields, fields...)
			}
		}
		if len(tfJSONFields) == 0 {
			l.Debug("match_fields: skipping controller %s (no TF JSON fields)", controller.ServiceName)
			continue
		}
		for _, resource := range controller.Resources {
			items = append(items, matchItem{
				controller: controller,
				resource:   resource,
				tfFields:   tfJSONFields,
				itemKey:    controller.ServiceName + "_" + resource.Kind,
			})
		}
	}

	l.Info("match_fields: processing %d resources across %d controllers (parallelism: %d)",
		len(items), len(controllers), maxParallel)

	type result struct {
		itemKey string
		output  *MatchFieldsOutput
		skipped bool
	}

	total := len(items)
	results := make([]result, total)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var cacheHits, cacheMisses, completed atomic.Int32

	for i, item := range items {
		select {
		case <-ctx.Done():
			break
		default:
		}

		wg.Add(1)
		go func(idx int, mi matchItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check cache
			inputParams := buildMatchInputParams(mi.resource, mi.tfFields, mi.controller.ServiceName)
			if resultCache != nil {
				entry, err := resultCache.Get(matchFieldsTool, mi.itemKey, inputParams)
				if err == nil && entry != nil {
					var matchResult MatchFieldsOutput
					if err := json.Unmarshal(entry.Result, &matchResult); err == nil {
						l.CacheHit(matchFieldsTool + "/" + mi.itemKey)
						cacheHits.Add(1)
						done := int(completed.Add(1))
						l.Progress(done, total, "match_fields")
						results[idx] = result{itemKey: mi.itemKey, output: &matchResult}
						return
					}
				}
			}

			cacheMisses.Add(1)

			// Cache miss — call agent
			matchResult, err := MatchResource(ctx, ag, mi.resource, mi.tfFields, mi.controller.ServiceName, resultCache, validator, l)
			done := int(completed.Add(1))
			if err != nil {
				if err == agent.ErrSkipItem {
					l.Skip(mi.controller.ServiceName+"/"+mi.resource.Kind, "validation failed after retries")
				} else {
					l.Error("match_fields error for %s/%s: %v", mi.controller.ServiceName, mi.resource.Kind, err)
				}
				l.Progress(done, total, "match_fields")
				results[idx] = result{itemKey: mi.itemKey, skipped: true}
				return
			}

			l.Progress(done, total, "match_fields")
			results[idx] = result{itemKey: mi.itemKey, output: matchResult}
		}(i, item)
	}

	wg.Wait()

	// Aggregate results
	for _, r := range results {
		if r.output != nil {
			output.Results[r.itemKey] = r.output
		} else if r.skipped {
			output.Skipped = append(output.Skipped, r.itemKey)
		}
	}

	l.CacheSummary("match_fields", int(cacheHits.Load()), int(cacheMisses.Load()), len(output.Skipped))

	return output, nil
}

// ValidateMatchFieldsOutput checks that a MatchFieldsOutput conforms to the
// expected schema: each match entry must have terraform_field_name, ack_field_name,
// ack_field_path, and confidence (between 0 and 1). The ack_field_name should
// refer to a valid ACK field from the resource's string fields.
func ValidateMatchFieldsOutput(output *MatchFieldsOutput, validACKFields map[string]bool) error {
	if output == nil {
		return fmt.Errorf("output is nil")
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
		if match.Confidence < 0 || match.Confidence > 1 {
			return fmt.Errorf("matches[%d]: confidence must be between 0 and 1, got %f", i, match.Confidence)
		}
		if validACKFields != nil && !validACKFields[match.ACKFieldName] {
			return fmt.Errorf("matches[%d]: ack_field_name %q is not a valid ACK field", i, match.ACKFieldName)
		}
	}

	return nil
}

// ValidateMatchCompleteness checks that every TF JSON field provided to matching
// appears either in the matches list or in the unmatched list — none are silently dropped.
func ValidateMatchCompleteness(output *MatchFieldsOutput, tfJSONFields []types.JSONFieldInfo) error {
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

	// Every input TF field must appear in one of the two sets
	for _, tfField := range tfJSONFields {
		if !matchedTFFields[tfField.FieldName] && !unmatchedTFFields[tfField.FieldName] {
			return fmt.Errorf("TF field %q is neither matched nor listed as unmatched", tfField.FieldName)
		}
	}

	return nil
}

// buildMatchFieldsPrompt constructs the prompt sent to the agent for matching
// a single ACK resource's string fields against Terraform JSON fields.
func buildMatchFieldsPrompt(resource types.ResourceInfo, tfJSONFields []types.JSONFieldInfo, serviceName string) string {
	var sb strings.Builder

	sb.WriteString("You are cross-referencing Terraform JSON-accepting fields against ACK (AWS Controllers for Kubernetes) CRD string fields to determine which ACK fields correspond to JSON documents.\n\n")

	sb.WriteString("## ACK Resource\n")
	fmt.Fprintf(&sb, "Service: %s\n", serviceName)
	fmt.Fprintf(&sb, "Resource Kind: %s\n", resource.Kind)
	sb.WriteString("String Fields:\n")
	for _, field := range resource.StringFields {
		fmt.Fprintf(&sb, "  - Name: %s, Path: %s, JSON Tag: %s\n", field.Name, field.Path, field.JSONTag)
	}

	sb.WriteString("\n## Terraform JSON Fields\n")
	sb.WriteString("These fields have been identified as accepting JSON-encoded values in the corresponding Terraform documentation:\n")
	for _, field := range tfJSONFields {
		fmt.Fprintf(&sb, "  - Field: %s, Type: %s, Confidence: %.2f\n", field.FieldName, field.FieldType, field.Confidence)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Match each Terraform JSON field to its corresponding ACK CRD string field.\n")
	sb.WriteString("Use semantic understanding to resolve naming convention differences (e.g., 'assume_role_policy' in Terraform maps to 'AssumeRolePolicyDocument' in ACK).\n")
	sb.WriteString("For each match, provide the ACK field name, its full path, and a confidence score.\n")
	sb.WriteString("If multiple ACK fields could match, select the highest-confidence one and list alternatives.\n")
	sb.WriteString("If a Terraform field has no corresponding ACK field, include it in the unmatched list.\n")
	sb.WriteString("Every Terraform JSON field must appear either in matches or in unmatched_tf_fields.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"matches":[{"terraform_field_name":"<name of the TF field>","ack_field_name":"<name of the matched ACK field>","ack_field_path":"<full dot-separated path of the ACK field>","confidence":<0.0 to 1.0>,"alternatives":["<optional>"]}],"unmatched_tf_fields":["<TF field names with no ACK match>"]}`)
	sb.WriteString("\n")

	return sb.String()
}

// buildMatchInputParams creates the input parameters used for cache hashing.
func buildMatchInputParams(resource types.ResourceInfo, tfJSONFields []types.JSONFieldInfo, serviceName string) map[string]any {
	fieldNames := make([]string, 0, len(resource.StringFields))
	for _, f := range resource.StringFields {
		fieldNames = append(fieldNames, f.Name)
	}

	tfFieldNames := make([]string, 0, len(tfJSONFields))
	for _, f := range tfJSONFields {
		tfFieldNames = append(tfFieldNames, f.FieldName)
	}

	return map[string]any{
		"service_name":    serviceName,
		"resource_kind":   resource.Kind,
		"ack_field_count": len(resource.StringFields),
		"tf_field_count":  len(tfJSONFields),
		"ack_field_names": fieldNames,
		"tf_field_names":  tfFieldNames,
	}
}
