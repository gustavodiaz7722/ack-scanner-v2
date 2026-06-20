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

const analyzeTerraformRefsTool = "analyze_terraform_refs"

// TerraformReferenceInfo describes a single cross-resource reference identified
// from a Terraform documentation file.
type TerraformReferenceInfo struct {
	FieldName      string  `json:"field_name"`
	TargetResource string  `json:"target_resource"` // e.g., "aws_ebs_volume"
	ResolutionAttr string  `json:"resolution_attr"` // ".id", ".arn", ".name"
	SignalType     string  `json:"signal_type"`     // "hcl_example", "hcl_list", "backtick_mention", "argument_description"
	Confidence     float64 `json:"confidence"`
	Reasoning      string  `json:"reasoning"`
}

// AnalyzeTerraformRefsOutput is the agent's analysis of cross-resource references
// in a single Terraform documentation file.
type AnalyzeTerraformRefsOutput struct {
	ResourceType string                   `json:"resource_type"`
	References   []TerraformReferenceInfo `json:"references"`
}

// AnalyzeAllTerraformRefsOutput is the aggregated analysis result for all TF docs.
type AnalyzeAllTerraformRefsOutput struct {
	Results map[string]*AnalyzeTerraformRefsOutput `json:"results"`
	Skipped []string                               `json:"skipped,omitempty"`
}

// TerraformRefsAnalysisConfig returns the framework.AnalysisConfig for the
// Terraform documentation reference analysis tool.
func TerraformRefsAnalysisConfig() framework.AnalysisConfig[AnalyzeTerraformRefsOutput] {
	return framework.AnalysisConfig[AnalyzeTerraformRefsOutput]{
		ToolName:    analyzeTerraformRefsTool,
		BuildPrompt: buildAnalyzeTerraformRefsPrompt,
		ParseResult: parseTerraformRefsAnalysisResponse,
		InputParams: buildAnalyzeTerraformRefsInputParams,
	}
}

