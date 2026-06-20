package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// mockMatchModelsClient implements agent.BedrockClient for match_models tests.
type mockMatchModelsClient struct {
	responseFunc func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error)
	callIdx      atomic.Int32
}

func (m *mockMatchModelsClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	return m.responseFunc(idx, params)
}

func makeMatchModelsTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func TestMatchResourceModel_WithMatches(t *testing.T) {
	expectedOutput := MatchModelOutput{
		Matches: []ModelFieldMatch{
			{
				ModelFieldName: "SubnetGroupName",
				ACKFieldName:   "SubnetGroupName",
				ACKFieldPath:   "Spec.SubnetGroupName",
				TargetService:  "elasticache",
				TargetResource: "SubnetGroup",
				SignalType:     "name_suffix",
				Confidence:     0.85,
			},
			{
				ModelFieldName: "KmsKeyId",
				ACKFieldName:   "KMSKeyID",
				ACKFieldPath:   "Spec.KMSKeyID",
				TargetService:  "kms",
				TargetResource: "Key",
				SignalType:     "id_suffix",
				Confidence:     0.9,
			},
		},
		Unmatched: []string{"UnknownField"},
	}

	responseJSON, _ := json.Marshal(expectedOutput)

	client := &mockMatchModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeMatchModelsTextResponse(string(responseJSON)), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "SubnetGroupName", Path: "Spec.SubnetGroupName"},
			{Name: "KMSKeyID", Path: "Spec.KMSKeyID"},
			{Name: "Description", Path: "Spec.Description"},
		},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "SubnetGroupName", TargetService: "elasticache", TargetResource: "SubnetGroup", SignalType: "name_suffix", Confidence: 0.6},
		{FieldName: "KmsKeyId", TargetService: "kms", TargetResource: "Key", SignalType: "id_suffix", Confidence: 0.8},
		{FieldName: "UnknownField", TargetService: "unknown", SignalType: "doc_mention", Confidence: 0.5},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}

	result, err := MatchResourceModel(
		context.Background(), ag, resource, modelRefs, "elasticache",
		nil, validator,
	)
	if err != nil {
		t.Fatalf("MatchResourceModel failed: %v", err)
	}

	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result.Matches))
	}
	if result.Matches[0].ModelFieldName != "SubnetGroupName" {
		t.Errorf("expected model_field_name 'SubnetGroupName', got %q", result.Matches[0].ModelFieldName)
	}
	if result.Matches[0].ACKFieldName != "SubnetGroupName" {
		t.Errorf("expected ack_field_name 'SubnetGroupName', got %q", result.Matches[0].ACKFieldName)
	}
	if result.Matches[1].ACKFieldName != "KMSKeyID" {
		t.Errorf("expected ack_field_name 'KMSKeyID', got %q", result.Matches[1].ACKFieldName)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
	if result.Unmatched[0] != "UnknownField" {
		t.Errorf("expected unmatched 'UnknownField', got %q", result.Unmatched[0])
	}
}

func TestMatchResourceModel_NoMatches(t *testing.T) {
	expectedOutput := MatchModelOutput{
		Matches:   []ModelFieldMatch{},
		Unmatched: []string{"SomeField"},
	}

	responseJSON, _ := json.Marshal(expectedOutput)

	client := &mockMatchModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeMatchModelsTextResponse(string(responseJSON)), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	resource := types.ResourceInfo{
		Kind: "Bucket",
		StringFields: []types.FieldInfo{
			{Name: "BucketName", Path: "Spec.BucketName"},
		},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "SomeField", TargetService: "ec2", SignalType: "doc_mention", Confidence: 0.5},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}

	result, err := MatchResourceModel(
		context.Background(), ag, resource, modelRefs, "s3",
		nil, validator,
	)
	if err != nil {
		t.Fatalf("MatchResourceModel failed: %v", err)
	}

	if len(result.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result.Matches))
	}
	if len(result.Unmatched) != 1 {
		t.Errorf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
}

