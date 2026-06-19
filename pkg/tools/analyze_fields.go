package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const analyzeFieldsTool = "analyze_fields"

// AnalyzeFieldsOutput is the agent's analysis of JSON fields in a single TF doc.
type AnalyzeFieldsOutput struct {
	ResourceType string                `json:"resource_type"`
	JSONFields   []types.JSONFieldInfo `json:"json_fields"`
}

// AnalyzeAllDocsOutput is the aggregated analysis result for all TF docs.
type AnalyzeAllDocsOutput struct {
	Results map[string]*AnalyzeFieldsOutput `json:"results"`
	Skipped []string                        `json:"skipped,omitempty"`
}

// AnalyzeDoc invokes the agent to analyze a single Terraform documentation file
// for JSON-accepting fields. The prompt includes the full documentation content
// and the expected output schema. Results are cached per doc file.
func AnalyzeDoc(
	ctx context.Context,
	ag *agent.Agent,
	docFilePath string,
	docContent string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
) (*AnalyzeFieldsOutput, error) {
	itemKey := deriveItemKey(docFilePath)
	inputParams := buildAnalyzeInputParams(docFilePath, docContent)

	// Check cache first
	if resultCache != nil {
		entry, err := resultCache.Get(analyzeFieldsTool, itemKey, inputParams)
		if err == nil && entry != nil {
			var output AnalyzeFieldsOutput
			if err := json.Unmarshal(entry.Result, &output); err == nil {
				return &output, nil
			}
		}
	}

	// Build the prompt
	prompt := buildAnalyzeFieldsPrompt(docFilePath, docContent)

	// Call the agent with validation
	result, err := ag.RunWithValidation(ctx, prompt, validator)
	if err != nil {
		return nil, err
	}

	// Parse the response
	var output AnalyzeFieldsOutput
	if err := json.Unmarshal([]byte(result.FinalResponse), &output); err != nil {
		return nil, fmt.Errorf("parsing agent response for doc %q: %w", docFilePath, err)
	}

	// Cache the result
	if resultCache != nil {
		resultJSON, _ := json.Marshal(output)
		_ = resultCache.Put(analyzeFieldsTool, itemKey, inputParams, resultJSON)
	}

	return &output, nil
}

// AnalyzeAllDocs orchestrates analyzing all mapped Terraform documentation files.
// It iterates over controller mappings, reads each mapped doc file from the repo
// directory, checks the cache, calls the agent for misses, and aggregates results.
// On per-doc failure, it logs the error and continues with the remaining docs.
func AnalyzeAllDocs(
	ctx context.Context,
	ag *agent.Agent,
	mappings []types.ControllerMapping,
	repoDir string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
) (*AnalyzeAllDocsOutput, error) {
	output := &AnalyzeAllDocsOutput{
		Results: make(map[string]*AnalyzeFieldsOutput),
	}

	// Collect unique doc file paths from all mappings
	seen := make(map[string]bool)
	var docPaths []string
	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			if entry.DocFilePath != "" && !seen[entry.DocFilePath] {
				seen[entry.DocFilePath] = true
				docPaths = append(docPaths, entry.DocFilePath)
			}
		}
	}

	for _, docPath := range docPaths {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		default:
		}

		// Read the doc content from the repo directory
		fullPath := filepath.Join(repoDir, docPath)
		contentBytes, err := os.ReadFile(fullPath)
		if err != nil {
			log.Printf("[analyze_fields] skipping doc %q: failed to read file: %v", docPath, err)
			output.Skipped = append(output.Skipped, docPath)
			continue
		}
		docContent := string(contentBytes)

		// Call AnalyzeDoc
		result, err := AnalyzeDoc(ctx, ag, docPath, docContent, resultCache, validator)
		if err != nil {
			if err == agent.ErrSkipItem {
				log.Printf("[analyze_fields] skipping doc %q: validation failed after retries", docPath)
				output.Skipped = append(output.Skipped, docPath)
				continue
			}
			log.Printf("[analyze_fields] skipping doc %q: %v", docPath, err)
			output.Skipped = append(output.Skipped, docPath)
			continue
		}

		output.Results[docPath] = result
	}

	return output, nil
}

