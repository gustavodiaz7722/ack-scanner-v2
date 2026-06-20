package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// --- Mock Bedrock Client for match_upjet tests ---

type mockMatchUpjetBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockMatchUpjetBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeMatchUpjetFinalTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func newMatchUpjetMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockMatchUpjetBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Unit Tests ---

func TestMatchResourceUpjet_Success(t *testing.T) {
	expectedOutput := MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{
				UpjetFieldName: "parameter_group_name",
				ACKFieldName:   "CacheParameterGroupName",
				ACKFieldPath:   "Spec.CacheParameterGroupName",
				TargetResource: "aws_elasticache_parameter_group",
				IsAmbiguous:    false,
				Confidence:     0.9,
			},
			{
				UpjetFieldName: "subnet_group_name",
				ACKFieldName:   "CacheSubnetGroupName",
				ACKFieldPath:   "Spec.CacheSubnetGroupName",
				TargetResource: "aws_elasticache_subnet_group",
				IsAmbiguous:    false,
				Confidence:     0.9,
			},
		},
		Unmatched: []string{"notification_topic_arn"},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchUpjetMockAgent(t, makeMatchUpjetFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "CacheParameterGroupName", Path: "Spec.CacheParameterGroupName", JSONTag: "cacheParameterGroupName"},
			{Name: "CacheSubnetGroupName", Path: "Spec.CacheSubnetGroupName", JSONTag: "cacheSubnetGroupName"},
			{Name: "Engine", Path: "Spec.Engine", JSONTag: "engine"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "parameter_group_name", TargetResource: "aws_elasticache_parameter_group", Confidence: 1.0},
		{FieldName: "subnet_group_name", TargetResource: "aws_elasticache_subnet_group", Confidence: 1.0},
		{FieldName: "notification_topic_arn", TargetResource: "aws_sns_topic", Confidence: 1.0},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchResourceUpjet(
		context.Background(),
		ag,
		resource,
		upjetRefs,
		"elasticache",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceUpjet failed: %v", err)
	}

	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result.Matches))
	}
	if result.Matches[0].UpjetFieldName != "parameter_group_name" {
		t.Errorf("expected upjet_field_name 'parameter_group_name', got %q", result.Matches[0].UpjetFieldName)
	}
	if result.Matches[0].ACKFieldName != "CacheParameterGroupName" {
		t.Errorf("expected ack_field_name 'CacheParameterGroupName', got %q", result.Matches[0].ACKFieldName)
	}
	if result.Matches[0].TargetResource != "aws_elasticache_parameter_group" {
		t.Errorf("expected target_resource 'aws_elasticache_parameter_group', got %q", result.Matches[0].TargetResource)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
	if result.Unmatched[0] != "notification_topic_arn" {
		t.Errorf("expected unmatched 'notification_topic_arn', got %q", result.Unmatched[0])
	}
}

