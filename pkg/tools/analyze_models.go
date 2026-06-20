package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const analyzeModelsTool = "analyze_models"

// ModelReferenceInfo describes a single field identified as a potential cross-resource
// reference within an AWS Smithy API model.
type ModelReferenceInfo struct {
	FieldName      string  `json:"field_name"`
	TargetService  string  `json:"target_service,omitempty"`
	TargetResource string  `json:"target_resource,omitempty"`
	SignalType     string  `json:"signal_type"`
	Confidence     float64 `json:"confidence"`
	Reasoning      string  `json:"reasoning"`
}

// AnalyzeModelOutput is the agent's analysis result for a single API model file.
type AnalyzeModelOutput struct {
	ServiceName string               `json:"service_name"`
	References  []ModelReferenceInfo `json:"references"`
}

// AnalyzeAllModelsOutput is the aggregated analysis result for all API model files.
type AnalyzeAllModelsOutput struct {
	Results map[string]*AnalyzeModelOutput `json:"results"`
	Skipped []string                       `json:"skipped,omitempty"`
}

// AnalyzeModel invokes the agent to analyze a single AWS Smithy API model file
// and identify fields that are potential cross-resource references. The prompt
// includes relevant structures filtered to the mapped ACK resources.
func AnalyzeModel(
	ctx context.Context,
	ag *agent.Agent,
	filePath string,
	content string,
	ackResources []string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*AnalyzeModelOutput, error) {
	l := resolveLogger(log)

	file := framework.FileToAnalyze{
		Key:      deriveModelItemKey(filePath),
		FilePath: filePath,
		Content:  content,
	}

	config := buildAnalyzeModelsConfig(ackResources)

	result, err := framework.AnalyzeOne(ctx, config, ag, file, resultCache, validator, l)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// AnalyzeAllModels orchestrates analyzing all mapped API model files with bounded
// concurrency. It reads model file content from the repo directory, filters content
// to relevant structures for mapped ACK resources, and calls the agent for each.
func AnalyzeAllModels(
	ctx context.Context,
	ag *agent.Agent,
	mappings []ModelMapping,
	repoDir string,
	controllers []types.ControllerInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*AnalyzeAllModelsOutput, error) {
	l := resolveLogger(log)

	// Build a lookup from service name to resource kinds
	resourcesByService := make(map[string][]string)
	for _, ctrl := range controllers {
		kinds := make([]string, 0, len(ctrl.Resources))
		for _, r := range ctrl.Resources {
			kinds = append(kinds, r.Kind)
		}
		resourcesByService[ctrl.ServiceName] = kinds
	}

	// Collect unique model files to analyze from mappings
	var files []framework.FileToAnalyze
	seen := make(map[string]bool)

	for _, mapping := range mappings {
		if mapping.ModelFile == "" {
			continue
		}
		if seen[mapping.ModelFile] {
			continue
		}
		seen[mapping.ModelFile] = true

		// Read model file content
		fullPath := filepath.Join(repoDir, mapping.ModelFile)
		contentBytes, err := os.ReadFile(fullPath)
		if err != nil {
			l.Skip(mapping.ModelFile, fmt.Sprintf("failed to read file: %v", err))
			continue
		}

		// Get ACK resource kinds for this controller
		ackResources := resourcesByService[mapping.ServiceName]

		// Filter content to relevant structures for the mapped ACK resources
		filteredContent := filterModelContent(string(contentBytes), ackResources)

		files = append(files, framework.FileToAnalyze{
			Key:      deriveModelItemKey(mapping.ModelFile),
			FilePath: mapping.ModelFile,
			Content:  filteredContent,
		})
	}

	l.Info("analyze_models: processing %d model files (parallelism: %d)", len(files), maxParallel)

	// Use the framework AnalyzeAll with a config that includes empty ackResources
	// (the resources are already baked into the filtered content)
	config := buildAnalyzeModelsConfig(nil)

	frameworkResult, err := framework.AnalyzeAll(ctx, config, ag, files, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	output := &AnalyzeAllModelsOutput{
		Results: make(map[string]*AnalyzeModelOutput, len(frameworkResult.Results)),
		Skipped: frameworkResult.Skipped,
	}

	for key, result := range frameworkResult.Results {
		r := result
		output.Results[key] = &r
	}

	return output, nil
}

// buildAnalyzeModelsConfig creates the framework.AnalysisConfig for API model analysis.
func buildAnalyzeModelsConfig(ackResources []string) framework.AnalysisConfig[AnalyzeModelOutput] {
	return framework.AnalysisConfig[AnalyzeModelOutput]{
		ToolName:    analyzeModelsTool,
		BuildPrompt: buildAnalyzeModelPrompt,
		ParseResult: parseAnalyzeModelResult,
		InputParams: buildAnalyzeModelInputParams,
	}
}

// buildAnalyzeModelPrompt constructs the prompt for analyzing a single API model file.
func buildAnalyzeModelPrompt(file framework.FileToAnalyze) string {
	var sb strings.Builder

	sb.WriteString("You are analyzing an AWS Smithy JSON API model file to identify fields that are cross-resource references (fields that hold an ARN, ID, or Name pointing to another AWS resource).\n\n")

	sb.WriteString("## Model File\n")
	fmt.Fprintf(&sb, "File: %s\n\n", file.FilePath)

	sb.WriteString("## Model Content (relevant structures)\n")
	sb.WriteString("```json\n")
	sb.WriteString(file.Content)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Signal Hierarchy for Identifying References\n")
	sb.WriteString("Use the following signals in order of confidence:\n\n")
	sb.WriteString("1. **aws.api#arnReference trait** (DEFINITIVE, confidence: 1.0)\n")
	sb.WriteString("   - If a field or its target shape has `aws.api#arnReference` trait, it is definitely a reference.\n\n")
	sb.WriteString("2. **Field name ending in ARN/Arn + doc mentioning another service** (HIGH confidence: 0.85)\n")
	sb.WriteString("   - Example: `ServiceLinkedRoleARN` with doc saying \"IAM role\"\n\n")
	sb.WriteString("3. **Field name ending in Id/ID + doc saying \"The ID of the...\"** (HIGH confidence: 0.8)\n")
	sb.WriteString("   - Example: `SubnetId` with doc saying \"subnet IDs\"\n\n")
	sb.WriteString("4. **Documentation containing \"use the X API to get this value\"** (HIGH confidence: 0.8)\n")
	sb.WriteString("   - Explicitly names the source API/service\n\n")
	sb.WriteString("5. **Field name ending in Name + doc referencing a specific resource type** (MEDIUM confidence: 0.6)\n")
	sb.WriteString("   - Example: `PlacementGroup` with doc saying \"name of the placement group\"\n\n")

	sb.WriteString("## Fields to EXCLUDE (not references)\n")
	sb.WriteString("- **JSON/document fields**: Fields that accept arbitrary JSON content (policy documents, configuration blobs)\n")
	sb.WriteString("- **Tags**: Fields named Tags, TagList, TagSpecifications, or similar\n")
	sb.WriteString("- **Enum fields**: Fields with a fixed set of allowed values\n")
	sb.WriteString("- **Self-referential fields**: The resource's own primary key (name/ID/ARN)\n")
	sb.WriteString("- **Free-form strings**: Descriptions, comments, metadata fields\n\n")

	sb.WriteString("## Instructions\n")
	sb.WriteString("Analyze the model structures above and identify ALL fields that are references to other AWS resources.\n")
	sb.WriteString("For each field, determine:\n")
	sb.WriteString("- The target service (e.g., \"ec2\", \"iam\", \"kms\") if identifiable from documentation\n")
	sb.WriteString("- The target resource type (e.g., \"Subnet\", \"Role\", \"Key\") if identifiable\n")
	sb.WriteString("- The signal type that triggered detection\n")
	sb.WriteString("- A confidence score based on the hierarchy above\n")
	sb.WriteString("- Brief reasoning explaining your determination\n\n")
	sb.WriteString("If no reference fields are found, return an empty references array.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"service_name":"<AWS service name from the model file>","references":[{"field_name":"<name of the field>","target_service":"<target AWS service if identifiable, or empty>","target_resource":"<target resource type if identifiable, or empty>","signal_type":"<one of: arn_trait, arn_suffix, id_suffix, name_suffix, doc_mention>","confidence":<0.0 to 1.0>,"reasoning":"<brief explanation>"}]}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseAnalyzeModelResult deserializes the agent's JSON response into an AnalyzeModelOutput.
func parseAnalyzeModelResult(response string) (AnalyzeModelOutput, error) {
	var result AnalyzeModelOutput
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return result, fmt.Errorf("parsing model analysis response: %w", err)
	}
	return result, nil
}

// buildAnalyzeModelInputParams creates the input parameters used for cache hashing.
func buildAnalyzeModelInputParams(file framework.FileToAnalyze) map[string]any {
	return map[string]any{
		"file_path":      file.FilePath,
		"content_length": len(file.Content),
	}
}

// deriveModelItemKey extracts a cache key from an API model file path.
// For example: "codegen/sdk-codegen/aws-models/elasticache.json" → "elasticache"
func deriveModelItemKey(filePath string) string {
	base := filepath.Base(filePath)
	if idx := strings.LastIndex(base, ".json"); idx > 0 {
		return base[:idx]
	}
	ext := filepath.Ext(base)
	if ext != "" {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

// filterModelContent filters the Smithy JSON model content to include only
// structures relevant to the given ACK resource kinds. This reduces the amount
// of content sent to the agent, keeping the context focused.
//
// If ackResources is nil or empty, returns a truncated version of the full content
// (first 50KB to stay within reasonable prompt limits).
func filterModelContent(content string, ackResources []string) string {
	if len(ackResources) == 0 {
		// No filtering possible — return truncated content
		return truncateContent(content, 50000)
	}

	// Parse the Smithy JSON model to extract relevant structures
	var model map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &model); err != nil {
		// If we can't parse, return truncated raw content
		return truncateContent(content, 50000)
	}

	// The Smithy JSON AST has shapes under a "shapes" key
	shapesRaw, ok := model["shapes"]
	if !ok {
		return truncateContent(content, 50000)
	}

	var shapes map[string]json.RawMessage
	if err := json.Unmarshal(shapesRaw, &shapes); err != nil {
		return truncateContent(content, 50000)
	}

	// Build a set of keywords to match against shape names
	// We look for shapes that contain the resource kind names (case-insensitive)
	keywords := make([]string, 0, len(ackResources))
	for _, kind := range ackResources {
		keywords = append(keywords, strings.ToLower(kind))
	}

	// Collect relevant shapes: structures that reference our resource kinds
	relevantShapes := make(map[string]json.RawMessage)
	for shapeName, shapeData := range shapes {
		lowerName := strings.ToLower(shapeName)

		// Include shapes whose names contain any of our resource kind keywords
		for _, kw := range keywords {
			if strings.Contains(lowerName, kw) {
				relevantShapes[shapeName] = shapeData
				break
			}
		}
	}

	// If we found relevant shapes, also include their member target shapes
	// to provide context for reference resolution
	targetShapes := make(map[string]json.RawMessage)
	for _, shapeData := range relevantShapes {
		var shape struct {
			Type    string `json:"type"`
			Members map[string]struct {
				Target string `json:"target"`
			} `json:"members"`
		}
		if err := json.Unmarshal(shapeData, &shape); err != nil {
			continue
		}
		if shape.Type != "structure" {
			continue
		}
		for _, member := range shape.Members {
			if member.Target != "" {
				if targetData, ok := shapes[member.Target]; ok {
					targetShapes[member.Target] = targetData
				}
			}
		}
	}

	// Merge target shapes into relevant shapes
	for name, data := range targetShapes {
		if _, exists := relevantShapes[name]; !exists {
			relevantShapes[name] = data
		}
	}

	// If no relevant shapes found, return truncated content
	if len(relevantShapes) == 0 {
		return truncateContent(content, 50000)
	}

	// Rebuild a filtered model with only relevant shapes
	filtered := map[string]interface{}{
		"shapes": relevantShapes,
	}

	filteredJSON, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return truncateContent(content, 50000)
	}

	// Ensure we don't exceed reasonable prompt limits
	return truncateContent(string(filteredJSON), 80000)
}

// truncateContent truncates content to maxLen characters, appending a truncation notice.
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "\n... [truncated]"
}

// ValidateAnalyzeModelOutput checks that an AnalyzeModelOutput conforms to
// the expected schema.
func ValidateAnalyzeModelOutput(output *AnalyzeModelOutput) error {
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

	for i, ref := range output.References {
		if ref.FieldName == "" {
			return fmt.Errorf("references[%d]: field_name is empty", i)
		}
		if !validSignalTypes[ref.SignalType] {
			return fmt.Errorf("references[%d]: signal_type must be one of arn_trait, arn_suffix, id_suffix, name_suffix, doc_mention; got %q", i, ref.SignalType)
		}
		if ref.Confidence < 0 || ref.Confidence > 1 {
			return fmt.Errorf("references[%d]: confidence must be between 0 and 1, got %f", i, ref.Confidence)
		}
	}

	return nil
}
