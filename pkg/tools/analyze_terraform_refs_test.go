package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"pgregory.net/rapid"
)

// --- Mock Bedrock Client for analyze_terraform_refs tests ---

type mockTFRefsAnalysisBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockTFRefsAnalysisBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeTFRefsAnalysisFinalTextResponse(text string) *bedrockruntime.ConverseOutput {
	tokens := int32(50)
	return &bedrockruntime.ConverseOutput{
		StopReason: brtypes.StopReasonEndTurn,
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: text},
				},
			},
		},
		Usage: &brtypes.TokenUsage{TotalTokens: &tokens},
	}
}

func newTFRefsAnalysisMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockTFRefsAnalysisBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Unit Tests ---

func TestAnalyzeTerraformRefs_Success(t *testing.T) {
	expectedOutput := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_ebs_snapshot",
		References: []TerraformReferenceInfo{
			{
				FieldName:      "volume_id",
				TargetResource: "aws_ebs_volume",
				ResolutionAttr: ".id",
				SignalType:     "hcl_example",
				Confidence:     0.95,
				Reasoning:      "HCL example shows volume_id = aws_ebs_volume.example.id",
			},
		},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newTFRefsAnalysisMockAgent(t, makeTFRefsAnalysisFinalTextResponse(string(responseJSON)))

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeTerraformRefs(
		context.Background(),
		ag,
		"website/docs/r/ebs_snapshot.html.markdown",
		"# Resource: aws_ebs_snapshot\n\nExample:\n```hcl\nresource \"aws_ebs_snapshot\" \"example\" {\n  volume_id = aws_ebs_volume.example.id\n}\n```",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("AnalyzeTerraformRefs failed: %v", err)
	}

	if result.ResourceType != "aws_ebs_snapshot" {
		t.Errorf("expected resource_type 'aws_ebs_snapshot', got %q", result.ResourceType)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(result.References))
	}
	if result.References[0].FieldName != "volume_id" {
		t.Errorf("expected field_name 'volume_id', got %q", result.References[0].FieldName)
	}
	if result.References[0].TargetResource != "aws_ebs_volume" {
		t.Errorf("expected target_resource 'aws_ebs_volume', got %q", result.References[0].TargetResource)
	}
	if result.References[0].ResolutionAttr != ".id" {
		t.Errorf("expected resolution_attr '.id', got %q", result.References[0].ResolutionAttr)
	}
	if result.References[0].SignalType != "hcl_example" {
		t.Errorf("expected signal_type 'hcl_example', got %q", result.References[0].SignalType)
	}
	if result.References[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.References[0].Confidence)
	}
}