func TestMatchResourceUpjet_AllUnmatched(t *testing.T) {
	expectedOutput := MatchUpjetOutput{
		Matches:   []UpjetFieldMatch{},
		Unmatched: []string{"some_field", "another_field"},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchUpjetMockAgent(t, makeMatchUpjetFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "Bucket",
		StringFields: []types.FieldInfo{
			{Name: "BucketName", Path: "Spec.BucketName", JSONTag: "bucketName"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "some_field", TargetResource: "aws_something", Confidence: 1.0},
		{FieldName: "another_field", TargetResource: "aws_another", Confidence: 1.0},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchResourceUpjet(
		context.Background(),
		ag,
		resource,
		upjetRefs,
		"s3",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceUpjet failed: %v", err)
	}

	if len(result.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result.Matches))
	}
	if len(result.Unmatched) != 2 {
		t.Errorf("expected 2 unmatched, got %d", len(result.Unmatched))
	}
}

func TestMatchResourceUpjet_AmbiguousMatch(t *testing.T) {
	expectedOutput := MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{
				UpjetFieldName: "security_group_ids",
				ACKFieldName:   "SecurityGroupIDs",
				ACKFieldPath:   "Spec.SecurityGroupIDs",
				TargetResource: "",
				IsAmbiguous:    true,
				Confidence:     0.85,
			},
		},
		Unmatched: []string{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchUpjetMockAgent(t, makeMatchUpjetFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "ReplicationGroup",
		StringFields: []types.FieldInfo{
			{Name: "SecurityGroupIDs", Path: "Spec.SecurityGroupIDs", JSONTag: "securityGroupIDs"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "security_group_ids", TargetResource: "", IsAmbiguous: true, Confidence: 0.8},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchResourceUpjet(
		context.Background(),
		ag,
		resource,
		upjetRefs,
		"elasticache",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceUpjet failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if !result.Matches[0].IsAmbiguous {
		t.Error("expected match to be marked as ambiguous")
	}
	if result.Matches[0].TargetResource != "" {
		t.Errorf("expected empty target_resource for ambiguous match, got %q", result.Matches[0].TargetResource)
	}
}

func TestMatchResourceUpjet_WithAlternatives(t *testing.T) {
	expectedOutput := MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{
				UpjetFieldName: "kms_key_id",
				ACKFieldName:   "KMSKeyID",
				ACKFieldPath:   "Spec.KMSKeyID",
				TargetResource: "aws_kms_key",
				IsAmbiguous:    false,
				Confidence:     0.8,
				Alternatives:   []string{"KmsKeyId"},
			},
		},
		Unmatched: []string{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchUpjetMockAgent(t, makeMatchUpjetFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "KMSKeyID", Path: "Spec.KMSKeyID", JSONTag: "kmsKeyID"},
			{Name: "KmsKeyId", Path: "Spec.KmsKeyId", JSONTag: "kmsKeyId"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "kms_key_id", TargetResource: "aws_kms_key", Confidence: 1.0},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchResourceUpjet(
		context.Background(),
		ag,
		resource,
		upjetRefs,
		"elasticache",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceUpjet failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if len(result.Matches[0].Alternatives) != 1 {
		t.Fatalf("expected 1 alternative, got %d", len(result.Matches[0].Alternatives))
	}
	if result.Matches[0].Alternatives[0] != "KmsKeyId" {
		t.Errorf("expected alternative 'KmsKeyId', got %q", result.Matches[0].Alternatives[0])
	}
}

func TestMatchResourceUpjet_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	expectedOutput := MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{
				UpjetFieldName: "role_arn",
				ACKFieldName:   "RoleARN",
				ACKFieldPath:   "Spec.RoleARN",
				TargetResource: "aws_iam_role",
				Confidence:     0.95,
			},
		},
		Unmatched: []string{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchUpjetMockAgent(t, makeMatchUpjetFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "Function",
		StringFields: []types.FieldInfo{
			{Name: "RoleARN", Path: "Spec.RoleARN", JSONTag: "roleARN"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "role_arn", TargetResource: "aws_iam_role", Confidence: 1.0},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	// First call — should hit agent
	result, err := MatchResourceUpjet(
		context.Background(),
		ag,
		resource,
		upjetRefs,
		"lambda",
		resultCache,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}

	// Second call — should use cache (no agent responses available)
	ag2 := newMatchUpjetMockAgent(t) // no responses
	result2, err := MatchResourceUpjet(
		context.Background(),
		ag2,
		resource,
		upjetRefs,
		"lambda",
		resultCache,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("second call failed (should have used cache): %v", err)
	}
	if len(result2.Matches) != 1 {
		t.Fatalf("cached result has wrong number of matches: %d", len(result2.Matches))
	}
	if result2.Matches[0].ACKFieldName != "RoleARN" {
		t.Errorf("cached match has wrong ack_field_name: %q", result2.Matches[0].ACKFieldName)
	}
}

func TestMatchAllResourcesUpjet_Success(t *testing.T) {
	// Use identical responses for both resources to avoid goroutine ordering issues
	output := MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "some_ref", ACKFieldName: "SomeRef", ACKFieldPath: "Spec.SomeRef", TargetResource: "aws_some_resource", Confidence: 0.9},
		},
		Unmatched: []string{"other_ref"},
	}

	resp, _ := json.Marshal(output)

	ag := newMatchUpjetMockAgent(t,
		makeMatchUpjetFinalTextResponse(string(resp)),
		makeMatchUpjetFinalTextResponse(string(resp)),
	)

	controllers := []types.ControllerInfo{
		{
			ServiceName: "lambda",
			Resources: []types.ResourceInfo{
				{Kind: "Function", StringFields: []types.FieldInfo{
					{Name: "RoleARN", Path: "Spec.RoleARN", JSONTag: "roleARN"},
				}},
			},
		},
		{
			ServiceName: "s3",
			Resources: []types.ResourceInfo{
				{Kind: "Bucket", StringFields: []types.FieldInfo{
					{Name: "BucketName", Path: "Spec.BucketName", JSONTag: "bucketName"},
				}},
			},
		},
	}

	analysisResults := map[string]*AnalyzeUpjetOutput{
		"lambda": {
			ServiceName: "lambda",
			References:  []UpjetReferenceInfo{{FieldName: "some_ref", TargetResource: "aws_some_resource", Confidence: 1.0}, {FieldName: "other_ref", TargetResource: "aws_other", Confidence: 1.0}},
		},
		"s3": {
			ServiceName: "s3",
			References:  []UpjetReferenceInfo{{FieldName: "some_ref", TargetResource: "aws_some_resource", Confidence: 1.0}, {FieldName: "other_ref", TargetResource: "aws_other", Confidence: 1.0}},
		},
	}

	mappings := []UpjetMapping{
		{ServiceName: "lambda", UpjetConfigs: []UpjetMappingEntry{{UpjetService: "lambda", FilePath: "config/lambda/config.go", Confidence: 0.95}}},
		{ServiceName: "s3", UpjetConfigs: []UpjetMappingEntry{{UpjetService: "s3", FilePath: "config/s3/config.go", Confidence: 0.95}}},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchAllResourcesUpjet(context.Background(), ag, controllers, analysisResults, mappings, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAllResourcesUpjet failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// Verify both results exist and have expected structure
	lambdaResult, ok := result.Results["lambda_Function"]
	if !ok {
		t.Fatal("expected result for 'lambda_Function'")
	}
	if len(lambdaResult.Matches) != 1 || len(lambdaResult.Unmatched) != 1 {
		t.Errorf("lambda_Function: expected 1 match + 1 unmatched, got %d matches + %d unmatched",
			len(lambdaResult.Matches), len(lambdaResult.Unmatched))
	}

	s3Result, ok := result.Results["s3_Bucket"]
	if !ok {
		t.Fatal("expected result for 's3_Bucket'")
	}
	if len(s3Result.Matches) != 1 || len(s3Result.Unmatched) != 1 {
		t.Errorf("s3_Bucket: expected 1 match + 1 unmatched, got %d matches + %d unmatched",
			len(s3Result.Matches), len(s3Result.Unmatched))
	}
}

func TestMatchAllResourcesUpjet_NoSourceData(t *testing.T) {
	ag := newMatchUpjetMockAgent(t) // no responses needed

	controllers := []types.ControllerInfo{
		{
			ServiceName: "lambda",
			Resources: []types.ResourceInfo{
				{Kind: "Function", StringFields: []types.FieldInfo{
					{Name: "RoleARN", Path: "Spec.RoleARN", JSONTag: "roleARN"},
				}},
			},
		},
	}

	// No analysis results for lambda
	analysisResults := map[string]*AnalyzeUpjetOutput{}
	mappings := []UpjetMapping{
		{ServiceName: "lambda", UpjetConfigs: []UpjetMappingEntry{{UpjetService: "lambda", FilePath: "config/lambda/config.go", Confidence: 0.95}}},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchAllResourcesUpjet(context.Background(), ag, controllers, analysisResults, mappings, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAllResourcesUpjet failed: %v", err)
	}

	// No source data means the controller is skipped
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results when no source data, got %d", len(result.Results))
	}
}

func TestMatchAllResourcesUpjet_EmptyControllers(t *testing.T) {
	ag := newMatchUpjetMockAgent(t)

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	result, err := MatchAllResourcesUpjet(context.Background(), ag, nil, nil, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAllResourcesUpjet failed: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty controllers, got %d", len(result.Results))
	}
}

func TestMatchAllResourcesUpjet_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ag := newMatchUpjetMockAgent(t, makeMatchUpjetFinalTextResponse(`{}`))

	controllers := []types.ControllerInfo{
		{
			ServiceName: "lambda",
			Resources: []types.ResourceInfo{
				{Kind: "Function", StringFields: []types.FieldInfo{
					{Name: "RoleARN", Path: "Spec.RoleARN", JSONTag: "roleARN"},
				}},
			},
		},
	}

	analysisResults := map[string]*AnalyzeUpjetOutput{
		"lambda": {ServiceName: "lambda", References: []UpjetReferenceInfo{{FieldName: "role_arn", TargetResource: "aws_iam_role", Confidence: 1.0}}},
	}

	mappings := []UpjetMapping{
		{ServiceName: "lambda", UpjetConfigs: []UpjetMappingEntry{{UpjetService: "lambda", FilePath: "config/lambda/config.go", Confidence: 0.95}}},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_upjet_fields"}}

	_, err := MatchAllResourcesUpjet(ctx, ag, controllers, analysisResults, mappings, nil, validator, 1, logger.Nop())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- Field Filter Tests ---
// Note: filterFieldsForReferenceMatching is tested in match_terraform_refs_test.go
// and match_models_test.go. Here we just test the config uses it correctly.

func TestMatchUpjetConfig_UsesFilterFields(t *testing.T) {
	config := MatchUpjetConfig()
	if config.FilterFields == nil {
		t.Fatal("expected FilterFields to be set in MatchUpjet config")
	}

	// Verify it correctly filters annotated fields
	fields := []types.FieldInfo{
		{Name: "RoleARN", Path: "Spec.RoleARN"},
		{Name: "PolicyDocument", Path: "Spec.PolicyDocument", IsDocument: true},
		{Name: "AssumeRolePolicy", Path: "Spec.AssumeRolePolicy", IsIAMPolicy: true},
		{Name: "SubnetID", Path: "Spec.SubnetID", HasReference: true},
		{Name: "VPCID", Path: "Spec.VPCID"},
	}

	filtered := config.FilterFields(fields)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 fields after filtering, got %d", len(filtered))
	}

	expected := map[string]bool{"RoleARN": true, "VPCID": true}
	for _, f := range filtered {
		if !expected[f.Name] {
			t.Errorf("unexpected field in filtered result: %s", f.Name)
		}
	}
}

// --- Prompt Tests ---

func TestBuildMatchUpjetPrompt_ContainsExpectedContent(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "CacheParameterGroupName", Path: "Spec.CacheParameterGroupName", JSONTag: "cacheParameterGroupName"},
			{Name: "Engine", Path: "Spec.Engine", JSONTag: "engine"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "parameter_group_name", TargetResource: "aws_elasticache_parameter_group", Confidence: 1.0},
		{FieldName: "kms_key_id", TargetResource: "aws_kms_key", IsAmbiguous: false, Confidence: 1.0},
	}

	prompt := buildMatchUpjetPrompt(resource, upjetRefs, "elasticache")

	expectedPhrases := []string{
		"elasticache",
		"CacheCluster",
		"CacheParameterGroupName",
		"parameter_group_name",
		"aws_elasticache_parameter_group",
		"kms_key_id",
		"aws_kms_key",
		"upjet_field_name",
		"ack_field_name",
		"ack_field_path",
		"target_resource",
		"confidence",
		"unmatched_upjet_fields",
		"is_ambiguous",
		"alternatives",
		"PascalCase",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstr(prompt, phrase) {
			t.Errorf("prompt missing expected phrase: %q", phrase)
		}
	}
}

func TestBuildMatchUpjetPrompt_AmbiguousFieldMarked(t *testing.T) {
	resource := types.ResourceInfo{
		Kind:         "Instance",
		StringFields: []types.FieldInfo{{Name: "SecurityGroupIDs", Path: "Spec.SecurityGroupIDs", JSONTag: "securityGroupIDs"}},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "security_group_ids", TargetResource: "", IsAmbiguous: true, Confidence: 0.8},
	}

	prompt := buildMatchUpjetPrompt(resource, upjetRefs, "ec2")

	if !containsSubstr(prompt, "AMBIGUOUS") {
		t.Error("prompt should mark ambiguous fields with [AMBIGUOUS]")
	}
}

// --- Input Params Tests ---

func TestBuildMatchUpjetInputParams(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "CacheCluster",
		StringFields: []types.FieldInfo{
			{Name: "FieldA", Path: "Spec.FieldA"},
			{Name: "FieldB", Path: "Spec.FieldB"},
		},
	}

	upjetRefs := []UpjetReferenceInfo{
		{FieldName: "ref_a", TargetResource: "aws_a"},
		{FieldName: "ref_b", TargetResource: "aws_b"},
		{FieldName: "ref_c", TargetResource: "aws_c"},
	}

	params := buildMatchUpjetInputParams(resource, upjetRefs, "elasticache")

	if params["service_name"] != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %v", params["service_name"])
	}
	if params["resource_kind"] != "CacheCluster" {
		t.Errorf("expected resource_kind 'CacheCluster', got %v", params["resource_kind"])
	}
	if params["ack_field_count"] != 2 {
		t.Errorf("expected ack_field_count 2, got %v", params["ack_field_count"])
	}
	if params["upjet_field_count"] != 3 {
		t.Errorf("expected upjet_field_count 3, got %v", params["upjet_field_count"])
	}

	ackFields, ok := params["ack_field_names"].([]string)
	if !ok {
		t.Fatal("ack_field_names is not []string")
	}
	if len(ackFields) != 2 || ackFields[0] != "FieldA" || ackFields[1] != "FieldB" {
		t.Errorf("unexpected ack_field_names: %v", ackFields)
	}

	upjetFields, ok := params["upjet_field_names"].([]string)
	if !ok {
		t.Fatal("upjet_field_names is not []string")
	}
	if len(upjetFields) != 3 || upjetFields[0] != "ref_a" {
		t.Errorf("unexpected upjet_field_names: %v", upjetFields)
	}
}

