package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// mockAnalyzeModelsClient implements agent.BedrockClient for analyze_models tests.
type mockAnalyzeModelsClient struct {
	responseFunc func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error)
	callIdx      atomic.Int32
}

func (m *mockAnalyzeModelsClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	return m.responseFunc(idx, params)
}

func makeAnalyzeModelsTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func TestAnalyzeModel_WithReferences(t *testing.T) {
	expectedOutput := AnalyzeModelOutput{
		ServiceName: "elasticache",
		References: []ModelReferenceInfo{
			{
				FieldName:      "SubnetGroupName",
				TargetService:  "elasticache",
				TargetResource: "SubnetGroup",
				SignalType:     "name_suffix",
				Confidence:     0.6,
				Reasoning:      "Field name ends in Name and documentation references a subnet group resource",
			},
			{
				FieldName:      "KmsKeyId",
				TargetService:  "kms",
				TargetResource: "Key",
				SignalType:     "id_suffix",
				Confidence:     0.8,
				Reasoning:      "Field name ends in Id and documentation says 'The ID of the KMS key'",
			},
			{
				FieldName:      "NotificationTopicArn",
				TargetService:  "sns",
				TargetResource: "Topic",
				SignalType:     "arn_suffix",
				Confidence:     0.85,
				Reasoning:      "Field name ends in Arn and documentation mentions SNS topic",
			},
		},
	}

	responseJSON, _ := json.Marshal(expectedOutput)

	client := &mockAnalyzeModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeAnalyzeModelsTextResponse(string(responseJSON)), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	modelContent := `{"shapes":{"com.amazonaws.elasticache#CacheCluster":{"type":"structure","members":{"SubnetGroupName":{"target":"com.amazonaws.elasticache#String","traits":{"smithy.api#documentation":"The name of the subnet group"}},"KmsKeyId":{"target":"com.amazonaws.elasticache#String","traits":{"smithy.api#documentation":"The ID of the KMS key used for encryption"}}}}}}`

	result, err := AnalyzeModel(
		context.Background(), ag,
		"codegen/sdk-codegen/aws-models/elasticache.json",
		modelContent,
		[]string{"CacheCluster", "ReplicationGroup"},
		nil, validator,
	)
	if err != nil {
		t.Fatalf("AnalyzeModel failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if len(result.References) != 3 {
		t.Fatalf("expected 3 references, got %d", len(result.References))
	}

	// Verify first reference
	ref := result.References[0]
	if ref.FieldName != "SubnetGroupName" {
		t.Errorf("expected field_name 'SubnetGroupName', got %q", ref.FieldName)
	}
	if ref.SignalType != "name_suffix" {
		t.Errorf("expected signal_type 'name_suffix', got %q", ref.SignalType)
	}
	if ref.Confidence != 0.6 {
		t.Errorf("expected confidence 0.6, got %f", ref.Confidence)
	}

	// Verify second reference
	ref = result.References[1]
	if ref.FieldName != "KmsKeyId" {
		t.Errorf("expected field_name 'KmsKeyId', got %q", ref.FieldName)
	}
	if ref.TargetService != "kms" {
		t.Errorf("expected target_service 'kms', got %q", ref.TargetService)
	}
}

func TestAnalyzeModel_NoReferences(t *testing.T) {
	expectedOutput := AnalyzeModelOutput{
		ServiceName: "s3",
		References:  []ModelReferenceInfo{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)

	client := &mockAnalyzeModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeAnalyzeModelsTextResponse(string(responseJSON)), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeModel(
		context.Background(), ag,
		"codegen/sdk-codegen/aws-models/s3.json",
		`{"shapes":{}}`,
		[]string{"Bucket"},
		nil, validator,
	)
	if err != nil {
		t.Fatalf("AnalyzeModel failed: %v", err)
	}

	if result.ServiceName != "s3" {
		t.Errorf("expected service_name 's3', got %q", result.ServiceName)
	}
	if len(result.References) != 0 {
		t.Errorf("expected 0 references, got %d", len(result.References))
	}
}

func TestAnalyzeModel_ARNTrait(t *testing.T) {
	expectedOutput := AnalyzeModelOutput{
		ServiceName: "autoscaling",
		References: []ModelReferenceInfo{
			{
				FieldName:      "ServiceLinkedRoleARN",
				TargetService:  "iam",
				TargetResource: "Role",
				SignalType:     "arn_trait",
				Confidence:     1.0,
				Reasoning:      "Field has aws.api#arnReference trait",
			},
		},
	}

	responseJSON, _ := json.Marshal(expectedOutput)

	client := &mockAnalyzeModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeAnalyzeModelsTextResponse(string(responseJSON)), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeModel(
		context.Background(), ag,
		"codegen/sdk-codegen/aws-models/auto-scaling.json",
		`{"shapes":{"com.amazonaws.autoscaling#ServiceLinkedRoleARN":{"type":"string","traits":{"aws.api#arnReference":{"service":"iam","resource":"Role"}}}}}`,
		[]string{"AutoScalingGroup"},
		nil, validator,
	)
	if err != nil {
		t.Fatalf("AnalyzeModel failed: %v", err)
	}

	if len(result.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(result.References))
	}
	if result.References[0].Confidence != 1.0 {
		t.Errorf("expected confidence 1.0 for arn_trait, got %f", result.References[0].Confidence)
	}
	if result.References[0].SignalType != "arn_trait" {
		t.Errorf("expected signal_type 'arn_trait', got %q", result.References[0].SignalType)
	}
}

func TestAnalyzeAllModels_MultipleModels(t *testing.T) {
	controllers := []types.ControllerInfo{
		{
			ServiceName: "elasticache",
			RepoName:    "elasticache-controller",
			Resources:   []types.ResourceInfo{{Kind: "CacheCluster"}},
		},
		{
			ServiceName: "ec2",
			RepoName:    "ec2-controller",
			Resources:   []types.ResourceInfo{{Kind: "Instance"}, {Kind: "SecurityGroup"}},
		},
	}

	mappings := []ModelMapping{
		{ServiceName: "elasticache", ModelFile: "codegen/sdk-codegen/aws-models/elasticache.json", Confidence: 1.0},
		{ServiceName: "ec2", ModelFile: "codegen/sdk-codegen/aws-models/ec2.json", Confidence: 1.0},
	}

	responses := map[string]string{
		"elasticache": `{"service_name":"elasticache","references":[{"field_name":"KmsKeyId","target_service":"kms","target_resource":"Key","signal_type":"id_suffix","confidence":0.8,"reasoning":"Field name ends in Id"}]}`,
		"ec2":         `{"service_name":"ec2","references":[{"field_name":"SubnetId","target_service":"ec2","target_resource":"Subnet","signal_type":"id_suffix","confidence":0.8,"reasoning":"The ID of the subnet"}]}`,
	}

	client := &mockAnalyzeModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			// Detect which model is being analyzed from the prompt
			for _, msg := range input.Messages {
				for _, block := range msg.Content {
					if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
						for svc, resp := range responses {
							if strings.Contains(textBlock.Value, svc+".json") {
								return makeAnalyzeModelsTextResponse(resp), nil
							}
						}
					}
				}
			}
			return makeAnalyzeModelsTextResponse(`{"service_name":"unknown","references":[]}`), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	// Create temp dir with mock model files
	tmpDir := t.TempDir()
	modelsDir := tmpDir + "/codegen/sdk-codegen/aws-models"
	if err := createDirAll(modelsDir); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Write mock model files
	writeTestFile(t, modelsDir+"/elasticache.json", `{"shapes":{"com.amazonaws.elasticache#CacheCluster":{"type":"structure","members":{"KmsKeyId":{"target":"com.amazonaws.elasticache#String"}}}}}`)
	writeTestFile(t, modelsDir+"/ec2.json", `{"shapes":{"com.amazonaws.ec2#Instance":{"type":"structure","members":{"SubnetId":{"target":"com.amazonaws.ec2#String"}}}}}`)

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeAllModels(ctx, ag, mappings, tmpDir, controllers, nil, validator, 1)
	if err != nil {
		t.Fatalf("AnalyzeAllModels failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	// Check elasticache result
	ecResult, ok := result.Results["elasticache"]
	if !ok {
		t.Fatal("missing result for elasticache")
	}
	if len(ecResult.References) != 1 {
		t.Errorf("expected 1 reference for elasticache, got %d", len(ecResult.References))
	}

	// Check ec2 result
	ec2Result, ok := result.Results["ec2"]
	if !ok {
		t.Fatal("missing result for ec2")
	}
	if len(ec2Result.References) != 1 {
		t.Errorf("expected 1 reference for ec2, got %d", len(ec2Result.References))
	}

	if len(result.Skipped) != 0 {
		t.Errorf("expected no skipped models, got %v", result.Skipped)
	}
}

func TestAnalyzeAllModels_SkipsEmptyModelFile(t *testing.T) {
	controllers := []types.ControllerInfo{
		{
			ServiceName: "s3",
			RepoName:    "s3-controller",
			Resources:   []types.ResourceInfo{{Kind: "Bucket"}},
		},
	}

	// Mapping with empty model_file should be skipped
	mappings := []ModelMapping{
		{ServiceName: "s3", ModelFile: "", Confidence: 0.0, NoMatchReason: "No model found"},
	}

	client := &mockAnalyzeModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			t.Fatal("agent should not have been called for empty model file")
			return nil, nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name", "references"}}

	result, err := AnalyzeAllModels(ctx, ag, mappings, t.TempDir(), controllers, nil, validator, 1)
	if err != nil {
		t.Fatalf("AnalyzeAllModels failed: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty model file, got %d", len(result.Results))
	}
}

func TestBuildAnalyzeModelPrompt_ContainsSignalHierarchy(t *testing.T) {
	file := framework_FileToAnalyze{
		Key:      "elasticache",
		FilePath: "codegen/sdk-codegen/aws-models/elasticache.json",
		Content:  `{"shapes":{}}`,
	}

	prompt := buildAnalyzeModelPrompt(file)

	// Verify prompt contains signal hierarchy instructions
	if !strings.Contains(prompt, "aws.api#arnReference") {
		t.Error("prompt missing arn_trait signal")
	}
	if !strings.Contains(prompt, "confidence: 1.0") {
		t.Error("prompt missing arn_trait confidence level")
	}
	if !strings.Contains(prompt, "ARN/Arn") {
		t.Error("prompt missing ARN suffix signal")
	}
	if !strings.Contains(prompt, "confidence: 0.85") {
		t.Error("prompt missing ARN suffix confidence level")
	}
	if !strings.Contains(prompt, "Id/ID") {
		t.Error("prompt missing ID suffix signal")
	}
	if !strings.Contains(prompt, "confidence: 0.8") {
		t.Error("prompt missing ID suffix confidence level")
	}
	if !strings.Contains(prompt, "Name") {
		t.Error("prompt missing Name suffix signal")
	}
	if !strings.Contains(prompt, "confidence: 0.6") {
		t.Error("prompt missing Name suffix confidence level")
	}

	// Verify prompt contains exclusion rules
	if !strings.Contains(prompt, "JSON/document fields") {
		t.Error("prompt missing JSON/document exclusion")
	}
	if !strings.Contains(prompt, "Tags") {
		t.Error("prompt missing Tags exclusion")
	}
	if !strings.Contains(prompt, "Enum fields") {
		t.Error("prompt missing Enum exclusion")
	}
	if !strings.Contains(prompt, "Self-referential") {
		t.Error("prompt missing self-referential exclusion")
	}

	// Verify prompt contains file path
	if !strings.Contains(prompt, "elasticache.json") {
		t.Error("prompt missing file path")
	}

	// Verify prompt contains JSON output schema
	if !strings.Contains(prompt, "service_name") {
		t.Error("prompt missing service_name in output schema")
	}
	if !strings.Contains(prompt, "signal_type") {
		t.Error("prompt missing signal_type in output schema")
	}
}

func TestBuildAnalyzeModelPrompt_ContainsContent(t *testing.T) {
	content := `{"shapes":{"com.amazonaws.ec2#Instance":{"type":"structure"}}}`
	file := framework_FileToAnalyze{
		Key:      "ec2",
		FilePath: "codegen/sdk-codegen/aws-models/ec2.json",
		Content:  content,
	}

	prompt := buildAnalyzeModelPrompt(file)

	if !strings.Contains(prompt, content) {
		t.Error("prompt missing model content")
	}
}

func TestParseAnalyzeModelResult_ValidJSON(t *testing.T) {
	input := `{"service_name":"elasticache","references":[{"field_name":"KmsKeyId","target_service":"kms","target_resource":"Key","signal_type":"id_suffix","confidence":0.8,"reasoning":"ID of KMS key"}]}`

	result, err := parseAnalyzeModelResult(input)
	if err != nil {
		t.Fatalf("parseAnalyzeModelResult failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if len(result.References) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(result.References))
	}
	if result.References[0].FieldName != "KmsKeyId" {
		t.Errorf("expected field_name 'KmsKeyId', got %q", result.References[0].FieldName)
	}
}

func TestParseAnalyzeModelResult_EmptyReferences(t *testing.T) {
	input := `{"service_name":"s3","references":[]}`

	result, err := parseAnalyzeModelResult(input)
	if err != nil {
		t.Fatalf("parseAnalyzeModelResult failed: %v", err)
	}

	if result.ServiceName != "s3" {
		t.Errorf("expected service_name 's3', got %q", result.ServiceName)
	}
	if len(result.References) != 0 {
		t.Errorf("expected 0 references, got %d", len(result.References))
	}
}

func TestParseAnalyzeModelResult_InvalidJSON(t *testing.T) {
	_, err := parseAnalyzeModelResult("not json")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestDeriveModelItemKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"codegen/sdk-codegen/aws-models/elasticache.json", "elasticache"},
		{"codegen/sdk-codegen/aws-models/application-auto-scaling.json", "application-auto-scaling"},
		{"codegen/sdk-codegen/aws-models/ec2.json", "ec2"},
		{"s3.json", "s3"},
		{"some-file.txt", "some-file"},
		{"noext", "noext"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := deriveModelItemKey(tc.input)
			if result != tc.expected {
				t.Errorf("deriveModelItemKey(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestFilterModelContent_WithResources(t *testing.T) {
	modelJSON := `{
		"shapes": {
			"com.amazonaws.elasticache#CreateCacheClusterRequest": {
				"type": "structure",
				"members": {
					"CacheClusterId": {"target": "com.amazonaws.elasticache#String"},
					"SubnetGroupName": {"target": "com.amazonaws.elasticache#String"}
				}
			},
			"com.amazonaws.elasticache#DescribeReplicationGroupsResult": {
				"type": "structure",
				"members": {
					"ReplicationGroups": {"target": "com.amazonaws.elasticache#ReplicationGroupList"}
				}
			},
			"com.amazonaws.elasticache#UnrelatedShape": {
				"type": "structure",
				"members": {
					"Foo": {"target": "com.amazonaws.elasticache#String"}
				}
			}
		}
	}`

	result := filterModelContent(modelJSON, []string{"CacheCluster", "ReplicationGroup"})

	// Should contain shapes related to CacheCluster and ReplicationGroup
	if !strings.Contains(result, "CreateCacheClusterRequest") {
		t.Error("filtered content should include CacheCluster-related shape")
	}
	if !strings.Contains(result, "DescribeReplicationGroupsResult") {
		t.Error("filtered content should include ReplicationGroup-related shape")
	}
	// Should NOT contain unrelated shapes
	if strings.Contains(result, "UnrelatedShape") {
		t.Error("filtered content should not include unrelated shapes")
	}
}

func TestFilterModelContent_NoResources(t *testing.T) {
	content := `{"shapes": {"foo": {"type": "string"}}}`
	result := filterModelContent(content, nil)

	// Should return truncated content when no resources specified
	if result == "" {
		t.Error("expected non-empty content when no resources specified")
	}
	if !strings.Contains(result, "foo") {
		t.Error("expected content to contain original shapes when no resources specified")
	}
}

func TestFilterModelContent_InvalidJSON(t *testing.T) {
	content := "not valid json at all"
	result := filterModelContent(content, []string{"Bucket"})

	// Should return truncated raw content on parse failure
	if result == "" {
		t.Error("expected non-empty content on parse failure")
	}
	if !strings.Contains(result, "not valid json") {
		t.Error("expected raw content to be preserved on parse failure")
	}
}

func TestFilterModelContent_LargeContent(t *testing.T) {
	// Generate content larger than the truncation limit
	largeContent := strings.Repeat("a", 60000)
	result := filterModelContent(largeContent, []string{"Bucket"})

	// Should be truncated
	if len(result) >= 60000 {
		t.Error("expected content to be truncated")
	}
	if !strings.Contains(result, "[truncated]") {
		t.Error("expected truncation notice")
	}
}

func TestTruncateContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLen   int
		wantLen  int
		hasTrunc bool
	}{
		{"short content", "hello", 10, 5, false},
		{"exact limit", "hello", 5, 5, false},
		{"needs truncation", "hello world", 5, 5 + len("\n... [truncated]"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateContent(tc.content, tc.maxLen)
			if tc.hasTrunc {
				if !strings.HasSuffix(result, "[truncated]") {
					t.Error("expected truncation notice")
				}
			} else {
				if result != tc.content {
					t.Errorf("expected original content, got %q", result)
				}
			}
		})
	}
}

func TestValidateAnalyzeModelOutput_Valid(t *testing.T) {
	output := &AnalyzeModelOutput{
		ServiceName: "ec2",
		References: []ModelReferenceInfo{
			{FieldName: "SubnetId", SignalType: "id_suffix", Confidence: 0.8, Reasoning: "test"},
			{FieldName: "RoleArn", SignalType: "arn_suffix", Confidence: 0.85, Reasoning: "test"},
			{FieldName: "TopicArn", SignalType: "arn_trait", Confidence: 1.0, Reasoning: "test"},
			{FieldName: "GroupName", SignalType: "name_suffix", Confidence: 0.6, Reasoning: "test"},
			{FieldName: "TargetId", SignalType: "doc_mention", Confidence: 0.8, Reasoning: "test"},
		},
	}

	if err := ValidateAnalyzeModelOutput(output); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateAnalyzeModelOutput_EmptyReferences(t *testing.T) {
	output := &AnalyzeModelOutput{
		ServiceName: "s3",
		References:  []ModelReferenceInfo{},
	}

	if err := ValidateAnalyzeModelOutput(output); err != nil {
		t.Errorf("expected valid output with empty references, got error: %v", err)
	}
}

func TestValidateAnalyzeModelOutput_NilOutput(t *testing.T) {
	err := ValidateAnalyzeModelOutput(nil)
	if err == nil {
		t.Error("expected error for nil output")
	}
}

func TestValidateAnalyzeModelOutput_EmptyFieldName(t *testing.T) {
	output := &AnalyzeModelOutput{
		ServiceName: "ec2",
		References: []ModelReferenceInfo{
			{FieldName: "", SignalType: "id_suffix", Confidence: 0.8},
		},
	}

	err := ValidateAnalyzeModelOutput(output)
	if err == nil {
		t.Error("expected error for empty field_name")
	}
}

func TestValidateAnalyzeModelOutput_InvalidSignalType(t *testing.T) {
	output := &AnalyzeModelOutput{
		ServiceName: "ec2",
		References: []ModelReferenceInfo{
			{FieldName: "SubnetId", SignalType: "invalid_type", Confidence: 0.8},
		},
	}

	err := ValidateAnalyzeModelOutput(output)
	if err == nil {
		t.Error("expected error for invalid signal_type")
	}
	if !strings.Contains(err.Error(), "signal_type") {
		t.Errorf("error should mention signal_type, got: %v", err)
	}
}

func TestValidateAnalyzeModelOutput_InvalidConfidence(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := &AnalyzeModelOutput{
				ServiceName: "ec2",
				References: []ModelReferenceInfo{
					{FieldName: "SubnetId", SignalType: "id_suffix", Confidence: tc.confidence},
				},
			}

			err := ValidateAnalyzeModelOutput(output)
			if err == nil {
				t.Error("expected error for invalid confidence")
			}
		})
	}
}

func TestBuildAnalyzeModelInputParams(t *testing.T) {
	file := framework_FileToAnalyze{
		Key:      "elasticache",
		FilePath: "codegen/sdk-codegen/aws-models/elasticache.json",
		Content:  strings.Repeat("x", 1000),
	}

	params := buildAnalyzeModelInputParams(file)

	if params["file_path"] != file.FilePath {
		t.Errorf("expected file_path %q, got %v", file.FilePath, params["file_path"])
	}
	if params["content_length"] != 1000 {
		t.Errorf("expected content_length 1000, got %v", params["content_length"])
	}

	// Verify JSON-serializable (for cache hashing)
	_, err := json.Marshal(params)
	if err != nil {
		t.Errorf("params not JSON-serializable: %v", err)
	}
}

// Helper types and functions for tests

// framework_FileToAnalyze is a local alias to avoid import cycle in test helpers.
type framework_FileToAnalyze = struct {
	Key      string
	FilePath string
	Content  string
}

var ctx = context.Background()

func createDirAll(path string) error {
	return os.MkdirAll(path, 0755)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}