func TestMatchAllResourcesModel_MultipleControllers(t *testing.T) {
	responses := map[string]string{
		"CacheCluster": `{"matches":[{"model_field_name":"KmsKeyId","ack_field_name":"KMSKeyID","ack_field_path":"Spec.KMSKeyID","target_service":"kms","target_resource":"Key","signal_type":"id_suffix","confidence":0.9}],"unmatched_model_fields":[]}`,
		"Instance":     `{"matches":[{"model_field_name":"SubnetId","ack_field_name":"SubnetID","ack_field_path":"Spec.SubnetID","target_service":"ec2","target_resource":"Subnet","signal_type":"id_suffix","confidence":0.9}],"unmatched_model_fields":[]}`,
	}

	client := &mockMatchModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			for _, msg := range input.Messages {
				for _, block := range msg.Content {
					if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
						for kind, resp := range responses {
							if strings.Contains(textBlock.Value, kind) {
								return makeMatchModelsTextResponse(resp), nil
							}
						}
					}
				}
			}
			return makeMatchModelsTextResponse(`{"matches":[],"unmatched_model_fields":[]}`), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	controllers := []types.ControllerInfo{
		{
			ServiceName: "elasticache",
			RepoName:    "elasticache-controller",
			Resources: []types.ResourceInfo{
				{Kind: "CacheCluster", StringFields: []types.FieldInfo{
					{Name: "KMSKeyID", Path: "Spec.KMSKeyID"},
					{Name: "Description", Path: "Spec.Description"},
				}},
			},
		},
		{
			ServiceName: "ec2",
			RepoName:    "ec2-controller",
			Resources: []types.ResourceInfo{
				{Kind: "Instance", StringFields: []types.FieldInfo{
					{Name: "SubnetID", Path: "Spec.SubnetID"},
					{Name: "InstanceType", Path: "Spec.InstanceType"},
				}},
			},
		},
	}

	analysisResults := map[string]*AnalyzeModelOutput{
		"elasticache": {
			ServiceName: "elasticache",
			References: []ModelReferenceInfo{
				{FieldName: "KmsKeyId", TargetService: "kms", TargetResource: "Key", SignalType: "id_suffix", Confidence: 0.8},
			},
		},
		"ec2": {
			ServiceName: "ec2",
			References: []ModelReferenceInfo{
				{FieldName: "SubnetId", TargetService: "ec2", TargetResource: "Subnet", SignalType: "id_suffix", Confidence: 0.8},
			},
		},
	}

	mappings := []ModelMapping{
		{ServiceName: "elasticache", ModelFile: "codegen/sdk-codegen/aws-models/elasticache.json", Confidence: 1.0},
		{ServiceName: "ec2", ModelFile: "codegen/sdk-codegen/aws-models/ec2.json", Confidence: 1.0},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}

	result, err := MatchAllResourcesModel(
		context.Background(), ag, controllers, analysisResults, mappings,
		nil, validator, 1,
	)
	if err != nil {
		t.Fatalf("MatchAllResourcesModel failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	ecResult, ok := result.Results["elasticache_CacheCluster"]
	if !ok {
		t.Fatal("missing result for elasticache_CacheCluster")
	}
	if len(ecResult.Matches) != 1 {
		t.Errorf("expected 1 match for elasticache, got %d", len(ecResult.Matches))
	}

	ec2Result, ok := result.Results["ec2_Instance"]
	if !ok {
		t.Fatal("missing result for ec2_Instance")
	}
	if len(ec2Result.Matches) != 1 {
		t.Errorf("expected 1 match for ec2, got %d", len(ec2Result.Matches))
	}

	if len(result.Skipped) != 0 {
		t.Errorf("expected no skipped resources, got %v", result.Skipped)
	}
}

func TestMatchAllResourcesModel_SkipsEmptyAnalysis(t *testing.T) {
	client := &mockMatchModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			t.Fatal("agent should not be called for controller with no analysis results")
			return nil, nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	controllers := []types.ControllerInfo{
		{
			ServiceName: "s3",
			RepoName:    "s3-controller",
			Resources: []types.ResourceInfo{
				{Kind: "Bucket", StringFields: []types.FieldInfo{
					{Name: "BucketName", Path: "Spec.BucketName"},
				}},
			},
		},
	}

	// Empty analysis results
	analysisResults := map[string]*AnalyzeModelOutput{}

	mappings := []ModelMapping{
		{ServiceName: "s3", ModelFile: "codegen/sdk-codegen/aws-models/s3.json", Confidence: 1.0},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_model_fields"}}

	result, err := MatchAllResourcesModel(
		context.Background(), ag, controllers, analysisResults, mappings,
		nil, validator, 1,
	)
	if err != nil {
		t.Fatalf("MatchAllResourcesModel failed: %v", err)
	}

	// Should have no results since no analysis data was available
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(result.Results))
	}
}