// --- Parse Tests ---

func TestParseMatchUpjetResult_ValidJSON(t *testing.T) {
	input := `{"matches":[{"upjet_field_name":"role_arn","ack_field_name":"RoleARN","ack_field_path":"Spec.RoleARN","target_resource":"aws_iam_role","is_ambiguous":false,"confidence":0.9}],"unmatched_upjet_fields":["other_field"]}`

	result, err := parseMatchUpjetResult(input)
	if err != nil {
		t.Fatalf("parseMatchUpjetResult failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if result.Matches[0].UpjetFieldName != "role_arn" {
		t.Errorf("expected upjet_field_name 'role_arn', got %q", result.Matches[0].UpjetFieldName)
	}
	if result.Matches[0].TargetResource != "aws_iam_role" {
		t.Errorf("expected target_resource 'aws_iam_role', got %q", result.Matches[0].TargetResource)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
}

func TestParseMatchUpjetResult_InvalidJSON(t *testing.T) {
	_, err := parseMatchUpjetResult("not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseMatchUpjetResult_EmptyMatches(t *testing.T) {
	input := `{"matches":[],"unmatched_upjet_fields":["field_a","field_b"]}`

	result, err := parseMatchUpjetResult(input)
	if err != nil {
		t.Fatalf("parseMatchUpjetResult failed: %v", err)
	}

	if len(result.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result.Matches))
	}
	if len(result.Unmatched) != 2 {
		t.Errorf("expected 2 unmatched, got %d", len(result.Unmatched))
	}
}

// --- Validation Tests ---

func TestValidateMatchUpjetOutput_ValidOutput(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "role_arn", ACKFieldName: "RoleARN", ACKFieldPath: "Spec.RoleARN", TargetResource: "aws_iam_role", Confidence: 0.9},
			{UpjetFieldName: "kms_key_id", ACKFieldName: "KMSKeyID", ACKFieldPath: "Spec.KMSKeyID", TargetResource: "aws_kms_key", Confidence: 0.85},
		},
		Unmatched: []string{"other_field"},
	}

	if err := ValidateMatchUpjetOutput(output, nil); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateMatchUpjetOutput_WithValidACKFields(t *testing.T) {
	validFields := map[string]bool{"RoleARN": true, "KMSKeyID": true}

	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "role_arn", ACKFieldName: "RoleARN", ACKFieldPath: "Spec.RoleARN", TargetResource: "aws_iam_role", Confidence: 0.9},
		},
		Unmatched: []string{},
	}

	if err := ValidateMatchUpjetOutput(output, validFields); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateMatchUpjetOutput_InvalidACKField(t *testing.T) {
	validFields := map[string]bool{"RoleARN": true}

	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "role_arn", ACKFieldName: "NonExistentField", ACKFieldPath: "Spec.NonExistentField", TargetResource: "aws_iam_role", Confidence: 0.9},
		},
		Unmatched: []string{},
	}

	if err := ValidateMatchUpjetOutput(output, validFields); err == nil {
		t.Error("expected error for invalid ACK field")
	}
}