// AnalyzeTerraformRefs invokes the agent to analyze a single Terraform documentation
// file for cross-resource references. It identifies HCL example patterns, list patterns,
// backtick mentions, and argument description references. Results are cached per doc file
// under "analyze_terraform_refs/".
func AnalyzeTerraformRefs(
	ctx context.Context,
	ag *agent.Agent,
	filePath string,
	content string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*AnalyzeTerraformRefsOutput, error) {
	l := resolveLogger(log)
	config := TerraformRefsAnalysisConfig()

	file := framework.FileToAnalyze{
		Key:      deriveTerraformRefsItemKey(filePath),
		FilePath: filePath,
		Content:  content,
	}

	result, err := framework.AnalyzeOne(ctx, config, ag, file, resultCache, validator, l)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// AnalyzeAllTerraformRefs orchestrates analyzing all mapped Terraform documentation
// files for cross-resource references. It uses the controller mapping results and
// reads each doc from the local Terraform repo directory.
func AnalyzeAllTerraformRefs(
	ctx context.Context,
	ag *agent.Agent,
	mappings []types.ControllerMapping,
	repoDir string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*AnalyzeAllTerraformRefsOutput, error) {
	l := resolveLogger(log)
	config := TerraformRefsAnalysisConfig()

	// Collect unique doc file paths from all mappings
	seen := make(map[string]bool)
	var files []framework.FileToAnalyze

	for _, mapping := range mappings {
		for _, entry := range mapping.TFDocFiles {
			if entry.DocFilePath == "" || seen[entry.DocFilePath] {
				continue
			}
			seen[entry.DocFilePath] = true

			fullPath := filepath.Join(repoDir, entry.DocFilePath)
			contentBytes, err := os.ReadFile(fullPath)
			if err != nil {
				l.Skip(entry.DocFilePath, fmt.Sprintf("failed to read file: %v", err))
				continue
			}

			files = append(files, framework.FileToAnalyze{
				Key:      deriveTerraformRefsItemKey(entry.DocFilePath),
				FilePath: entry.DocFilePath,
				Content:  string(contentBytes),
			})
		}
	}

	l.Info("%s: collected %d unique doc files from %d mappings", analyzeTerraformRefsTool, len(files), len(mappings))

	frameworkResult, err := framework.AnalyzeAll(ctx, config, ag, files, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type
	output := &AnalyzeAllTerraformRefsOutput{
		Results: make(map[string]*AnalyzeTerraformRefsOutput, len(frameworkResult.Results)),
		Skipped: frameworkResult.Skipped,
	}
	for key, result := range frameworkResult.Results {
		r := result // avoid aliasing
		output.Results[key] = &r
	}

	return output, nil
}

// deriveTerraformRefsItemKey extracts a cache item key from a Terraform doc file path.
// For example: "website/docs/r/ebs_snapshot.html.markdown" → "ebs_snapshot"
func deriveTerraformRefsItemKey(docFilePath string) string {
	base := filepath.Base(docFilePath)

	if idx := strings.Index(base, ".html.markdown"); idx > 0 {
		return base[:idx]
	}

	ext := filepath.Ext(base)
	if ext != "" {
		return strings.TrimSuffix(base, ext)
	}

	return base
}

// buildAnalyzeTerraformRefsPrompt constructs the prompt sent to the agent for
// analyzing a single Terraform documentation file for cross-resource references.
func buildAnalyzeTerraformRefsPrompt(file framework.FileToAnalyze) string {
	var sb strings.Builder

	sb.WriteString("You are analyzing a Terraform AWS provider resource documentation file to identify all fields that reference OTHER AWS resources.\n\n")

	sb.WriteString("## Documentation File\n")
	fmt.Fprintf(&sb, "File: %s\n\n", file.FilePath)

	sb.WriteString("## Full Documentation Content\n")
	sb.WriteString("```\n")
	sb.WriteString(file.Content)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Instructions\n")
	sb.WriteString("Analyze the documentation above and identify ALL fields (arguments) that reference other AWS resources. ")
	sb.WriteString("A reference field holds an identifier (ARN, ID, or name) pointing to a different AWS resource type.\n\n")

	sb.WriteString("Look for these signal types, in order of confidence:\n\n")

	sb.WriteString("### 1. HCL Example Patterns (signal_type: \"hcl_example\", confidence: 0.95)\n")
	sb.WriteString("In HCL code blocks, look for patterns like:\n")
	sb.WriteString("```\nfield = aws_<resource>.<name>.<attribute>\n```\n")
	sb.WriteString("Example: `volume_id = aws_ebs_volume.example.id`\n")
	sb.WriteString("This definitively identifies `volume_id` as referencing `aws_ebs_volume` via `.id`.\n\n")

	sb.WriteString("### 2. HCL List Patterns (signal_type: \"hcl_list\", confidence: 0.95)\n")
	sb.WriteString("In HCL code blocks, look for list patterns like:\n")
	sb.WriteString("```\nfield = [aws_<resource>.<name>.<attribute>]\n```\n")
	sb.WriteString("Example: `vpc_zone_identifier = [aws_subnet.example.id]`\n")
	sb.WriteString("This definitively identifies `vpc_zone_identifier` as referencing `aws_subnet` via `.id`.\n\n")

	sb.WriteString("### 3. Backtick Mentions (signal_type: \"backtick_mention\", confidence: 0.8)\n")
	sb.WriteString("In the \"Argument Reference\" section, look for backtick mentions of AWS resource types:\n")
	sb.WriteString("Example: \"A list of `aws_alb_target_group` ARNs\"\n")
	sb.WriteString("This identifies the field as referencing `aws_alb_target_group` via `.arn`.\n\n")

	sb.WriteString("### 4. Description Patterns (signal_type: \"argument_description\", confidence: 0.6)\n")
	sb.WriteString("In the \"Argument Reference\" section, look for descriptions containing:\n")
	sb.WriteString("- \"The ARN of the...\" → resolution_attr: \".arn\"\n")
	sb.WriteString("- \"The ID of the...\" → resolution_attr: \".id\"\n")
	sb.WriteString("- \"The name of the...\" → resolution_attr: \".name\"\n")
	sb.WriteString("When a specific resource type is identifiable from context, include it as target_resource.\n\n")

	sb.WriteString("## Exclusions\n")
	sb.WriteString("Do NOT include fields that:\n")
	sb.WriteString("- Accept JSON-encoded values or policy documents (these are document fields, not references)\n")
	sb.WriteString("- Are tags or tag-related fields\n")
	sb.WriteString("- Are the resource's own name/ID (self-referential primary key)\n")
	sb.WriteString("- Are free-form strings (descriptions, comments)\n")
	sb.WriteString("- Accept enum values with a fixed set of allowed strings\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"resource_type":"<the Terraform resource type, e.g. aws_ebs_snapshot>","references":[{"field_name":"<name of the field>","target_resource":"<referenced AWS resource type, e.g. aws_ebs_volume>","resolution_attr":"<.id, .arn, or .name>","signal_type":"<one of: hcl_example, hcl_list, backtick_mention, argument_description>","confidence":<0.0 to 1.0>,"reasoning":"<brief explanation of why this is a reference>"}]}`)
	sb.WriteString("\nIf no cross-resource references are found, return an empty references array.\n")

	return sb.String()
}

// parseTerraformRefsAnalysisResponse parses the agent's JSON response into an
// AnalyzeTerraformRefsOutput.
func parseTerraformRefsAnalysisResponse(response string) (AnalyzeTerraformRefsOutput, error) {
	var output AnalyzeTerraformRefsOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return AnalyzeTerraformRefsOutput{}, fmt.Errorf("parsing terraform refs analysis response: %w", err)
	}
	return output, nil
}

// buildAnalyzeTerraformRefsInputParams creates the input parameters used for cache hashing.
func buildAnalyzeTerraformRefsInputParams(file framework.FileToAnalyze) map[string]any {
	return map[string]any{
		"doc_file_path":  file.FilePath,
		"content_length": len(file.Content),
	}
}

// ValidateAnalyzeTerraformRefsOutput checks that an AnalyzeTerraformRefsOutput
// conforms to the expected schema.
func ValidateAnalyzeTerraformRefsOutput(output *AnalyzeTerraformRefsOutput) error {
	if output == nil {
		return fmt.Errorf("output is nil")
	}

	validSignalTypes := map[string]bool{
		"hcl_example":          true,
		"hcl_list":             true,
		"backtick_mention":     true,
		"argument_description": true,
	}

	validResolutionAttrs := map[string]bool{
		".id":   true,
		".arn":  true,
		".name": true,
	}

	for i, ref := range output.References {
		if ref.FieldName == "" {
			return fmt.Errorf("references[%d]: field_name is empty", i)
		}
		if ref.TargetResource == "" {
			return fmt.Errorf("references[%d]: target_resource is empty", i)
		}
		if !validResolutionAttrs[ref.ResolutionAttr] {
			return fmt.Errorf("references[%d]: resolution_attr must be \".id\", \".arn\", or \".name\", got %q", i, ref.ResolutionAttr)
		}
		if !validSignalTypes[ref.SignalType] {
			return fmt.Errorf("references[%d]: signal_type must be one of hcl_example, hcl_list, backtick_mention, argument_description, got %q", i, ref.SignalType)
		}
		if ref.Confidence < 0 || ref.Confidence > 1 {
			return fmt.Errorf("references[%d]: confidence must be between 0 and 1, got %f", i, ref.Confidence)
		}
	}

	return nil
}