func TestAnalyzeTerraformRefs_EmptyReferences(t *testing.T) {
	expectedOutput := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_cloudwatch_log_group",
		References:   []TerraformReferenceInfo{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newTFRefsAnalysisMockAgent(t, makeTFRefsAnalysisFinalTextResponse(string(responseJSON)))

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeTerraformRefs(
		context.Background(),
		ag,
		"website/docs/r/cloudwatch_log_group.html.markdown",
		"# Resource: aws_cloudwatch_log_group\n\nNo references to other resources.",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("AnalyzeTerraformRefs failed: %v", err)
	}

	if result.ResourceType != "aws_cloudwatch_log_group" {
		t.Errorf("expected resource_type 'aws_cloudwatch_log_group', got %q", result.ResourceType)
	}
	if len(result.References) != 0 {
		t.Errorf("expected 0 references, got %d", len(result.References))
	}
}

func TestAnalyzeTerraformRefs_MultipleSignalTypes(t *testing.T) {
	expectedOutput := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_autoscaling_group",
		References: []TerraformReferenceInfo{
			{
				FieldName:      "vpc_zone_identifier",
				TargetResource: "aws_subnet",
				ResolutionAttr: ".id",
				SignalType:     "hcl_list",
				Confidence:     0.95,
				Reasoning:      "HCL list pattern: vpc_zone_identifier = [aws_subnet.example.id]",
			},
			{
				FieldName:      "target_group_arns",
				TargetResource: "aws_alb_target_group",
				ResolutionAttr: ".arn",
				SignalType:     "backtick_mention",
				Confidence:     0.8,
				Reasoning:      "Description mentions `aws_alb_target_group` ARNs",
			},
			{
				FieldName:      "service_linked_role_arn",
				TargetResource: "aws_iam_service_linked_role",
				ResolutionAttr: ".arn",
				SignalType:     "argument_description",
				Confidence:     0.6,
				Reasoning:      "Description says 'The ARN of the service-linked role'",
			},
		},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newTFRefsAnalysisMockAgent(t, makeTFRefsAnalysisFinalTextResponse(string(responseJSON)))

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeTerraformRefs(
		context.Background(),
		ag,
		"website/docs/r/autoscaling_group.html.markdown",
		"# Resource: aws_autoscaling_group\n\nExample with references...",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("AnalyzeTerraformRefs failed: %v", err)
	}

	if len(result.References) != 3 {
		t.Fatalf("expected 3 references, got %d", len(result.References))
	}

	// Verify each signal type is represented
	signalTypes := map[string]bool{}
	for _, ref := range result.References {
		signalTypes[ref.SignalType] = true
	}
	if !signalTypes["hcl_list"] {
		t.Error("expected hcl_list signal type in results")
	}
	if !signalTypes["backtick_mention"] {
		t.Error("expected backtick_mention signal type in results")
	}
	if !signalTypes["argument_description"] {
		t.Error("expected argument_description signal type in results")
	}
}

func TestAnalyzeTerraformRefs_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	expectedOutput := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_instance",
		References: []TerraformReferenceInfo{
			{
				FieldName:      "subnet_id",
				TargetResource: "aws_subnet",
				ResolutionAttr: ".id",
				SignalType:     "hcl_example",
				Confidence:     0.95,
				Reasoning:      "HCL shows subnet_id = aws_subnet.main.id",
			},
		},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newTFRefsAnalysisMockAgent(t, makeTFRefsAnalysisFinalTextResponse(string(responseJSON)))

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	// First call — should hit agent
	result, err := AnalyzeTerraformRefs(
		context.Background(),
		ag,
		"website/docs/r/instance.html.markdown",
		"# Resource: aws_instance\n\nsubnet_id = aws_subnet.main.id",
		resultCache,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if result.ResourceType != "aws_instance" {
		t.Errorf("expected 'aws_instance', got %q", result.ResourceType)
	}

	// Second call — should use cache (no agent responses available)
	ag2 := newTFRefsAnalysisMockAgent(t) // no responses
	result2, err := AnalyzeTerraformRefs(
		context.Background(),
		ag2,
		"website/docs/r/instance.html.markdown",
		"# Resource: aws_instance\n\nsubnet_id = aws_subnet.main.id",
		resultCache,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("second call failed (should have used cache): %v", err)
	}
	if result2.ResourceType != "aws_instance" {
		t.Errorf("cached result has wrong resource_type: %q", result2.ResourceType)
	}
	if len(result2.References) != 1 {
		t.Errorf("cached result has wrong number of references: %d", len(result2.References))
	}
}

func TestAnalyzeAllTerraformRefs_Success(t *testing.T) {
	// Create a temp directory with doc files
	repoDir := t.TempDir()
	docsDir := filepath.Join(repoDir, "website", "docs", "r")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}

	doc1Content := "# Resource: aws_ebs_snapshot\n\nvolume_id = aws_ebs_volume.example.id"
	doc2Content := "# Resource: aws_instance\n\nsubnet_id = aws_subnet.main.id"
	os.WriteFile(filepath.Join(docsDir, "ebs_snapshot.html.markdown"), []byte(doc1Content), 0644)
	os.WriteFile(filepath.Join(docsDir, "instance.html.markdown"), []byte(doc2Content), 0644)

	output1 := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_ebs_snapshot",
		References: []TerraformReferenceInfo{
			{FieldName: "volume_id", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95, Reasoning: "HCL pattern"},
		},
	}
	output2 := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_instance",
		References: []TerraformReferenceInfo{
			{FieldName: "subnet_id", TargetResource: "aws_subnet", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95, Reasoning: "HCL pattern"},
		},
	}

	resp1, _ := json.Marshal(output1)
	resp2, _ := json.Marshal(output2)

	ag := newTFRefsAnalysisMockAgent(t,
		makeTFRefsAnalysisFinalTextResponse(string(resp1)),
		makeTFRefsAnalysisFinalTextResponse(string(resp2)),
	)

	mappings := []TerraformRefMapping{
		{
			ServiceName: "ebs",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_ebs_snapshot", DocFilePath: "website/docs/r/ebs_snapshot.html.markdown", Confidence: 0.95},
			},
		},
		{
			ServiceName: "ec2",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_instance", DocFilePath: "website/docs/r/instance.html.markdown", Confidence: 0.9},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeAllTerraformRefs(context.Background(), ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllTerraformRefs failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// Check that results are keyed by derived item key
	if _, ok := result.Results["ebs_snapshot"]; !ok {
		t.Error("expected result for key 'ebs_snapshot'")
	}
	if _, ok := result.Results["instance"]; !ok {
		t.Error("expected result for key 'instance'")
	}
}