func TestValidateMatchUpjetOutput_NilOutput(t *testing.T) {
	if err := ValidateMatchUpjetOutput(nil, nil); err == nil {
		t.Error("expected error for nil output")
	}
}

func TestValidateMatchUpjetOutput_EmptyUpjetFieldName(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "", ACKFieldName: "RoleARN", ACKFieldPath: "Spec.RoleARN", TargetResource: "aws_iam_role", Confidence: 0.9},
		},
	}

	if err := ValidateMatchUpjetOutput(output, nil); err == nil {
		t.Error("expected error for empty upjet_field_name")
	}
}

func TestValidateMatchUpjetOutput_EmptyACKFieldName(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "role_arn", ACKFieldName: "", ACKFieldPath: "Spec.RoleARN", TargetResource: "aws_iam_role", Confidence: 0.9},
		},
	}

	if err := ValidateMatchUpjetOutput(output, nil); err == nil {
		t.Error("expected error for empty ack_field_name")
	}
}

func TestValidateMatchUpjetOutput_EmptyACKFieldPath(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "role_arn", ACKFieldName: "RoleARN", ACKFieldPath: "", TargetResource: "aws_iam_role", Confidence: 0.9},
		},
	}

	if err := ValidateMatchUpjetOutput(output, nil); err == nil {
		t.Error("expected error for empty ack_field_path")
	}
}

