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
)

// --- Mock Bedrock Client for analyze_upjet tests ---

type mockAnalyzeUpjetBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockAnalyzeUpjetBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeAnalyzeUpjetTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func newAnalyzeUpjetMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockAnalyzeUpjetBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Tests ---

func TestAnalyzeUpjetConfig_WithReferences(t *testing.T) {
	resp := `{"service_name":"elasticache","references":[{"field_name":"parameter_group_name","target_resource":"aws_elasticache_parameter_group","extractor":"","is_ambiguous":false,"confidence":1.0},{"field_name":"kms_key_id","target_resource":"aws_kms_key","extractor":"common.TerraformID()","is_ambiguous":false,"confidence":1.0}]}`
	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(resp))

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeUpjetConfig(context.Background(), ag, "config/elasticache/config.go", "package elasticache\n// some content", nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeUpjetConfig failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if len(result.References) != 2 {
		t.Fatalf("expected 2 references, got %d", len(result.References))
	}

	ref0 := result.References[0]
	if ref0.FieldName != "parameter_group_name" {
		t.Errorf("expected field_name 'parameter_group_name', got %q", ref0.FieldName)
	}
	if ref0.TargetResource != "aws_elasticache_parameter_group" {
		t.Errorf("expected target_resource 'aws_elasticache_parameter_group', got %q", ref0.TargetResource)
	}
	if ref0.IsAmbiguous {
		t.Error("expected is_ambiguous false")
	}
	if ref0.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", ref0.Confidence)
	}

	ref1 := result.References[1]
	if ref1.Extractor != "common.TerraformID()" {
		t.Errorf("expected extractor 'common.TerraformID()', got %q", ref1.Extractor)
	}
}

func TestAnalyzeUpjetConfig_WithDeletePattern(t *testing.T) {
	resp := `{"service_name":"ec2","references":[{"field_name":"security_group_ids","target_resource":"aws_security_group","extractor":"","is_ambiguous":false,"confidence":1.0},{"field_name":"subnet_id","target_resource":"","extractor":"","is_ambiguous":true,"confidence":0.8}]}`
	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(resp))

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeUpjetConfig(context.Background(), ag, "config/ec2/config.go", "package ec2\n// content", nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeUpjetConfig failed: %v", err)
	}

	if len(result.References) != 2 {
		t.Fatalf("expected 2 references, got %d", len(result.References))
	}

	// First one is a normal reference
	if result.References[0].IsAmbiguous {
		t.Error("first reference should not be ambiguous")
	}

	// Second one is from delete pattern
	if !result.References[1].IsAmbiguous {
		t.Error("second reference should be ambiguous (from delete pattern)")
	}
	if result.References[1].FieldName != "subnet_id" {
		t.Errorf("expected field_name 'subnet_id', got %q", result.References[1].FieldName)
	}
}

func TestAnalyzeUpjetConfig_NoReferences(t *testing.T) {
	resp := `{"service_name":"route53","references":[]}`
	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(resp))

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeUpjetConfig(context.Background(), ag, "config/route53/config.go", "package route53\n// no references here", nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeUpjetConfig failed: %v", err)
	}

	if result.ServiceName != "route53" {
		t.Errorf("expected service_name 'route53', got %q", result.ServiceName)
	}
	if len(result.References) != 0 {
		t.Errorf("expected 0 references, got %d", len(result.References))
	}
}

func TestAnalyzeUpjetConfig_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	resp := `{"service_name":"s3","references":[{"field_name":"bucket","target_resource":"aws_s3_bucket","extractor":"","is_ambiguous":false,"confidence":1.0}]}`
	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(resp))

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	// First call — agent responds and result is cached
	result, err := AnalyzeUpjetConfig(context.Background(), ag, "config/s3/config.go", "package s3\n// content", resultCache, validator, logger.Nop())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(result.References))
	}

	// Second call — should use cache (agent with no responses will error if called)
	ag2 := newAnalyzeUpjetMockAgent(t) // no responses
	result2, err := AnalyzeUpjetConfig(context.Background(), ag2, "config/s3/config.go", "package s3\n// content", resultCache, validator, logger.Nop())
	if err != nil {
		t.Fatalf("second call failed (should have used cache): %v", err)
	}
	if len(result2.References) != 1 {
		t.Fatalf("expected 1 cached reference, got %d", len(result2.References))
	}
	if result2.References[0].FieldName != "bucket" {
		t.Errorf("cached result has wrong field_name: %q", result2.References[0].FieldName)
	}
}