func TestAnalyzeAllTerraformRefs_DeduplicatesDocFiles(t *testing.T) {
	repoDir := t.TempDir()
	docsDir := filepath.Join(repoDir, "website", "docs", "r")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}

	docContent := "# Resource: aws_s3_bucket\n\nsome content"
	os.WriteFile(filepath.Join(docsDir, "s3_bucket.html.markdown"), []byte(docContent), 0644)

	expectedOutput := AnalyzeTerraformRefsOutput{
		ResourceType: "aws_s3_bucket",
		References:   []TerraformReferenceInfo{},
	}
	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newTFRefsAnalysisMockAgent(t, makeTFRefsAnalysisFinalTextResponse(string(responseJSON)))

	// Same doc file in two mappings — should only be analyzed once
	mappings := []TerraformRefMapping{
		{
			ServiceName: "s3",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_s3_bucket", DocFilePath: "website/docs/r/s3_bucket.html.markdown", Confidence: 0.95},
			},
		},
		{
			ServiceName: "s3_also",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_s3_bucket", DocFilePath: "website/docs/r/s3_bucket.html.markdown", Confidence: 0.9},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeAllTerraformRefs(context.Background(), ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllTerraformRefs failed: %v", err)
	}

	// Should only have 1 result since the same doc file is only analyzed once
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result (deduplicated), got %d", len(result.Results))
	}
}

func TestAnalyzeAllTerraformRefs_SkipsMissingFiles(t *testing.T) {
	repoDir := t.TempDir()
	// Don't create the doc file — should be skipped

	ag := newTFRefsAnalysisMockAgent(t) // no responses needed

	mappings := []TerraformRefMapping{
		{
			ServiceName: "ec2",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_instance", DocFilePath: "website/docs/r/instance.html.markdown", Confidence: 0.95},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeAllTerraformRefs(context.Background(), ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllTerraformRefs failed: %v", err)
	}

	// File doesn't exist, so nothing to analyze
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for missing file, got %d", len(result.Results))
	}
}

func TestAnalyzeAllTerraformRefs_EmptyMappings(t *testing.T) {
	ag := newTFRefsAnalysisMockAgent(t) // no responses needed

	validator := &agent.JSONValidator{RequiredFields: []string{"resource_type", "references"}}

	result, err := AnalyzeAllTerraformRefs(context.Background(), ag, nil, "/nonexistent", nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllTerraformRefs failed: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty mappings, got %d", len(result.Results))
	}
}

// --- Prompt Content Tests ---

func TestBuildAnalyzeTerraformRefsPrompt_ContainsExpectedContent(t *testing.T) {
	file := frameworkFileToAnalyze("website/docs/r/ebs_snapshot.html.markdown", "# Resource: aws_ebs_snapshot\n\nExample content here")

	prompt := buildAnalyzeTerraformRefsPrompt(file)

	expectedPhrases := []string{
		"cross-resource reference",
		"ebs_snapshot.html.markdown",
		"aws_ebs_snapshot",
		"hcl_example",
		"hcl_list",
		"backtick_mention",
		"argument_description",
		"field = aws_<resource>.<name>.<attribute>",
		"field = [aws_<resource>.<name>.<attribute>]",
		".id",
		".arn",
		".name",
		"JSON-encoded",
		"confidence",
		"resource_type",
		"references",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstr(prompt, phrase) {
			t.Errorf("prompt missing expected phrase: %q", phrase)
		}
	}
}

func TestBuildAnalyzeTerraformRefsPrompt_ExcludesDocFields(t *testing.T) {
	file := frameworkFileToAnalyze("website/docs/r/test.html.markdown", "test content")

	prompt := buildAnalyzeTerraformRefsPrompt(file)

	// The prompt should mention excluding JSON/document fields
	if !containsSubstr(prompt, "JSON-encoded") {
		t.Error("prompt should mention excluding JSON-encoded fields")
	}
	if !containsSubstr(prompt, "policy documents") {
		t.Error("prompt should mention excluding policy documents")
	}
}