func TestMatchModels_FilterFieldsForReferenceMatching(t *testing.T) {
	fields := []types.FieldInfo{
		{Name: "PolicyDocument", Path: "Spec.PolicyDocument", IsDocument: true},
		{Name: "AssumeRolePolicy", Path: "Spec.AssumeRolePolicy", IsIAMPolicy: true},
		{Name: "SubnetID", Path: "Spec.SubnetID", HasReference: true},
		{Name: "KMSKeyID", Path: "Spec.KMSKeyID"},
		{Name: "Description", Path: "Spec.Description"},
	}

	filtered := filterFieldsForReferenceMatching(fields)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 fields after filtering, got %d", len(filtered))
	}
	if filtered[0].Name != "KMSKeyID" {
		t.Errorf("expected first filtered field to be 'KMSKeyID', got %q", filtered[0].Name)
	}
	if filtered[1].Name != "Description" {
		t.Errorf("expected second filtered field to be 'Description', got %q", filtered[1].Name)
	}
}

func TestMatchModels_FilterFieldsForReferenceMatching_AllExcluded(t *testing.T) {
	fields := []types.FieldInfo{
		{Name: "PolicyDocument", Path: "Spec.PolicyDocument", IsDocument: true},
		{Name: "RoleRef", Path: "Spec.RoleRef", HasReference: true},
	}

	filtered := filterFieldsForReferenceMatching(fields)

	if len(filtered) != 0 {
		t.Errorf("expected 0 fields after filtering, got %d", len(filtered))
	}
}

func TestMatchModels_FilterFieldsForReferenceMatching_NoneExcluded(t *testing.T) {
	fields := []types.FieldInfo{
		{Name: "SubnetID", Path: "Spec.SubnetID"},
		{Name: "KMSKeyID", Path: "Spec.KMSKeyID"},
	}

	filtered := filterFieldsForReferenceMatching(fields)

	if len(filtered) != 2 {
		t.Errorf("expected 2 fields after filtering, got %d", len(filtered))
	}
}

func TestBuildMatchModelPrompt_ContainsPascalCaseNote(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "SubnetGroupName", Path: "Spec.SubnetGroupName"},
		},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "SubnetGroupName", SignalType: "name_suffix", Confidence: 0.6},
	}

	prompt := buildMatchModelPrompt(resource, modelRefs, "elasticache")

	if !strings.Contains(prompt, "PascalCase") {
		t.Error("prompt missing PascalCase correspondence note")
	}
	if !strings.Contains(prompt, "directly correspond") {
		t.Error("prompt missing direct correspondence explanation")
	}
	if !strings.Contains(prompt, "ServiceLinkedRoleARN") {
		t.Error("prompt missing PascalCase example")
	}
}

func TestBuildMatchModelPrompt_ContainsResourceInfo(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "AutoScalingGroup",
		StringFields: []types.FieldInfo{
			{Name: "ServiceLinkedRoleARN", Path: "Spec.ServiceLinkedRoleARN"},
			{Name: "VPCZoneIdentifier", Path: "Spec.VPCZoneIdentifier"},
		},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "ServiceLinkedRoleARN", TargetService: "iam", TargetResource: "Role", SignalType: "arn_trait", Confidence: 1.0},
	}

	prompt := buildMatchModelPrompt(resource, modelRefs, "autoscaling")

	if !strings.Contains(prompt, "autoscaling") {
		t.Error("prompt missing service name")
	}
	if !strings.Contains(prompt, "AutoScalingGroup") {
		t.Error("prompt missing resource kind")
	}
	if !strings.Contains(prompt, "ServiceLinkedRoleARN") {
		t.Error("prompt missing ACK field name")
	}
	if !strings.Contains(prompt, "arn_trait") {
		t.Error("prompt missing signal type")
	}
	if !strings.Contains(prompt, "iam") {
		t.Error("prompt missing target service")
	}
}