func TestValidateMatchUpjetOutput_EmptyTargetResourceNonAmbiguous(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "role_arn", ACKFieldName: "RoleARN", ACKFieldPath: "Spec.RoleARN", TargetResource: "", IsAmbiguous: false, Confidence: 0.9},
		},
	}

	if err := ValidateMatchUpjetOutput(output, nil); err == nil {
		t.Error("expected error for empty target_resource on non-ambiguous match")
	}
}

func TestValidateMatchUpjetOutput_EmptyTargetResourceAmbiguous(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "security_groups", ACKFieldName: "SecurityGroups", ACKFieldPath: "Spec.SecurityGroups", TargetResource: "", IsAmbiguous: true, Confidence: 0.8},
		},
	}

	// Ambiguous matches are allowed to have empty target_resource
	if err := ValidateMatchUpjetOutput(output, nil); err != nil {
		t.Errorf("expected valid output for ambiguous match with empty target, got error: %v", err)
	}
}

func TestValidateMatchUpjetOutput_ConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"too high", 1.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &MatchUpjetOutput{
				Matches: []UpjetFieldMatch{
					{UpjetFieldName: "role_arn", ACKFieldName: "RoleARN", ACKFieldPath: "Spec.RoleARN", TargetResource: "aws_iam_role", Confidence: tt.confidence},
				},
			}

			if err := ValidateMatchUpjetOutput(output, nil); err == nil {
				t.Errorf("expected error for confidence %f", tt.confidence)
			}
		})
	}
}