// --- Validation Tests ---

func TestValidateAnalyzeTerraformRefsOutput_ValidOutput(t *testing.T) {
	output := &AnalyzeTerraformRefsOutput{
		ResourceType: "aws_ebs_snapshot",
		References: []TerraformReferenceInfo{
			{FieldName: "volume_id", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95, Reasoning: "test"},
			{FieldName: "kms_key_id", TargetResource: "aws_kms_key", ResolutionAttr: ".arn", SignalType: "backtick_mention", Confidence: 0.8, Reasoning: "test"},
			{FieldName: "role_arn", TargetResource: "aws_iam_role", ResolutionAttr: ".arn", SignalType: "argument_description", Confidence: 0.6, Reasoning: "test"},
			{FieldName: "subnet_ids", TargetResource: "aws_subnet", ResolutionAttr: ".id", SignalType: "hcl_list", Confidence: 0.95, Reasoning: "test"},
		},
	}

	if err := ValidateAnalyzeTerraformRefsOutput(output); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateAnalyzeTerraformRefsOutput_EmptyReferences(t *testing.T) {
	output := &AnalyzeTerraformRefsOutput{
		ResourceType: "aws_cloudwatch_log_group",
		References:   []TerraformReferenceInfo{},
	}

	if err := ValidateAnalyzeTerraformRefsOutput(output); err != nil {
		t.Errorf("expected valid output with empty references, got error: %v", err)
	}
}

func TestValidateAnalyzeTerraformRefsOutput_NilOutput(t *testing.T) {
	if err := ValidateAnalyzeTerraformRefsOutput(nil); err == nil {
		t.Error("expected error for nil output")
	}
}

func TestValidateAnalyzeTerraformRefsOutput_EmptyFieldName(t *testing.T) {
	output := &AnalyzeTerraformRefsOutput{
		ResourceType: "aws_test",
		References: []TerraformReferenceInfo{
			{FieldName: "", TargetResource: "aws_ec2", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.9, Reasoning: "test"},
		},
	}
	if err := ValidateAnalyzeTerraformRefsOutput(output); err == nil {
		t.Error("expected error for empty field_name")
	}
}

func TestValidateAnalyzeTerraformRefsOutput_EmptyTargetResource(t *testing.T) {
	output := &AnalyzeTerraformRefsOutput{
		ResourceType: "aws_test",
		References: []TerraformReferenceInfo{
			{FieldName: "field", TargetResource: "", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.9, Reasoning: "test"},
		},
	}
	if err := ValidateAnalyzeTerraformRefsOutput(output); err == nil {
		t.Error("expected error for empty target_resource")
	}
}

func TestValidateAnalyzeTerraformRefsOutput_InvalidResolutionAttr(t *testing.T) {
	output := &AnalyzeTerraformRefsOutput{
		ResourceType: "aws_test",
		References: []TerraformReferenceInfo{
			{FieldName: "field", TargetResource: "aws_ec2", ResolutionAttr: ".key", SignalType: "hcl_example", Confidence: 0.9, Reasoning: "test"},
		},
	}
	if err := ValidateAnalyzeTerraformRefsOutput(output); err == nil {
		t.Error("expected error for invalid resolution_attr")
	}
}

func TestValidateAnalyzeTerraformRefsOutput_InvalidSignalType(t *testing.T) {
	output := &AnalyzeTerraformRefsOutput{
		ResourceType: "aws_test",
		References: []TerraformReferenceInfo{
			{FieldName: "field", TargetResource: "aws_ec2", ResolutionAttr: ".id", SignalType: "unknown_signal", Confidence: 0.9, Reasoning: "test"},
		},
	}
	if err := ValidateAnalyzeTerraformRefsOutput(output); err == nil {
		t.Error("expected error for invalid signal_type")
	}
}

func TestValidateAnalyzeTerraformRefsOutput_ConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"too high", 1.1},
		{"very negative", -5.0},
		{"way too high", 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &AnalyzeTerraformRefsOutput{
				ResourceType: "aws_test",
				References: []TerraformReferenceInfo{
					{FieldName: "field", TargetResource: "aws_ec2", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: tt.confidence, Reasoning: "test"},
				},
			}
			if err := ValidateAnalyzeTerraformRefsOutput(output); err == nil {
				t.Errorf("expected error for confidence %f", tt.confidence)
			}
		})
	}
}