func TestBuildMatchModelPrompt_ContainsOutputSchema(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "Bucket",
		StringFields: []types.FieldInfo{
			{Name: "Name", Path: "Spec.Name"},
		},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "BucketName", SignalType: "name_suffix", Confidence: 0.6},
	}

	prompt := buildMatchModelPrompt(resource, modelRefs, "s3")

	if !strings.Contains(prompt, "model_field_name") {
		t.Error("prompt missing model_field_name in output schema")
	}
	if !strings.Contains(prompt, "ack_field_name") {
		t.Error("prompt missing ack_field_name in output schema")
	}
	if !strings.Contains(prompt, "signal_type") {
		t.Error("prompt missing signal_type in output schema")
	}
	if !strings.Contains(prompt, "unmatched_model_fields") {
		t.Error("prompt missing unmatched_model_fields in output schema")
	}
}

func TestParseMatchModelResult_ValidJSON(t *testing.T) {
	input := `{"matches":[{"model_field_name":"KmsKeyId","ack_field_name":"KMSKeyID","ack_field_path":"Spec.KMSKeyID","target_service":"kms","target_resource":"Key","signal_type":"id_suffix","confidence":0.9}],"unmatched_model_fields":["UnknownField"]}`

	result, err := parseMatchModelResult(input)
	if err != nil {
		t.Fatalf("parseMatchModelResult failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if result.Matches[0].ModelFieldName != "KmsKeyId" {
		t.Errorf("expected model_field_name 'KmsKeyId', got %q", result.Matches[0].ModelFieldName)
	}
	if result.Matches[0].ACKFieldName != "KMSKeyID" {
		t.Errorf("expected ack_field_name 'KMSKeyID', got %q", result.Matches[0].ACKFieldName)
	}
	if result.Matches[0].SignalType != "id_suffix" {
		t.Errorf("expected signal_type 'id_suffix', got %q", result.Matches[0].SignalType)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
}

func TestParseMatchModelResult_EmptyResult(t *testing.T) {
	input := `{"matches":[],"unmatched_model_fields":[]}`

	result, err := parseMatchModelResult(input)
	if err != nil {
		t.Fatalf("parseMatchModelResult failed: %v", err)
	}

	if len(result.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result.Matches))
	}
	if len(result.Unmatched) != 0 {
		t.Errorf("expected 0 unmatched, got %d", len(result.Unmatched))
	}
}

func TestParseMatchModelResult_InvalidJSON(t *testing.T) {
	_, err := parseMatchModelResult("not valid json")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestValidateMatchModelOutput_Valid(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "id_suffix", Confidence: 0.8},
			{ModelFieldName: "RoleArn", ACKFieldName: "RoleARN", ACKFieldPath: "Spec.RoleARN", SignalType: "arn_suffix", Confidence: 0.85},
			{ModelFieldName: "KeyId", ACKFieldName: "KeyID", ACKFieldPath: "Spec.KeyID", SignalType: "arn_trait", Confidence: 1.0},
		},
		Unmatched: []string{"UnknownField"},
	}

	if err := ValidateMatchModelOutput(output, nil); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateMatchModelOutput_NilOutput(t *testing.T) {
	err := ValidateMatchModelOutput(nil, nil)
	if err == nil {
		t.Error("expected error for nil output")
	}
}

func TestValidateMatchModelOutput_EmptyModelFieldName(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "id_suffix", Confidence: 0.8},
		},
	}

	err := ValidateMatchModelOutput(output, nil)
	if err == nil {
		t.Error("expected error for empty model_field_name")
	}
	if !strings.Contains(err.Error(), "model_field_name") {
		t.Errorf("error should mention model_field_name, got: %v", err)
	}
}

func TestValidateMatchModelOutput_EmptyACKFieldName(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "", ACKFieldPath: "Spec.SubnetID", SignalType: "id_suffix", Confidence: 0.8},
		},
	}

	err := ValidateMatchModelOutput(output, nil)
	if err == nil {
		t.Error("expected error for empty ack_field_name")
	}
}

func TestValidateMatchModelOutput_EmptyACKFieldPath(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "", SignalType: "id_suffix", Confidence: 0.8},
		},
	}

	err := ValidateMatchModelOutput(output, nil)
	if err == nil {
		t.Error("expected error for empty ack_field_path")
	}
}