// --- Completeness Validation Tests ---

func TestValidateMatchUpjetCompleteness_AllAccountedFor(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "field_a", ACKFieldName: "FieldA", ACKFieldPath: "Spec.FieldA", TargetResource: "aws_a", Confidence: 0.9},
		},
		Unmatched: []string{"field_b"},
	}

	refs := []UpjetReferenceInfo{
		{FieldName: "field_a"},
		{FieldName: "field_b"},
	}

	if err := ValidateMatchUpjetCompleteness(output, refs); err != nil {
		t.Errorf("expected valid completeness, got error: %v", err)
	}
}

func TestValidateMatchUpjetCompleteness_MissingField(t *testing.T) {
	output := &MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{UpjetFieldName: "field_a", ACKFieldName: "FieldA", ACKFieldPath: "Spec.FieldA", TargetResource: "aws_a", Confidence: 0.9},
		},
		Unmatched: []string{},
	}

	refs := []UpjetReferenceInfo{
		{FieldName: "field_a"},
		{FieldName: "field_b"}, // missing from output
	}

	if err := ValidateMatchUpjetCompleteness(output, refs); err == nil {
		t.Error("expected error for missing field in output")
	}
}

func TestValidateMatchUpjetCompleteness_NilOutput(t *testing.T) {
	refs := []UpjetReferenceInfo{{FieldName: "field_a"}}

	if err := ValidateMatchUpjetCompleteness(nil, refs); err == nil {
		t.Error("expected error for nil output")
	}
}