// --- Item Key Tests ---

func TestDeriveTerraformRefsItemKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"website/docs/r/ebs_snapshot.html.markdown", "ebs_snapshot"},
		{"website/docs/r/autoscaling_group.html.markdown", "autoscaling_group"},
		{"website/docs/r/iam_role.html.markdown", "iam_role"},
		{"ebs_snapshot.html.markdown", "ebs_snapshot"},
		{"website/docs/r/s3_bucket.json", "s3_bucket"},
		{"simple_file", "simple_file"},
	}

	for _, tt := range tests {
		got := deriveTerraformRefsItemKey(tt.input)
		if got != tt.expected {
			t.Errorf("deriveTerraformRefsItemKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// --- Parse Response Tests ---

func TestParseTerraformRefsAnalysisResponse_ValidJSON(t *testing.T) {
	input := `{"resource_type":"aws_ebs_snapshot","references":[{"field_name":"volume_id","target_resource":"aws_ebs_volume","resolution_attr":".id","signal_type":"hcl_example","confidence":0.95,"reasoning":"HCL pattern"}]}`

	result, err := parseTerraformRefsAnalysisResponse(input)
	if err != nil {
		t.Fatalf("parseTerraformRefsAnalysisResponse failed: %v", err)
	}

	if result.ResourceType != "aws_ebs_snapshot" {
		t.Errorf("expected resource_type 'aws_ebs_snapshot', got %q", result.ResourceType)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(result.References))
	}
	if result.References[0].FieldName != "volume_id" {
		t.Errorf("expected field_name 'volume_id', got %q", result.References[0].FieldName)
	}
}

func TestParseTerraformRefsAnalysisResponse_InvalidJSON(t *testing.T) {
	_, err := parseTerraformRefsAnalysisResponse("not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseTerraformRefsAnalysisResponse_EmptyReferences(t *testing.T) {
	input := `{"resource_type":"aws_cloudwatch_log_group","references":[]}`

	result, err := parseTerraformRefsAnalysisResponse(input)
	if err != nil {
		t.Fatalf("parseTerraformRefsAnalysisResponse failed: %v", err)
	}

	if result.ResourceType != "aws_cloudwatch_log_group" {
		t.Errorf("expected resource_type 'aws_cloudwatch_log_group', got %q", result.ResourceType)
	}
	if len(result.References) != 0 {
		t.Errorf("expected 0 references, got %d", len(result.References))
	}
}

// --- Config Tests ---

func TestTerraformRefsAnalysisConfig_ToolName(t *testing.T) {
	config := TerraformRefsAnalysisConfig()
	if config.ToolName != "analyze_terraform_refs" {
		t.Errorf("expected ToolName 'analyze_terraform_refs', got %q", config.ToolName)
	}
}

// --- Property-Based Tests ---

// TestProperty_TerraformRefsOutputSchemaValidity verifies that for any Terraform
// reference analysis result, each entry SHALL have valid field_name, target_resource,
// resolution_attr, signal_type, and confidence.
//
// **Validates: Requirements 11.1, 11.2, 11.3, 11.4**
func TestProperty_TerraformRefsOutputSchemaValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validSignalTypes := []string{"hcl_example", "hcl_list", "backtick_mention", "argument_description"}
		validResolutionAttrs := []string{".id", ".arn", ".name"}

		numRefs := rapid.IntRange(0, 10).Draw(t, "numRefs")
		refs := make([]TerraformReferenceInfo, numRefs)

		for i := range refs {
			nameLen := rapid.IntRange(3, 30).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "nameByte"))
			}

			targetLen := rapid.IntRange(5, 30).Draw(t, "targetLen")
			targetBytes := make([]byte, targetLen)
			for j := range targetBytes {
				targetBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "targetByte"))
			}

			confidenceInt := rapid.IntRange(0, 100).Draw(t, "confidenceInt")
			confidence := float64(confidenceInt) / 100.0

			refs[i] = TerraformReferenceInfo{
				FieldName:      string(nameBytes),
				TargetResource: "aws_" + string(targetBytes),
				ResolutionAttr: rapid.SampledFrom(validResolutionAttrs).Draw(t, "resolutionAttr"),
				SignalType:     rapid.SampledFrom(validSignalTypes).Draw(t, "signalType"),
				Confidence:     confidence,
				Reasoning:      "generated reasoning",
			}
		}

		output := &AnalyzeTerraformRefsOutput{
			ResourceType: "aws_test_resource",
			References:   refs,
		}

		// Property: ValidateAnalyzeTerraformRefsOutput must pass for well-formed output
		if err := ValidateAnalyzeTerraformRefsOutput(output); err != nil {
			t.Fatalf("valid output failed validation: %v", err)
		}

		// Property: every field has non-empty field_name
		for i, ref := range output.References {
			if ref.FieldName == "" {
				t.Fatalf("references[%d]: field_name is empty", i)
			}
		}

		// Property: every target_resource is non-empty
		for i, ref := range output.References {
			if ref.TargetResource == "" {
				t.Fatalf("references[%d]: target_resource is empty", i)
			}
		}

		// Property: every signal_type is one of the four valid values
		validSignalTypeSet := map[string]bool{
			"hcl_example": true, "hcl_list": true,
			"backtick_mention": true, "argument_description": true,
		}
		for i, ref := range output.References {
			if !validSignalTypeSet[ref.SignalType] {
				t.Fatalf("references[%d]: signal_type %q is not valid", i, ref.SignalType)
			}
		}

		// Property: every confidence is between 0 and 1
		for i, ref := range output.References {
			if ref.Confidence < 0 || ref.Confidence > 1 {
				t.Fatalf("references[%d]: confidence %f is out of range [0, 1]", i, ref.Confidence)
			}
		}

		// Property: every resolution_attr is one of the three valid values
		validResAttrSet := map[string]bool{".id": true, ".arn": true, ".name": true}
		for i, ref := range output.References {
			if !validResAttrSet[ref.ResolutionAttr] {
				t.Fatalf("references[%d]: resolution_attr %q is not valid", i, ref.ResolutionAttr)
			}
		}
	})
}