func TestValidateMatchModelOutput_EmptySignalType(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "", Confidence: 0.8},
		},
	}

	err := ValidateMatchModelOutput(output, nil)
	if err == nil {
		t.Error("expected error for empty signal_type")
	}
}

func TestValidateMatchModelOutput_InvalidSignalType(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "invalid_type", Confidence: 0.8},
		},
	}

	err := ValidateMatchModelOutput(output, nil)
	if err == nil {
		t.Error("expected error for invalid signal_type")
	}
	if !strings.Contains(err.Error(), "signal_type") {
		t.Errorf("error should mention signal_type, got: %v", err)
	}
}

func TestValidateMatchModelOutput_InvalidConfidence(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := &MatchModelOutput{
				Matches: []ModelFieldMatch{
					{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "id_suffix", Confidence: tc.confidence},
				},
			}

			err := ValidateMatchModelOutput(output, nil)
			if err == nil {
				t.Error("expected error for invalid confidence")
			}
		})
	}
}

func TestValidateMatchModelOutput_InvalidACKField(t *testing.T) {
	validFields := map[string]bool{
		"SubnetID": true,
		"KMSKeyID": true,
	}

	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "NonExistentField", ACKFieldPath: "Spec.NonExistentField", SignalType: "id_suffix", Confidence: 0.8},
		},
	}

	err := ValidateMatchModelOutput(output, validFields)
	if err == nil {
		t.Error("expected error for invalid ACK field")
	}
	if !strings.Contains(err.Error(), "NonExistentField") {
		t.Errorf("error should mention the invalid field, got: %v", err)
	}
}

func TestValidateMatchModelCompleteness_AllAccountedFor(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "id_suffix", Confidence: 0.8},
		},
		Unmatched: []string{"UnknownField"},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "SubnetId"},
		{FieldName: "UnknownField"},
	}

	if err := ValidateMatchModelCompleteness(output, modelRefs); err != nil {
		t.Errorf("expected valid completeness, got error: %v", err)
	}
}

func TestValidateMatchModelCompleteness_MissingField(t *testing.T) {
	output := &MatchModelOutput{
		Matches: []ModelFieldMatch{
			{ModelFieldName: "SubnetId", ACKFieldName: "SubnetID", ACKFieldPath: "Spec.SubnetID", SignalType: "id_suffix", Confidence: 0.8},
		},
		Unmatched: []string{},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "SubnetId"},
		{FieldName: "DroppedField"},
	}

	err := ValidateMatchModelCompleteness(output, modelRefs)
	if err == nil {
		t.Error("expected error for missing field")
	}
	if !strings.Contains(err.Error(), "DroppedField") {
		t.Errorf("error should mention the missing field, got: %v", err)
	}
}

func TestValidateMatchModelCompleteness_NilOutput(t *testing.T) {
	err := ValidateMatchModelCompleteness(nil, nil)
	if err == nil {
		t.Error("expected error for nil output")
	}
}

func TestBuildMatchModelInputParams(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "SubnetGroupName", Path: "Spec.SubnetGroupName"},
			{Name: "KMSKeyID", Path: "Spec.KMSKeyID"},
		},
	}

	modelRefs := []ModelReferenceInfo{
		{FieldName: "SubnetGroupName", SignalType: "name_suffix"},
		{FieldName: "KmsKeyId", SignalType: "id_suffix"},
	}

	params := buildMatchModelInputParams(resource, modelRefs, "elasticache")

	if params["service_name"] != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %v", params["service_name"])
	}
	if params["resource_kind"] != "CacheCluster" {
		t.Errorf("expected resource_kind 'CacheCluster', got %v", params["resource_kind"])
	}
	if params["ack_field_count"] != 2 {
		t.Errorf("expected ack_field_count 2, got %v", params["ack_field_count"])
	}
	if params["model_field_count"] != 2 {
		t.Errorf("expected model_field_count 2, got %v", params["model_field_count"])
	}

	// Verify JSON-serializable (for cache hashing)
	_, err := json.Marshal(params)
	if err != nil {
		t.Errorf("params not JSON-serializable: %v", err)
	}
}
