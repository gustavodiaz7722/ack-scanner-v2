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
)

const analyzeUpjetTool = "analyze_upjet"

// UpjetReferenceInfo describes a single reference declaration extracted from
// an Upjet config file.
type UpjetReferenceInfo struct {
	FieldName      string  `json:"field_name"`
	TargetResource string  `json:"target_resource"` // e.g., "aws_kms_key"
	Extractor      string  `json:"extractor,omitempty"`
	IsAmbiguous    bool    `json:"is_ambiguous"` // true if delete(r.References, ...) found
	Confidence     float64 `json:"confidence"`
}

// AnalyzeUpjetOutput is the agent's analysis of references in a single Upjet
// config file.
type AnalyzeUpjetOutput struct {
	ServiceName string               `json:"service_name"`
	References  []UpjetReferenceInfo `json:"references"`
}

// AnalyzeAllUpjetOutput is the aggregated analysis result for all Upjet config files.
type AnalyzeAllUpjetOutput struct {
	Results map[string]*AnalyzeUpjetOutput `json:"results"`
	Skipped []string                       `json:"skipped,omitempty"`
}

// AnalyzeUpjetConfig invokes the agent to analyze a single Upjet config file
// for reference declarations. The prompt instructs the agent to extract
// r.References[...] assignments and delete(r.References, ...) patterns.
// Results are cached per config file under the "analyze_upjet/" directory.
func AnalyzeUpjetConfig(
	ctx context.Context,
	ag *agent.Agent,
	filePath string,
	content string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*AnalyzeUpjetOutput, error) {
	l := resolveLogger(log)

	config := buildAnalyzeUpjetConfig()
	file := framework.FileToAnalyze{
		Key:      deriveUpjetItemKey(filePath),
		FilePath: filePath,
		Content:  content,
	}

	result, err := framework.AnalyzeOne(ctx, config, ag, file, resultCache, validator, l)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// AnalyzeAllUpjetConfigs orchestrates analyzing all mapped Upjet config files
// with bounded concurrency. It reads each mapped config file from the repo
// directory, delegates to the framework.AnalyzeAll function, and aggregates results.
func AnalyzeAllUpjetConfigs(
	ctx context.Context,
	ag *agent.Agent,
	mappings []UpjetMapping,
	repoDir string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*AnalyzeAllUpjetOutput, error) {
	l := resolveLogger(log)

	// Collect unique config file paths from all mappings
	seen := make(map[string]bool)
	var files []framework.FileToAnalyze
	for _, mapping := range mappings {
		for _, entry := range mapping.UpjetConfigs {
			if entry.FilePath != "" && !seen[entry.FilePath] {
				seen[entry.FilePath] = true

				// Read file content
				fullPath := filepath.Join(repoDir, entry.FilePath)
				contentBytes, err := os.ReadFile(fullPath)
				if err != nil {
					l.Skip(entry.FilePath, fmt.Sprintf("failed to read file: %v", err))
					continue
				}

				files = append(files, framework.FileToAnalyze{
					Key:      deriveUpjetItemKey(entry.FilePath),
					FilePath: entry.FilePath,
					Content:  string(contentBytes),
				})
			}
		}
	}

	config := buildAnalyzeUpjetConfig()

	frameworkResult, err := framework.AnalyzeAll(ctx, config, ag, files, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type
	output := &AnalyzeAllUpjetOutput{
		Results: make(map[string]*AnalyzeUpjetOutput, len(frameworkResult.Results)),
		Skipped: frameworkResult.Skipped,
	}

	for key, result := range frameworkResult.Results {
		r := result // avoid aliasing loop variable
		output.Results[key] = &r
	}

	return output, nil
}

// buildAnalyzeUpjetConfig returns the framework.AnalysisConfig for Upjet reference analysis.
func buildAnalyzeUpjetConfig() framework.AnalysisConfig[AnalyzeUpjetOutput] {
	return framework.AnalysisConfig[AnalyzeUpjetOutput]{
		ToolName:    analyzeUpjetTool,
		BuildPrompt: buildAnalyzeUpjetPrompt,
		ParseResult: parseAnalyzeUpjetResult,
		InputParams: buildAnalyzeUpjetInputParams,
	}
}

// deriveUpjetItemKey extracts a cache item key from an Upjet config file path.
// For example: "config/elasticache/config.go" → "elasticache"
func deriveUpjetItemKey(filePath string) string {
	// Normalize to forward slashes
	normalized := filepath.ToSlash(filePath)

	// Expected: config/<service>/config.go
	parts := strings.Split(normalized, "/")
	if len(parts) >= 3 && parts[0] == "config" && parts[len(parts)-1] == "config.go" {
		return parts[1]
	}

	// Fallback: use the parent directory name
	dir := filepath.Dir(filePath)
	return filepath.Base(dir)
}

// buildAnalyzeUpjetInputParams creates the input parameters used for cache hashing.
func buildAnalyzeUpjetInputParams(file framework.FileToAnalyze) map[string]any {
	return map[string]any{
		"file_path":      file.FilePath,
		"content_length": len(file.Content),
	}
}

// buildAnalyzeUpjetPrompt constructs the prompt sent to the agent for analyzing
// a single Upjet config file for reference declarations.
func buildAnalyzeUpjetPrompt(file framework.FileToAnalyze) string {
	var sb strings.Builder

	sb.WriteString("You are analyzing an Upjet/Crossplane AWS provider configuration file to extract all cross-resource reference declarations.\n\n")

	sb.WriteString("## Configuration File\n")
	fmt.Fprintf(&sb, "File: %s\n\n", file.FilePath)

	sb.WriteString("## Full File Content\n")
	sb.WriteString("```go\n")
	sb.WriteString(file.Content)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Instructions\n")
	sb.WriteString("Analyze the Go source code above and extract ALL cross-resource reference declarations.\n\n")
	sb.WriteString("Look for these patterns:\n\n")
	sb.WriteString("1. **Reference assignments** — `r.References[\"field_name\"] = config.Reference{TerraformName: \"aws_*\", ...}`\n")
	sb.WriteString("   - Extract the field name (the key in the map)\n")
	sb.WriteString("   - Extract the TerraformName value (target resource)\n")
	sb.WriteString("   - Extract the Extractor value if present\n")
	sb.WriteString("   - Set is_ambiguous to false\n")
	sb.WriteString("   - Set confidence to 1.0 (these are explicit declarations)\n\n")
	sb.WriteString("2. **Reference deletions** — `delete(r.References, \"field_name\")`\n")
	sb.WriteString("   - Extract the field name being deleted\n")
	sb.WriteString("   - Set target_resource to empty string (unknown)\n")
	sb.WriteString("   - Set is_ambiguous to true (deletion means the field can reference multiple resource types)\n")
	sb.WriteString("   - Set confidence to 0.8\n\n")
	sb.WriteString("If the file contains NO reference declarations (no r.References assignments and no delete(r.References, ...) calls), return an empty references array.\n\n")
	sb.WriteString("Extract the service name from the file path (e.g., config/elasticache/config.go → \"elasticache\").\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"service_name":"<service from file path>","references":[{"field_name":"<field key>","target_resource":"<terraform resource type or empty>","extractor":"<extractor value if present>","is_ambiguous":<true or false>,"confidence":<0.0 to 1.0>}]}`)
	sb.WriteString("\n\nIf no references are found, return an empty references array.\n")

	return sb.String()
}

// parseAnalyzeUpjetResult parses the agent's JSON response into an AnalyzeUpjetOutput.
func parseAnalyzeUpjetResult(response string) (AnalyzeUpjetOutput, error) {
	var result AnalyzeUpjetOutput
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return result, fmt.Errorf("parsing upjet analysis response: %w", err)
	}
	return result, nil
}

// ValidateAnalyzeUpjetOutput checks that an AnalyzeUpjetOutput conforms to
// the expected schema: each UpjetReferenceInfo must have field_name and
// confidence between 0 and 1. Non-ambiguous entries must have a target_resource.
func ValidateAnalyzeUpjetOutput(output *AnalyzeUpjetOutput) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	for i, ref := range output.References {
		if ref.FieldName == "" {
			return fmt.Errorf("references[%d]: field_name is empty", i)
		}
		if ref.Confidence < 0 || ref.Confidence > 1 {
			return fmt.Errorf("references[%d]: confidence must be between 0 and 1, got %f", i, ref.Confidence)
		}
		if !ref.IsAmbiguous && ref.TargetResource == "" {
			return fmt.Errorf("references[%d]: non-ambiguous reference must have a target_resource", i)
		}
	}

	return nil
}
