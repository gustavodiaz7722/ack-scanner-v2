package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
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
	log ...*logger.Logger,
) (*AnalyzeFieldsOutput, error) {
	l := resolveLogger(log)
	itemKey := deriveItemKey(docFilePath)
	inputParams := buildAnalyzeInputParams(docFilePath, docContent)

	// Check cache first
	if resultCache != nil {
		entry, err := resultCache.Get(analyzeFieldsTool, itemKey, inputParams)
		if err == nil && entry != nil {
			var output AnalyzeFieldsOutput
			if err := json.Unmarshal(entry.Result, &output); err == nil {
				l.CacheHit(analyzeFieldsTool + "/" + itemKey)
				return &output, nil
			}
		}
	}

	l.CacheMiss(analyzeFieldsTool + "/" + itemKey)
	l.AgentCall("analyze_fields", itemKey)

	// Build the prompt
	prompt := buildAnalyzeFieldsPrompt(docFilePath, docContent)

	// Call the agent with validation
	result, err := ag.RunWithValidation(ctx, prompt, validator)
	if err != nil {
		l.Error("analyze_fields agent call failed for %s: %v", itemKey, err)
		return nil, err
	}

	// Parse the response
	var output AnalyzeFieldsOutput
	if err := json.Unmarshal([]byte(result.FinalResponse), &output); err != nil {
		l.Error("analyze_fields failed to parse response for %s: %v", itemKey, err)
		return nil, fmt.Errorf("parsing agent response for doc %q: %w", docFilePath, err)
	}

	// Cache the result
	if resultCache != nil {
		resultJSON, _ := json.Marshal(output)
		if err := resultCache.Put(analyzeFieldsTool, itemKey, inputParams, resultJSON); err != nil {
			l.Warn("analyze_fields failed to cache result for %s: %v", itemKey, err)
		} else {
			l.Debug("analyze_fields cached result for %s (%d JSON fields found)", itemKey, len(output.JSONFields))
		}
	}

	return &output, nil
}

// AnalyzeAllDocs orchestrates analyzing all mapped Terraform documentation files.
// It iterates over controller mappings, reads each mapped doc file from the repo
// directory, checks the cache, calls the agent for misses, and aggregates results.
// On per-doc failure, it logs the error and continues with the remaining docs.
//
// NOTE: This function retains its own concurrency implementation rather than using
// framework.AnalyzeAll because it includes pre-processing logic (collecting unique
// doc paths from mappings, reading file content from disk) that doesn't cleanly
// map to the framework's FileToAnalyze interface. The framework expects pre-loaded
// content, but here content loading is interleaved with the concurrent processing.
// The scan orchestrator also has its own analyzeDocsConcurrent wrapper that adds
// progress logging on top of this function's single-item calls.
func AnalyzeAllDocs(
	ctx context.Context,
	ag *agent.Agent,
	mappings []types.ControllerMapping,
	repoDir string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*AnalyzeAllDocsOutput, error) {
	return AnalyzeAllDocsParallel(ctx, ag, mappings, repoDir, resultCache, validator, 1, log...)
}

// AnalyzeAllDocsParallel orchestrates analyzing all mapped Terraform documentation
// files with bounded concurrency.
func AnalyzeAllDocsParallel(
	ctx context.Context,
	ag *agent.Agent,
	mappings []types.ControllerMapping,
	repoDir string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*AnalyzeAllDocsOutput, error) {
	l := resolveLogger(log)
	output := &AnalyzeAllDocsOutput{
		Results: make(map[string]*AnalyzeFieldsOutput),
	}

	if maxParallel <= 0 {
		maxParallel = 1
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

	l.Info("analyze_fields: processing %d unique doc files (parallelism: %d)", len(docPaths), maxParallel)

	type result struct {
		docPath string
		output  *AnalyzeFieldsOutput
		skipped bool
	}

	total := len(docPaths)
	results := make([]result, total)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var cacheHits, cacheMisses, completed atomic.Int32

	for i, docPath := range docPaths {
		select {
		case <-ctx.Done():
			break
		default:
		}

		wg.Add(1)
		go func(idx int, dp string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Read the doc content from the repo directory
			fullPath := filepath.Join(repoDir, dp)
			contentBytes, err := os.ReadFile(fullPath)
			if err != nil {
				l.Skip(dp, fmt.Sprintf("failed to read file: %v", err))
				done := int(completed.Add(1))
				l.Progress(done, total, "analyze_fields")
				results[idx] = result{docPath: dp, skipped: true}
				return
			}
			docContent := string(contentBytes)

			// Check cache
			itemKey := deriveItemKey(dp)
			inputParams := buildAnalyzeInputParams(dp, docContent)
			if resultCache != nil {
				entry, err := resultCache.Get(analyzeFieldsTool, itemKey, inputParams)
				if err == nil && entry != nil {
					var cachedOutput AnalyzeFieldsOutput
					if err := json.Unmarshal(entry.Result, &cachedOutput); err == nil {
						l.CacheHit(analyzeFieldsTool + "/" + itemKey)
						cacheHits.Add(1)
						done := int(completed.Add(1))
						l.Progress(done, total, "analyze_fields")
						results[idx] = result{docPath: dp, output: &cachedOutput}
						return
					}
				}
			}

			cacheMisses.Add(1)

			// Cache miss — call agent
			analyzeResult, err := AnalyzeDoc(ctx, ag, dp, docContent, resultCache, validator, l)
			done := int(completed.Add(1))
			if err != nil {
				if err == agent.ErrSkipItem {
					l.Skip(itemKey, "validation failed after retries")
				} else {
					l.Error("analyze_fields error for %s: %v", itemKey, err)
				}
				l.Progress(done, total, "analyze_fields")
				results[idx] = result{docPath: dp, skipped: true}
				return
			}

			l.Progress(done, total, "analyze_fields")
			results[idx] = result{docPath: dp, output: analyzeResult}
		}(i, docPath)
	}

	wg.Wait()

	// Aggregate results
	for _, r := range results {
		if r.output != nil {
			output.Results[r.docPath] = r.output
		} else if r.skipped {
			output.Skipped = append(output.Skipped, r.docPath)
		}
	}

	l.CacheSummary("analyze_fields", int(cacheHits.Load()), int(cacheMisses.Load()), len(output.Skipped))

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