// deriveItemKey extracts a cache item key from a Terraform doc file path.
// For example: "website/docs/r/s3_bucket.html.markdown" → "s3_bucket"
func deriveItemKey(docFilePath string) string {
	// Get the filename without directory path
	base := filepath.Base(docFilePath)

	// Remove the .html.markdown extension
	if idx := strings.Index(base, ".html.markdown"); idx > 0 {
		return base[:idx]
	}

	// Fallback: remove any extension
	ext := filepath.Ext(base)
	if ext != "" {
		return strings.TrimSuffix(base, ext)
	}

	return base
}

// buildAnalyzeInputParams creates the input parameters used for cache hashing.
func buildAnalyzeInputParams(docFilePath, docContent string) map[string]any {
	return map[string]any{
		"doc_file_path":  docFilePath,
		"content_length": len(docContent),
	}
}

// buildAnalyzeFieldsPrompt constructs the prompt sent to the agent for analyzing
// a single Terraform documentation file for JSON-accepting fields.
func buildAnalyzeFieldsPrompt(docFilePath, docContent string) string {
	var sb strings.Builder

	sb.WriteString("You are analyzing a Terraform AWS provider resource documentation file to identify all fields that accept JSON-encoded values.\n\n")

	sb.WriteString("## Documentation File\n")
	fmt.Fprintf(&sb, "File: %s\n\n", docFilePath)

	sb.WriteString("## Full Documentation Content\n")
	sb.WriteString("```\n")
	sb.WriteString(docContent)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Instructions\n")
	sb.WriteString("Analyze the documentation above and identify ALL fields (arguments) that accept JSON-encoded values.\n")
	sb.WriteString("For each field, determine whether it is:\n")
	sb.WriteString("- \"json_document\": A field that accepts arbitrary JSON content (e.g., JSON schemas, configuration documents, event patterns)\n")
	sb.WriteString("- \"iam_policy\": A field that specifically accepts IAM policy documents (e.g., assume_role_policy, policy)\n\n")
	sb.WriteString("Look for indicators such as:\n")
	sb.WriteString("- Description mentioning \"JSON\", \"policy document\", \"JSON-encoded\", or \"jsonencode\"\n")
	sb.WriteString("- Example values showing JSON content or use of jsonencode()\n")
	sb.WriteString("- Field names containing \"policy\", \"document\", \"json\", \"configuration\"\n")
	sb.WriteString("- References to aws_iam_policy_document data sources\n\n")
	sb.WriteString("If no JSON fields are found, return an empty json_fields array.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"resource_type":"<the Terraform resource type, e.g. aws_s3_bucket>","json_fields":[{"field_name":"<name of the field>","field_type":"<one of: json_document, iam_policy>","confidence":<0.0 to 1.0>,"reasoning":"<brief explanation>"}]}`)
	sb.WriteString("\nIf no JSON fields are found, return an empty json_fields array.\n")

	return sb.String()
}

// ValidateAnalyzeFieldsOutput checks that an AnalyzeFieldsOutput conforms to
// the expected schema: each JSONFieldInfo must have field_name, field_type
// (one of "json_document" or "iam_policy"), and confidence (between 0 and 1).
func ValidateAnalyzeFieldsOutput(output *AnalyzeFieldsOutput) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	for i, field := range output.JSONFields {
		if field.FieldName == "" {
			return fmt.Errorf("json_fields[%d]: field_name is empty", i)
		}
		if field.FieldType != "json_document" && field.FieldType != "iam_policy" {
			return fmt.Errorf("json_fields[%d]: field_type must be \"json_document\" or \"iam_policy\", got %q", i, field.FieldType)
		}
		if field.Confidence < 0 || field.Confidence > 1 {
			return fmt.Errorf("json_fields[%d]: confidence must be between 0 and 1, got %f", i, field.Confidence)
		}
	}

	return nil
}