func TestAnalyzeAllUpjetConfigs_Success(t *testing.T) {
	// Both responses use a generic structure. We use the same response for both
	// since with concurrent processing the order is non-deterministic.
	resp := `{"service_name":"testservice","references":[{"field_name":"bucket","target_resource":"aws_s3_bucket","extractor":"","is_ambiguous":false,"confidence":1.0}]}`
	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(resp), makeAnalyzeUpjetTextResponse(resp))

	// Create temp repo directory with config files
	repoDir := t.TempDir()
	s3Dir := filepath.Join(repoDir, "config", "s3")
	iamDir := filepath.Join(repoDir, "config", "iam")
	if err := os.MkdirAll(s3Dir, 0755); err != nil {
		t.Fatalf("failed to create s3 dir: %v", err)
	}
	if err := os.MkdirAll(iamDir, 0755); err != nil {
		t.Fatalf("failed to create iam dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s3Dir, "config.go"), []byte("package s3"), 0644); err != nil {
		t.Fatalf("failed to write s3/config.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(iamDir, "config.go"), []byte("package iam"), 0644); err != nil {
		t.Fatalf("failed to write iam/config.go: %v", err)
	}

	mappings := []UpjetMapping{
		{
			ServiceName: "s3",
			UpjetConfigs: []UpjetMappingEntry{
				{UpjetService: "s3", FilePath: "config/s3/config.go", Confidence: 0.95},
			},
		},
		{
			ServiceName: "iam",
			UpjetConfigs: []UpjetMappingEntry{
				{UpjetService: "iam", FilePath: "config/iam/config.go", Confidence: 0.9},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeAllUpjetConfigs(context.Background(), ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllUpjetConfigs failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}

	// Verify both keys exist (s3 and iam, derived from file paths)
	if _, ok := result.Results["s3"]; !ok {
		t.Error("expected result for key 's3'")
	}
	if _, ok := result.Results["iam"]; !ok {
		t.Error("expected result for key 'iam'")
	}

	// Verify each result has the expected structure
	for key, analysis := range result.Results {
		if len(analysis.References) != 1 {
			t.Errorf("result[%s]: expected 1 reference, got %d", key, len(analysis.References))
		}
	}
}

func TestAnalyzeAllUpjetConfigs_DeduplicatesFiles(t *testing.T) {
	resp := `{"service_name":"s3","references":[{"field_name":"bucket","target_resource":"aws_s3_bucket","extractor":"","is_ambiguous":false,"confidence":1.0}]}`
	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(resp))

	// Create temp repo directory
	repoDir := t.TempDir()
	s3Dir := filepath.Join(repoDir, "config", "s3")
	if err := os.MkdirAll(s3Dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s3Dir, "config.go"), []byte("package s3"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Two mappings reference the same file
	mappings := []UpjetMapping{
		{
			ServiceName: "s3",
			UpjetConfigs: []UpjetMappingEntry{
				{UpjetService: "s3", FilePath: "config/s3/config.go", Confidence: 0.95},
			},
		},
		{
			ServiceName: "s3-duplicate",
			UpjetConfigs: []UpjetMappingEntry{
				{UpjetService: "s3", FilePath: "config/s3/config.go", Confidence: 0.9},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeAllUpjetConfigs(context.Background(), ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllUpjetConfigs failed: %v", err)
	}

	// Should only have 1 result since both mappings point to the same file
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result (deduplicated), got %d", len(result.Results))
	}
}

func TestAnalyzeAllUpjetConfigs_SkipsMissingFiles(t *testing.T) {
	ag := newAnalyzeUpjetMockAgent(t) // no responses needed

	repoDir := t.TempDir() // empty directory

	mappings := []UpjetMapping{
		{
			ServiceName: "nonexistent",
			UpjetConfigs: []UpjetMappingEntry{
				{UpjetService: "nonexistent", FilePath: "config/nonexistent/config.go", Confidence: 0.95},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeAllUpjetConfigs(context.Background(), ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllUpjetConfigs failed: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for missing files, got %d", len(result.Results))
	}
}

func TestAnalyzeAllUpjetConfigs_EmptyMappings(t *testing.T) {
	ag := newAnalyzeUpjetMockAgent(t) // no responses needed

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeAllUpjetConfigs(context.Background(), ag, nil, "/tmp", nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAllUpjetConfigs failed: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty mappings, got %d", len(result.Results))
	}
}

func TestAnalyzeAllUpjetConfigs_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ag := newAnalyzeUpjetMockAgent(t, makeAnalyzeUpjetTextResponse(`{}`))

	// Create temp repo directory with a file
	repoDir := t.TempDir()
	s3Dir := filepath.Join(repoDir, "config", "s3")
	if err := os.MkdirAll(s3Dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s3Dir, "config.go"), []byte("package s3"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	mappings := []UpjetMapping{
		{
			ServiceName: "s3",
			UpjetConfigs: []UpjetMappingEntry{
				{UpjetService: "s3", FilePath: "config/s3/config.go", Confidence: 0.95},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	_, err := AnalyzeAllUpjetConfigs(ctx, ag, mappings, repoDir, nil, validator, 1, logger.Nop())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- Prompt and Helper Tests ---

func TestBuildAnalyzeUpjetPrompt_ContainsRequiredInfo(t *testing.T) {
	file := framework.FileToAnalyze{
		Key:      "elasticache",
		FilePath: "config/elasticache/config.go",
		Content:  `r.References["kms_key_id"] = config.Reference{TerraformName: "aws_kms_key"}`,
	}

	// Need to use the config's BuildPrompt
	config := buildAnalyzeUpjetConfig()
	prompt := config.BuildPrompt(file)

	// Verify prompt contains the file path
	if !contains(prompt, "config/elasticache/config.go") {
		t.Error("prompt does not contain file path")
	}

	// Verify prompt contains the file content
	if !contains(prompt, "kms_key_id") {
		t.Error("prompt does not contain file content")
	}

	// Verify prompt contains instructions about patterns
	if !contains(prompt, "r.References") {
		t.Error("prompt does not mention r.References pattern")
	}
	if !contains(prompt, "delete(r.References") {
		t.Error("prompt does not mention delete pattern")
	}

	// Verify prompt contains output schema
	if !contains(prompt, "field_name") {
		t.Error("prompt does not contain schema field 'field_name'")
	}
	if !contains(prompt, "target_resource") {
		t.Error("prompt does not contain schema field 'target_resource'")
	}
	if !contains(prompt, "extractor") {
		t.Error("prompt does not contain schema field 'extractor'")
	}
	if !contains(prompt, "is_ambiguous") {
		t.Error("prompt does not contain schema field 'is_ambiguous'")
	}
	if !contains(prompt, "confidence") {
		t.Error("prompt does not contain schema field 'confidence'")
	}
	if !contains(prompt, "service_name") {
		t.Error("prompt does not contain schema field 'service_name'")
	}
}

func TestDeriveUpjetItemKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"config/elasticache/config.go", "elasticache"},
		{"config/s3/config.go", "s3"},
		{"config/iam/config.go", "iam"},
		{"config/ec2/config.go", "ec2"},
		{"config/opensearch/config.go", "opensearch"},
		// Fallback case
		{"other/path/file.go", "path"},
	}

	for _, tc := range tests {
		result := deriveUpjetItemKey(tc.input)
		if result != tc.expected {
			t.Errorf("deriveUpjetItemKey(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestBuildAnalyzeUpjetInputParams(t *testing.T) {
	file := framework.FileToAnalyze{
		Key:      "s3",
		FilePath: "config/s3/config.go",
		Content:  "package s3\n// content here",
	}

	params := buildAnalyzeUpjetInputParams(file)

	if params["file_path"] != "config/s3/config.go" {
		t.Errorf("expected file_path 'config/s3/config.go', got %v", params["file_path"])
	}
	if params["content_length"] != len("package s3\n// content here") {
		t.Errorf("unexpected content_length: %v", params["content_length"])
	}
}

func TestParseAnalyzeUpjetResult_ValidJSON(t *testing.T) {
	input := `{"service_name":"elasticache","references":[{"field_name":"kms_key_id","target_resource":"aws_kms_key","extractor":"","is_ambiguous":false,"confidence":1.0}]}`

	result, err := parseAnalyzeUpjetResult(input)
	if err != nil {
		t.Fatalf("parseAnalyzeUpjetResult failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(result.References))
	}
	if result.References[0].FieldName != "kms_key_id" {
		t.Errorf("expected field_name 'kms_key_id', got %q", result.References[0].FieldName)
	}
}

func TestParseAnalyzeUpjetResult_InvalidJSON(t *testing.T) {
	_, err := parseAnalyzeUpjetResult("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAnalyzeUpjetResult_EmptyReferences(t *testing.T) {
	input := `{"service_name":"route53","references":[]}`

	result, err := parseAnalyzeUpjetResult(input)
	if err != nil {
		t.Fatalf("parseAnalyzeUpjetResult failed: %v", err)
	}

	if result.ServiceName != "route53" {
		t.Errorf("expected service_name 'route53', got %q", result.ServiceName)
	}
	if len(result.References) != 0 {
		t.Errorf("expected 0 references, got %d", len(result.References))
	}
}

func TestValidateAnalyzeUpjetOutput_Valid(t *testing.T) {
	output := &AnalyzeUpjetOutput{
		ServiceName: "s3",
		References: []UpjetReferenceInfo{
			{FieldName: "bucket", TargetResource: "aws_s3_bucket", Confidence: 1.0},
			{FieldName: "ambiguous_field", TargetResource: "", IsAmbiguous: true, Confidence: 0.8},
		},
	}

	if err := ValidateAnalyzeUpjetOutput(output); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateAnalyzeUpjetOutput_NilOutput(t *testing.T) {
	if err := ValidateAnalyzeUpjetOutput(nil); err == nil {
		t.Error("expected error for nil output")
	}
}

func TestValidateAnalyzeUpjetOutput_EmptyFieldName(t *testing.T) {
	output := &AnalyzeUpjetOutput{
		ServiceName: "s3",
		References: []UpjetReferenceInfo{
			{FieldName: "", TargetResource: "aws_s3_bucket", Confidence: 1.0},
		},
	}

	if err := ValidateAnalyzeUpjetOutput(output); err == nil {
		t.Error("expected error for empty field_name")
	}
}

func TestValidateAnalyzeUpjetOutput_InvalidConfidence(t *testing.T) {
	output := &AnalyzeUpjetOutput{
		ServiceName: "s3",
		References: []UpjetReferenceInfo{
			{FieldName: "bucket", TargetResource: "aws_s3_bucket", Confidence: 1.5},
		},
	}

	if err := ValidateAnalyzeUpjetOutput(output); err == nil {
		t.Error("expected error for confidence > 1")
	}
}

func TestValidateAnalyzeUpjetOutput_MissingTargetForNonAmbiguous(t *testing.T) {
	output := &AnalyzeUpjetOutput{
		ServiceName: "s3",
		References: []UpjetReferenceInfo{
			{FieldName: "bucket", TargetResource: "", IsAmbiguous: false, Confidence: 1.0},
		},
	}

	if err := ValidateAnalyzeUpjetOutput(output); err == nil {
		t.Error("expected error for non-ambiguous reference with empty target_resource")
	}
}

func TestAnalyzeUpjetOutput_JSONRoundTrip(t *testing.T) {
	original := AnalyzeUpjetOutput{
		ServiceName: "elasticache",
		References: []UpjetReferenceInfo{
			{FieldName: "parameter_group_name", TargetResource: "aws_elasticache_parameter_group", Confidence: 1.0},
			{FieldName: "kms_key_id", TargetResource: "aws_kms_key", Extractor: "common.TerraformID()", Confidence: 1.0},
			{FieldName: "ambiguous_field", TargetResource: "", IsAmbiguous: true, Confidence: 0.8},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded AnalyzeUpjetOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ServiceName != original.ServiceName {
		t.Errorf("service_name mismatch: %q vs %q", decoded.ServiceName, original.ServiceName)
	}
	if len(decoded.References) != len(original.References) {
		t.Fatalf("references length mismatch: %d vs %d", len(decoded.References), len(original.References))
	}
	for i, ref := range decoded.References {
		if ref.FieldName != original.References[i].FieldName {
			t.Errorf("entry %d field_name mismatch", i)
		}
		if ref.TargetResource != original.References[i].TargetResource {
			t.Errorf("entry %d target_resource mismatch", i)
		}
		if ref.Extractor != original.References[i].Extractor {
			t.Errorf("entry %d extractor mismatch", i)
		}
		if ref.IsAmbiguous != original.References[i].IsAmbiguous {
			t.Errorf("entry %d is_ambiguous mismatch", i)
		}
		if ref.Confidence != original.References[i].Confidence {
			t.Errorf("entry %d confidence mismatch", i)
		}
	}
}