// --- Config Tests ---

func TestMatchUpjetConfig_ToolName(t *testing.T) {
	config := MatchUpjetConfig()
	if config.ToolName != "match_upjet" {
		t.Errorf("expected ToolName 'match_upjet', got %q", config.ToolName)
	}
}

func TestMatchUpjetConfig_ItemKey(t *testing.T) {
	config := MatchUpjetConfig()
	resource := types.ResourceInfo{Kind: "CacheCluster"}
	key := config.ItemKey("elasticache", resource)
	if key != "elasticache_CacheCluster" {
		t.Errorf("expected key 'elasticache_CacheCluster', got %q", key)
	}
}

// --- JSON Round Trip Test ---

func TestMatchUpjetOutput_JSONRoundTrip(t *testing.T) {
	original := MatchUpjetOutput{
		Matches: []UpjetFieldMatch{
			{
				UpjetFieldName: "parameter_group_name",
				ACKFieldName:   "CacheParameterGroupName",
				ACKFieldPath:   "Spec.CacheParameterGroupName",
				TargetResource: "aws_elasticache_parameter_group",
				IsAmbiguous:    false,
				Confidence:     0.9,
				Alternatives:   []string{"ParameterGroupName"},
			},
			{
				UpjetFieldName: "security_groups",
				ACKFieldName:   "SecurityGroupIDs",
				ACKFieldPath:   "Spec.SecurityGroupIDs",
				TargetResource: "",
				IsAmbiguous:    true,
				Confidence:     0.8,
			},
		},
		Unmatched: []string{"notification_topic_arn"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded MatchUpjetOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.Matches) != len(original.Matches) {
		t.Fatalf("matches length mismatch: %d vs %d", len(decoded.Matches), len(original.Matches))
	}

	for i, match := range decoded.Matches {
		if match.UpjetFieldName != original.Matches[i].UpjetFieldName {
			t.Errorf("match %d upjet_field_name mismatch", i)
		}
		if match.ACKFieldName != original.Matches[i].ACKFieldName {
			t.Errorf("match %d ack_field_name mismatch", i)
		}
		if match.IsAmbiguous != original.Matches[i].IsAmbiguous {
			t.Errorf("match %d is_ambiguous mismatch", i)
		}
		if match.Confidence != original.Matches[i].Confidence {
			t.Errorf("match %d confidence mismatch", i)
		}
	}

	if len(decoded.Unmatched) != len(original.Unmatched) {
		t.Fatalf("unmatched length mismatch: %d vs %d", len(decoded.Unmatched), len(original.Unmatched))
	}
	if decoded.Unmatched[0] != original.Unmatched[0] {
		t.Errorf("unmatched[0] mismatch: %q vs %q", decoded.Unmatched[0], original.Unmatched[0])
	}
}