// TestProperty_InvalidSignalTypeDetected verifies that ValidateAnalyzeTerraformRefsOutput
// correctly rejects outputs with invalid signal_type values.
//
// **Validates: Requirements 11.2**
func TestProperty_InvalidSignalTypeDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		invalidTypes := []string{
			"hcl", "list", "backtick", "description", "unknown",
			"HCL_EXAMPLE", "HCL_LIST", "BACKTICK_MENTION", "ARGUMENT_DESCRIPTION",
			"", "hcl_", "arg_description",
		}
		invalidType := rapid.SampledFrom(invalidTypes).Draw(t, "invalidType")

		output := &AnalyzeTerraformRefsOutput{
			ResourceType: "aws_test_resource",
			References: []TerraformReferenceInfo{
				{
					FieldName:      "test_field",
					TargetResource: "aws_ec2_instance",
					ResolutionAttr: ".id",
					SignalType:     invalidType,
					Confidence:     0.9,
					Reasoning:      "test reasoning",
				},
			},
		}

		err := ValidateAnalyzeTerraformRefsOutput(output)
		if err == nil {
			t.Fatalf("expected validation error for invalid signal_type %q, got nil", invalidType)
		}
	})
}

// TestProperty_InvalidResolutionAttrDetected verifies that ValidateAnalyzeTerraformRefsOutput
// correctly rejects outputs with invalid resolution_attr values.
//
// **Validates: Requirements 11.3**
func TestProperty_InvalidResolutionAttrDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		invalidAttrs := []string{
			"id", "arn", "name", ".key", ".endpoint", ".dns_name",
			"", ".ID", ".ARN", ".NAME", ".", "..",
		}
		invalidAttr := rapid.SampledFrom(invalidAttrs).Draw(t, "invalidAttr")

		output := &AnalyzeTerraformRefsOutput{
			ResourceType: "aws_test_resource",
			References: []TerraformReferenceInfo{
				{
					FieldName:      "test_field",
					TargetResource: "aws_ec2_instance",
					ResolutionAttr: invalidAttr,
					SignalType:     "hcl_example",
					Confidence:     0.9,
					Reasoning:      "test reasoning",
				},
			},
		}

		err := ValidateAnalyzeTerraformRefsOutput(output)
		if err == nil {
			t.Fatalf("expected validation error for invalid resolution_attr %q, got nil", invalidAttr)
		}
	})
}

// --- Helper ---

func frameworkFileToAnalyze(filePath, content string) framework.FileToAnalyze {
	return framework.FileToAnalyze{
		Key:      deriveTerraformRefsItemKey(filePath),
		FilePath: filePath,
		Content:  content,
	}
}
