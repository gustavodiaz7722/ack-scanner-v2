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
	"pgregory.net/rapid"
)

// --- Mock Bedrock Client for match_terraform_refs tests ---

type mockMatchTFRefsBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockMatchTFRefsBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeMatchTFRefsFinalTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func newMatchTFRefsMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockMatchTFRefsBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Unit Tests ---

func TestMatchResourceTerraformRefs_Success(t *testing.T) {
	expectedOutput := MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{
				TFFieldName:    "volume_id",
				ACKFieldName:   "VolumeID",
				ACKFieldPath:   "spec.volumeID",
				TargetResource: "aws_ebs_volume",
				ResolutionAttr: ".id",
				Confidence:     0.95,
			},
			{
				TFFieldName:    "kms_key_id",
				ACKFieldName:   "KMSKeyID",
				ACKFieldPath:   "spec.kmsKeyID",
				TargetResource: "aws_kms_key",
				ResolutionAttr: ".arn",
				Confidence:     0.9,
			},
		},
		Unmatched: []string{"owner_id"},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchTFRefsMockAgent(t, makeMatchTFRefsFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "EBSSnapshot",
		StringFields: []types.FieldInfo{
			{Name: "VolumeID", Path: "spec.volumeID", JSONTag: "volumeID"},
			{Name: "KMSKeyID", Path: "spec.kmsKeyID", JSONTag: "kmsKeyID"},
			{Name: "Description", Path: "spec.description", JSONTag: "description"},
		},
	}

	tfRefs := []TerraformReferenceInfo{
		{FieldName: "volume_id", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95},
		{FieldName: "kms_key_id", TargetResource: "aws_kms_key", ResolutionAttr: ".arn", SignalType: "backtick_mention", Confidence: 0.8},
		{FieldName: "owner_id", TargetResource: "aws_account", ResolutionAttr: ".id", SignalType: "argument_description", Confidence: 0.5},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	result, err := MatchResourceTerraformRefs(
		context.Background(),
		ag,
		resource,
		tfRefs,
		"ebs",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceTerraformRefs failed: %v", err)
	}

	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result.Matches))
	}
	if result.Matches[0].TFFieldName != "volume_id" {
		t.Errorf("expected first match TF field 'volume_id', got %q", result.Matches[0].TFFieldName)
	}
	if result.Matches[0].ACKFieldName != "VolumeID" {
		t.Errorf("expected first match ACK field 'VolumeID', got %q", result.Matches[0].ACKFieldName)
	}
	if result.Matches[0].TargetResource != "aws_ebs_volume" {
		t.Errorf("expected target_resource 'aws_ebs_volume', got %q", result.Matches[0].TargetResource)
	}
	if result.Matches[0].ResolutionAttr != ".id" {
		t.Errorf("expected resolution_attr '.id', got %q", result.Matches[0].ResolutionAttr)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
	if result.Unmatched[0] != "owner_id" {
		t.Errorf("expected unmatched 'owner_id', got %q", result.Unmatched[0])
	}
}

func TestMatchResourceTerraformRefs_NoMatches(t *testing.T) {
	expectedOutput := MatchTerraformRefsOutput{
		Matches:   []TerraformRefFieldMatch{},
		Unmatched: []string{"some_field"},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchTFRefsMockAgent(t, makeMatchTFRefsFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "LogGroup",
		StringFields: []types.FieldInfo{
			{Name: "Name", Path: "spec.name", JSONTag: "name"},
		},
	}

	tfRefs := []TerraformReferenceInfo{
		{FieldName: "some_field", TargetResource: "aws_other", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.7},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	result, err := MatchResourceTerraformRefs(
		context.Background(),
		ag,
		resource,
		tfRefs,
		"cloudwatchlogs",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceTerraformRefs failed: %v", err)
	}

	if len(result.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result.Matches))
	}
	if len(result.Unmatched) != 1 {
		t.Errorf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
}

func TestMatchResourceTerraformRefs_WithAlternatives(t *testing.T) {
	expectedOutput := MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{
				TFFieldName:    "subnet_id",
				ACKFieldName:   "SubnetID",
				ACKFieldPath:   "spec.subnetID",
				TargetResource: "aws_subnet",
				ResolutionAttr: ".id",
				Confidence:     0.85,
				Alternatives:   []string{"VPCSubnetID"},
			},
		},
		Unmatched: []string{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchTFRefsMockAgent(t, makeMatchTFRefsFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "Instance",
		StringFields: []types.FieldInfo{
			{Name: "SubnetID", Path: "spec.subnetID", JSONTag: "subnetID"},
			{Name: "VPCSubnetID", Path: "spec.vpcSubnetID", JSONTag: "vpcSubnetID"},
		},
	}

	tfRefs := []TerraformReferenceInfo{
		{FieldName: "subnet_id", TargetResource: "aws_subnet", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	result, err := MatchResourceTerraformRefs(
		context.Background(),
		ag,
		resource,
		tfRefs,
		"ec2",
		nil,
		validator,
		logger.Nop(),
	)
	if err != nil {
		t.Fatalf("MatchResourceTerraformRefs failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if len(result.Matches[0].Alternatives) != 1 {
		t.Fatalf("expected 1 alternative, got %d", len(result.Matches[0].Alternatives))
	}
	if result.Matches[0].Alternatives[0] != "VPCSubnetID" {
		t.Errorf("expected alternative 'VPCSubnetID', got %q", result.Matches[0].Alternatives[0])
	}
}

func TestMatchResourceTerraformRefs_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	expectedOutput := MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{
				TFFieldName:    "role_arn",
				ACKFieldName:   "RoleARN",
				ACKFieldPath:   "spec.roleARN",
				TargetResource: "aws_iam_role",
				ResolutionAttr: ".arn",
				Confidence:     0.95,
			},
		},
		Unmatched: []string{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchTFRefsMockAgent(t, makeMatchTFRefsFinalTextResponse(string(responseJSON)))

	resource := types.ResourceInfo{
		Kind: "Function",
		StringFields: []types.FieldInfo{
			{Name: "RoleARN", Path: "spec.roleARN", JSONTag: "roleARN"},
		},
	}

	tfRefs := []TerraformReferenceInfo{
		{FieldName: "role_arn", TargetResource: "aws_iam_role", ResolutionAttr: ".arn", SignalType: "hcl_example", Confidence: 0.95},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	// First call — should hit agent
	result, err := MatchResourceTerraformRefs(
		context.Background(),
		ag,
		resource,
		tfRefs,
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
	ag2 := newMatchTFRefsMockAgent(t)
	result2, err := MatchResourceTerraformRefs(
		context.Background(),
		ag2,
		resource,
		tfRefs,
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
		t.Errorf("cached result has wrong ACK field name: %q", result2.Matches[0].ACKFieldName)
	}
}

func TestMatchAllResourcesTerraformRefs_Success(t *testing.T) {
	// Use same output structure for both to avoid order-dependency with goroutines
	output1 := MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.95},
		},
		Unmatched: []string{},
	}
	output2 := MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "subnet_id", ACKFieldName: "SubnetID", ACKFieldPath: "spec.subnetID", TargetResource: "aws_subnet", ResolutionAttr: ".id", Confidence: 0.9},
		},
		Unmatched: []string{},
	}

	resp1, _ := json.Marshal(output1)
	resp2, _ := json.Marshal(output2)

	ag := newMatchTFRefsMockAgent(t,
		makeMatchTFRefsFinalTextResponse(string(resp1)),
		makeMatchTFRefsFinalTextResponse(string(resp2)),
	)

	controllers := []types.ControllerInfo{
		{
			ServiceName: "ebs",
			Resources: []types.ResourceInfo{
				{Kind: "Snapshot", StringFields: []types.FieldInfo{
					{Name: "VolumeID", Path: "spec.volumeID", JSONTag: "volumeID"},
				}},
			},
		},
		{
			ServiceName: "ec2",
			Resources: []types.ResourceInfo{
				{Kind: "Instance", StringFields: []types.FieldInfo{
					{Name: "SubnetID", Path: "spec.subnetID", JSONTag: "subnetID"},
				}},
			},
		},
	}

	analysisResults := map[string]*AnalyzeTerraformRefsOutput{
		"ebs_snapshot": {
			ResourceType: "aws_ebs_snapshot",
			References: []TerraformReferenceInfo{
				{FieldName: "volume_id", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95},
			},
		},
		"instance": {
			ResourceType: "aws_instance",
			References: []TerraformReferenceInfo{
				{FieldName: "subnet_id", TargetResource: "aws_subnet", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95},
			},
		},
	}

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

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	result, err := MatchAllResourcesTerraformRefs(
		context.Background(), ag, controllers, analysisResults, mappings, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAllResourcesTerraformRefs failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// Verify both resources produced results (don't assume order of execution)
	totalMatches := 0
	for _, res := range result.Results {
		totalMatches += len(res.Matches)
	}
	if totalMatches != 2 {
		t.Errorf("expected 2 total matches across all resources, got %d", totalMatches)
	}
}

func TestMatchAllResourcesTerraformRefs_FiltersAnnotatedFields(t *testing.T) {
	// The resource has annotated fields that should be filtered out
	expectedOutput := MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "subnet_id", ACKFieldName: "SubnetID", ACKFieldPath: "spec.subnetID", TargetResource: "aws_subnet", ResolutionAttr: ".id", Confidence: 0.9},
		},
		Unmatched: []string{},
	}

	responseJSON, _ := json.Marshal(expectedOutput)
	ag := newMatchTFRefsMockAgent(t, makeMatchTFRefsFinalTextResponse(string(responseJSON)))

	controllers := []types.ControllerInfo{
		{
			ServiceName: "ec2",
			Resources: []types.ResourceInfo{
				{Kind: "Instance", StringFields: []types.FieldInfo{
					{Name: "SubnetID", Path: "spec.subnetID", JSONTag: "subnetID"},
					{Name: "PolicyDocument", Path: "spec.policyDocument", JSONTag: "policyDocument", IsDocument: true},
					{Name: "AssumeRolePolicy", Path: "spec.assumeRolePolicy", JSONTag: "assumeRolePolicy", IsIAMPolicy: true},
					{Name: "VPCID", Path: "spec.vpcID", JSONTag: "vpcID", HasReference: true},
				}},
			},
		},
	}

	analysisResults := map[string]*AnalyzeTerraformRefsOutput{
		"instance": {
			ResourceType: "aws_instance",
			References: []TerraformReferenceInfo{
				{FieldName: "subnet_id", TargetResource: "aws_subnet", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95},
			},
		},
	}

	mappings := []TerraformRefMapping{
		{
			ServiceName: "ec2",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_instance", DocFilePath: "website/docs/r/instance.html.markdown", Confidence: 0.9},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	result, err := MatchAllResourcesTerraformRefs(
		context.Background(), ag, controllers, analysisResults, mappings, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAllResourcesTerraformRefs failed: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
}

func TestMatchAllResourcesTerraformRefs_EmptyAnalysisResults(t *testing.T) {
	ag := newMatchTFRefsMockAgent(t) // no responses needed

	controllers := []types.ControllerInfo{
		{
			ServiceName: "ec2",
			Resources:   []types.ResourceInfo{{Kind: "Instance"}},
		},
	}

	mappings := []TerraformRefMapping{
		{
			ServiceName: "ec2",
			TFDocFiles: []TerraformRefMappingEntry{
				{TFResourceType: "aws_instance", DocFilePath: "website/docs/r/instance.html.markdown", Confidence: 0.9},
			},
		},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	// Empty analysis results → no source data for matching
	result, err := MatchAllResourcesTerraformRefs(
		context.Background(), ag, controllers, map[string]*AnalyzeTerraformRefsOutput{}, mappings, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAllResourcesTerraformRefs failed: %v", err)
	}

	// With no references found in analysis, controllers should be skipped
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty analysis, got %d", len(result.Results))
	}
}

func TestMatchAllResourcesTerraformRefs_Parallel(t *testing.T) {
	responses := make([]*bedrockruntime.ConverseOutput, 3)
	for i := range 3 {
		output := MatchTerraformRefsOutput{
			Matches: []TerraformRefFieldMatch{
				{TFFieldName: fmt.Sprintf("field_%d", i), ACKFieldName: fmt.Sprintf("Field%d", i), ACKFieldPath: fmt.Sprintf("spec.field%d", i), TargetResource: fmt.Sprintf("aws_svc%d", i), ResolutionAttr: ".id", Confidence: 0.9},
			},
			Unmatched: []string{},
		}
		resp, _ := json.Marshal(output)
		responses[i] = makeMatchTFRefsFinalTextResponse(string(resp))
	}

	ag := newMatchTFRefsMockAgent(t, responses...)

	controllers := []types.ControllerInfo{
		{ServiceName: "svc0", Resources: []types.ResourceInfo{{Kind: "Res0", StringFields: []types.FieldInfo{{Name: "Field0", Path: "spec.field0", JSONTag: "field0"}}}}},
		{ServiceName: "svc1", Resources: []types.ResourceInfo{{Kind: "Res1", StringFields: []types.FieldInfo{{Name: "Field1", Path: "spec.field1", JSONTag: "field1"}}}}},
		{ServiceName: "svc2", Resources: []types.ResourceInfo{{Kind: "Res2", StringFields: []types.FieldInfo{{Name: "Field2", Path: "spec.field2", JSONTag: "field2"}}}}},
	}

	analysisResults := map[string]*AnalyzeTerraformRefsOutput{
		"svc0_res": {References: []TerraformReferenceInfo{{FieldName: "field_0", TargetResource: "aws_svc0", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.9}}},
		"svc1_res": {References: []TerraformReferenceInfo{{FieldName: "field_1", TargetResource: "aws_svc1", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.9}}},
		"svc2_res": {References: []TerraformReferenceInfo{{FieldName: "field_2", TargetResource: "aws_svc2", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.9}}},
	}

	mappings := []TerraformRefMapping{
		{ServiceName: "svc0", TFDocFiles: []TerraformRefMappingEntry{{TFResourceType: "aws_svc0_res", DocFilePath: "docs/r/svc0_res.html.markdown", Confidence: 0.9}}},
		{ServiceName: "svc1", TFDocFiles: []TerraformRefMappingEntry{{TFResourceType: "aws_svc1_res", DocFilePath: "docs/r/svc1_res.html.markdown", Confidence: 0.9}}},
		{ServiceName: "svc2", TFDocFiles: []TerraformRefMappingEntry{{TFResourceType: "aws_svc2_res", DocFilePath: "docs/r/svc2_res.html.markdown", Confidence: 0.9}}},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"matches", "unmatched_tf_fields"}}

	result, err := MatchAllResourcesTerraformRefs(
		context.Background(), ag, controllers, analysisResults, mappings, nil, validator, 2, logger.Nop())
	if err != nil {
		t.Fatalf("parallel matching failed: %v", err)
	}

	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}
}

// --- Field Filtering Tests (for TF refs matching context) ---

func TestTFRefsMatchFilterFields_MixedAnnotations(t *testing.T) {
	fields := []types.FieldInfo{
		{Name: "SubnetID", Path: "spec.subnetID", JSONTag: "subnetID"},
		{Name: "PolicyDocument", Path: "spec.policyDocument", JSONTag: "policyDocument", IsDocument: true},
		{Name: "AssumeRolePolicy", Path: "spec.assumeRolePolicy", JSONTag: "assumeRolePolicy", IsIAMPolicy: true},
		{Name: "VPCID", Path: "spec.vpcID", JSONTag: "vpcID", HasReference: true},
		{Name: "RoleARN", Path: "spec.roleARN", JSONTag: "roleARN"},
	}

	filtered := filterFieldsForReferenceMatching(fields)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered fields, got %d", len(filtered))
	}
	if filtered[0].Name != "SubnetID" {
		t.Errorf("expected first filtered field 'SubnetID', got %q", filtered[0].Name)
	}
	if filtered[1].Name != "RoleARN" {
		t.Errorf("expected second filtered field 'RoleARN', got %q", filtered[1].Name)
	}
}

func TestTFRefsMatchFilterFields_AllAnnotated(t *testing.T) {
	fields := []types.FieldInfo{
		{Name: "Doc", IsDocument: true},
		{Name: "Policy", IsIAMPolicy: true},
		{Name: "Ref", HasReference: true},
	}

	filtered := filterFieldsForReferenceMatching(fields)

	if len(filtered) != 0 {
		t.Errorf("expected 0 filtered fields, got %d", len(filtered))
	}
}

func TestTFRefsMatchFilterFields_NoneAnnotated(t *testing.T) {
	fields := []types.FieldInfo{
		{Name: "SubnetID"},
		{Name: "RoleARN"},
		{Name: "KMSKeyID"},
	}

	filtered := filterFieldsForReferenceMatching(fields)

	if len(filtered) != 3 {
		t.Errorf("expected 3 filtered fields, got %d", len(filtered))
	}
}

func TestTFRefsMatchFilterFields_Empty(t *testing.T) {
	filtered := filterFieldsForReferenceMatching(nil)
	if len(filtered) != 0 {
		t.Errorf("expected 0 filtered fields for nil input, got %d", len(filtered))
	}
}

// --- Validation Tests ---

func TestValidateMatchTerraformRefsOutput_ValidOutput(t *testing.T) {
	validACKFields := map[string]bool{
		"VolumeID": true,
		"KMSKeyID": true,
	}

	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.95},
			{TFFieldName: "kms_key_id", ACKFieldName: "KMSKeyID", ACKFieldPath: "spec.kmsKeyID", TargetResource: "aws_kms_key", ResolutionAttr: ".arn", Confidence: 0.9},
		},
		Unmatched: []string{"owner_id"},
	}

	if err := ValidateMatchTerraformRefsOutput(output, validACKFields); err != nil {
		t.Errorf("expected valid output, got error: %v", err)
	}
}

func TestValidateMatchTerraformRefsOutput_NilOutput(t *testing.T) {
	if err := ValidateMatchTerraformRefsOutput(nil, nil); err == nil {
		t.Error("expected error for nil output")
	}
}

func TestValidateMatchTerraformRefsOutput_EmptyTFFieldName(t *testing.T) {
	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.9},
		},
	}
	if err := ValidateMatchTerraformRefsOutput(output, nil); err == nil {
		t.Error("expected error for empty terraform_field_name")
	}
}

func TestValidateMatchTerraformRefsOutput_EmptyACKFieldName(t *testing.T) {
	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.9},
		},
	}
	if err := ValidateMatchTerraformRefsOutput(output, nil); err == nil {
		t.Error("expected error for empty ack_field_name")
	}
}

func TestValidateMatchTerraformRefsOutput_EmptyACKFieldPath(t *testing.T) {
	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.9},
		},
	}
	if err := ValidateMatchTerraformRefsOutput(output, nil); err == nil {
		t.Error("expected error for empty ack_field_path")
	}
}

func TestValidateMatchTerraformRefsOutput_EmptyTargetResource(t *testing.T) {
	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "", ResolutionAttr: ".id", Confidence: 0.9},
		},
	}
	if err := ValidateMatchTerraformRefsOutput(output, nil); err == nil {
		t.Error("expected error for empty target_resource")
	}
}

func TestValidateMatchTerraformRefsOutput_InvalidResolutionAttr(t *testing.T) {
	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".key", Confidence: 0.9},
		},
	}
	if err := ValidateMatchTerraformRefsOutput(output, nil); err == nil {
		t.Error("expected error for invalid resolution_attr")
	}
}

func TestValidateMatchTerraformRefsOutput_ConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"too high", 1.1},
		{"very negative", -5.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &MatchTerraformRefsOutput{
				Matches: []TerraformRefFieldMatch{
					{TFFieldName: "f", ACKFieldName: "F", ACKFieldPath: "spec.f", TargetResource: "aws_x", ResolutionAttr: ".id", Confidence: tt.confidence},
				},
			}
			if err := ValidateMatchTerraformRefsOutput(output, nil); err == nil {
				t.Errorf("expected error for confidence %f", tt.confidence)
			}
		})
	}
}

func TestValidateMatchTerraformRefsOutput_InvalidACKField(t *testing.T) {
	validACKFields := map[string]bool{"RoleARN": true}

	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "role_arn", ACKFieldName: "InvalidField", ACKFieldPath: "spec.invalid", TargetResource: "aws_iam_role", ResolutionAttr: ".arn", Confidence: 0.9},
		},
	}

	if err := ValidateMatchTerraformRefsOutput(output, validACKFields); err == nil {
		t.Error("expected error for invalid ACK field name")
	}
}

// --- Completeness Tests ---

func TestValidateMatchTerraformRefsCompleteness_Valid(t *testing.T) {
	tfRefs := []TerraformReferenceInfo{
		{FieldName: "volume_id"},
		{FieldName: "kms_key_id"},
		{FieldName: "owner_id"},
	}

	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.9},
			{TFFieldName: "kms_key_id", ACKFieldName: "KMSKeyID", ACKFieldPath: "spec.kmsKeyID", TargetResource: "aws_kms_key", ResolutionAttr: ".arn", Confidence: 0.8},
		},
		Unmatched: []string{"owner_id"},
	}

	if err := ValidateMatchTerraformRefsCompleteness(output, tfRefs); err != nil {
		t.Errorf("expected complete output, got error: %v", err)
	}
}

func TestValidateMatchTerraformRefsCompleteness_MissingField(t *testing.T) {
	tfRefs := []TerraformReferenceInfo{
		{FieldName: "volume_id"},
		{FieldName: "kms_key_id"},
		{FieldName: "owner_id"},
	}

	output := &MatchTerraformRefsOutput{
		Matches: []TerraformRefFieldMatch{
			{TFFieldName: "volume_id", ACKFieldName: "VolumeID", ACKFieldPath: "spec.volumeID", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", Confidence: 0.9},
		},
		Unmatched: []string{"owner_id"},
		// kms_key_id is missing!
	}

	if err := ValidateMatchTerraformRefsCompleteness(output, tfRefs); err == nil {
		t.Error("expected error for missing kms_key_id in output")
	}
}

func TestValidateMatchTerraformRefsCompleteness_NilOutput(t *testing.T) {
	if err := ValidateMatchTerraformRefsCompleteness(nil, nil); err == nil {
		t.Error("expected error for nil output")
	}
}

// --- Prompt Content Tests ---

func TestBuildMatchTerraformRefsPrompt_ContainsExpectedContent(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "EBSSnapshot",
		StringFields: []types.FieldInfo{
			{Name: "VolumeID", Path: "spec.volumeID", JSONTag: "volumeID"},
			{Name: "KMSKeyID", Path: "spec.kmsKeyID", JSONTag: "kmsKeyID"},
		},
	}

	tfRefs := []TerraformReferenceInfo{
		{FieldName: "volume_id", TargetResource: "aws_ebs_volume", ResolutionAttr: ".id", SignalType: "hcl_example", Confidence: 0.95},
	}

	prompt := buildMatchTerraformRefsPrompt(resource, tfRefs, "ebs")

	expectedPhrases := []string{
		"cross-resource references",
		"references:",
		"ebs",
		"EBSSnapshot",
		"VolumeID",
		"KMSKeyID",
		"volume_id",
		"aws_ebs_volume",
		".id",
		"hcl_example",
		"naming convention",
		"PascalCase",
		"snake_case",
		"terraform_field_name",
		"ack_field_name",
		"ack_field_path",
		"target_resource",
		"resolution_attr",
		"confidence",
		"unmatched_tf_fields",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstr(prompt, phrase) {
			t.Errorf("prompt missing expected phrase: %q", phrase)
		}
	}
}

// --- Config Tests ---

func TestTerraformRefsMatchConfig_ToolName(t *testing.T) {
	config := TerraformRefsMatchConfig()
	if config.ToolName != "match_terraform_refs" {
		t.Errorf("expected ToolName 'match_terraform_refs', got %q", config.ToolName)
	}
}

func TestTerraformRefsMatchConfig_ItemKey(t *testing.T) {
	config := TerraformRefsMatchConfig()
	key := config.ItemKey("ebs", types.ResourceInfo{Kind: "Snapshot"})
	if key != "ebs_Snapshot" {
		t.Errorf("expected key 'ebs_Snapshot', got %q", key)
	}
}

// --- Parse Response Tests ---

func TestParseMatchTerraformRefsResponse_ValidJSON(t *testing.T) {
	input := `{"matches":[{"terraform_field_name":"volume_id","ack_field_name":"VolumeID","ack_field_path":"spec.volumeID","target_resource":"aws_ebs_volume","resolution_attr":".id","confidence":0.95}],"unmatched_tf_fields":["owner_id"]}`

	result, err := parseMatchTerraformRefsResponse(input)
	if err != nil {
		t.Fatalf("parseMatchTerraformRefsResponse failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if result.Matches[0].TFFieldName != "volume_id" {
		t.Errorf("expected 'volume_id', got %q", result.Matches[0].TFFieldName)
	}
	if result.Matches[0].TargetResource != "aws_ebs_volume" {
		t.Errorf("expected 'aws_ebs_volume', got %q", result.Matches[0].TargetResource)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("expected 1 unmatched, got %d", len(result.Unmatched))
	}
}

func TestParseMatchTerraformRefsResponse_InvalidJSON(t *testing.T) {
	_, err := parseMatchTerraformRefsResponse("not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseMatchTerraformRefsResponse_EmptyMatches(t *testing.T) {
	input := `{"matches":[],"unmatched_tf_fields":["field1","field2"]}`

	result, err := parseMatchTerraformRefsResponse(input)
	if err != nil {
		t.Fatalf("parseMatchTerraformRefsResponse failed: %v", err)
	}

	if len(result.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result.Matches))
	}
	if len(result.Unmatched) != 2 {
		t.Errorf("expected 2 unmatched, got %d", len(result.Unmatched))
	}
}

// --- InputParams Tests ---

func TestBuildMatchTerraformRefsInputParams(t *testing.T) {
	resource := types.ResourceInfo{
		Kind: "Snapshot",
		StringFields: []types.FieldInfo{
			{Name: "VolumeID"},
			{Name: "KMSKeyID"},
		},
	}

	tfRefs := []TerraformReferenceInfo{
		{FieldName: "volume_id"},
		{FieldName: "kms_key_id"},
		{FieldName: "owner_id"},
	}

	params := buildMatchTerraformRefsInputParams(resource, tfRefs, "ebs")

	if params["service_name"] != "ebs" {
		t.Errorf("expected service_name 'ebs', got %v", params["service_name"])
	}
	if params["resource_kind"] != "Snapshot" {
		t.Errorf("expected resource_kind 'Snapshot', got %v", params["resource_kind"])
	}
	if params["ack_field_count"] != 2 {
		t.Errorf("expected ack_field_count 2, got %v", params["ack_field_count"])
	}
	if params["tf_ref_count"] != 3 {
		t.Errorf("expected tf_ref_count 3, got %v", params["tf_ref_count"])
	}

	ackNames, ok := params["ack_field_names"].([]string)
	if !ok {
		t.Fatal("ack_field_names is not []string")
	}
	if len(ackNames) != 2 || ackNames[0] != "VolumeID" || ackNames[1] != "KMSKeyID" {
		t.Errorf("unexpected ack_field_names: %v", ackNames)
	}

	tfNames, ok := params["tf_field_names"].([]string)
	if !ok {
		t.Fatal("tf_field_names is not []string")
	}
	if len(tfNames) != 3 || tfNames[0] != "volume_id" || tfNames[1] != "kms_key_id" || tfNames[2] != "owner_id" {
		t.Errorf("unexpected tf_field_names: %v", tfNames)
	}
}

// --- Property-Based Tests ---

// TestProperty_MatchTerraformRefsOutputSchemaValidity verifies that for any
// match result, each entry SHALL have terraform_field_name, ack_field_name,
// ack_field_path, target_resource, resolution_attr, and confidence (between 0 and 1).
//
// **Validates: Requirements 12.2, 12.3, 12.4**
func TestProperty_MatchTerraformRefsOutputSchemaValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validResolutionAttrs := []string{".id", ".arn", ".name"}

		// Generate a set of valid ACK field names
		numACKFields := rapid.IntRange(1, 15).Draw(t, "numACKFields")
		validACKFields := make(map[string]bool)
		ackFieldNames := make([]string, numACKFields)
		for i := range ackFieldNames {
			nameLen := rapid.IntRange(3, 20).Draw(t, "ackFieldNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('A', 'Z').Draw(t, "ackFieldByte"))
			}
			ackFieldNames[i] = string(nameBytes)
			validACKFields[ackFieldNames[i]] = true
		}

		// Generate match entries that reference valid ACK fields
		numMatches := rapid.IntRange(0, 10).Draw(t, "numMatches")
		matches := make([]TerraformRefFieldMatch, numMatches)
		for i := range matches {
			// Generate terraform field name
			tfNameLen := rapid.IntRange(3, 25).Draw(t, "tfNameLen")
			tfNameBytes := make([]byte, tfNameLen)
			for j := range tfNameBytes {
				tfNameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "tfNameByte"))
			}

			// Pick a valid ACK field
			ackIdx := rapid.IntRange(0, numACKFields-1).Draw(t, "ackIdx")
			ackFieldName := ackFieldNames[ackIdx]

			// Generate target resource
			targetLen := rapid.IntRange(5, 20).Draw(t, "targetLen")
			targetBytes := make([]byte, targetLen)
			for j := range targetBytes {
				targetBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "targetByte"))
			}

			// Generate confidence between 0 and 1
			confidenceInt := rapid.IntRange(0, 100).Draw(t, "confidenceInt")
			confidence := float64(confidenceInt) / 100.0

			matches[i] = TerraformRefFieldMatch{
				TFFieldName:    string(tfNameBytes),
				ACKFieldName:   ackFieldName,
				ACKFieldPath:   "spec." + ackFieldName,
				TargetResource: "aws_" + string(targetBytes),
				ResolutionAttr: rapid.SampledFrom(validResolutionAttrs).Draw(t, "resAttr"),
				Confidence:     confidence,
			}
		}

		// Generate unmatched fields
		numUnmatched := rapid.IntRange(0, 5).Draw(t, "numUnmatched")
		unmatched := make([]string, numUnmatched)
		for i := range unmatched {
			nameLen := rapid.IntRange(3, 20).Draw(t, "unmatchedNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "unmatchedByte"))
			}
			unmatched[i] = string(nameBytes)
		}

		output := &MatchTerraformRefsOutput{
			Matches:   matches,
			Unmatched: unmatched,
		}

		// Property: ValidateMatchTerraformRefsOutput must pass for well-formed output
		err := ValidateMatchTerraformRefsOutput(output, validACKFields)
		if err != nil {
			t.Fatalf("valid output failed validation: %v", err)
		}

		// Property: every match has non-empty terraform_field_name
		for i, match := range output.Matches {
			if match.TFFieldName == "" {
				t.Fatalf("matches[%d]: terraform_field_name is empty", i)
			}
		}

		// Property: every match has non-empty ack_field_name
		for i, match := range output.Matches {
			if match.ACKFieldName == "" {
				t.Fatalf("matches[%d]: ack_field_name is empty", i)
			}
		}

		// Property: every match has non-empty ack_field_path
		for i, match := range output.Matches {
			if match.ACKFieldPath == "" {
				t.Fatalf("matches[%d]: ack_field_path is empty", i)
			}
		}

		// Property: every match has non-empty target_resource
		for i, match := range output.Matches {
			if match.TargetResource == "" {
				t.Fatalf("matches[%d]: target_resource is empty", i)
			}
		}

		// Property: every resolution_attr is valid
		validResAttrSet := map[string]bool{".id": true, ".arn": true, ".name": true}
		for i, match := range output.Matches {
			if !validResAttrSet[match.ResolutionAttr] {
				t.Fatalf("matches[%d]: resolution_attr %q is not valid", i, match.ResolutionAttr)
			}
		}

		// Property: every confidence is between 0 and 1
		for i, match := range output.Matches {
			if match.Confidence < 0 || match.Confidence > 1 {
				t.Fatalf("matches[%d]: confidence %f is out of range [0, 1]", i, match.Confidence)
			}
		}

		// Property: every ack_field_name refers to a valid ACK field
		for i, match := range output.Matches {
			if !validACKFields[match.ACKFieldName] {
				t.Fatalf("matches[%d]: ack_field_name %q is not a valid ACK field", i, match.ACKFieldName)
			}
		}
	})
}

// TestProperty_MatchTerraformRefsCompleteness verifies that for any set of TF
// reference fields provided to matching, every field appears either in the
// matches list or in the unmatched list — none are silently dropped.
//
// **Validates: Requirements 12.4, 12.5**
func TestProperty_MatchTerraformRefsCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validResolutionAttrs := []string{".id", ".arn", ".name"}

		// Generate TF reference fields
		numTFRefs := rapid.IntRange(1, 20).Draw(t, "numTFRefs")
		tfRefs := make([]TerraformReferenceInfo, numTFRefs)
		for i := range tfRefs {
			nameLen := rapid.IntRange(3, 25).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "nameByte"))
			}
			fieldName := string(nameBytes) + fmt.Sprintf("_%d", i) // ensure uniqueness

			tfRefs[i] = TerraformReferenceInfo{
				FieldName:      fieldName,
				TargetResource: "aws_test_resource",
				ResolutionAttr: ".id",
				SignalType:     "hcl_example",
				Confidence:     0.9,
			}
		}

		// Generate ACK field names for matches
		numACKFields := rapid.IntRange(1, 10).Draw(t, "numACKFields")
		ackFieldNames := make([]string, numACKFields)
		for i := range ackFieldNames {
			nameLen := rapid.IntRange(3, 15).Draw(t, "ackNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('A', 'Z').Draw(t, "ackByte"))
			}
			ackFieldNames[i] = string(nameBytes)
		}

		// Randomly partition TF refs into matched and unmatched
		var matches []TerraformRefFieldMatch
		var unmatched []string

		for _, tfRef := range tfRefs {
			isMatched := rapid.Bool().Draw(t, "isMatched")
			if isMatched && numACKFields > 0 {
				ackIdx := rapid.IntRange(0, numACKFields-1).Draw(t, "matchACKIdx")
				confidenceInt := rapid.IntRange(50, 100).Draw(t, "matchConfidence")
				matches = append(matches, TerraformRefFieldMatch{
					TFFieldName:    tfRef.FieldName,
					ACKFieldName:   ackFieldNames[ackIdx],
					ACKFieldPath:   "spec." + ackFieldNames[ackIdx],
					TargetResource: tfRef.TargetResource,
					ResolutionAttr: rapid.SampledFrom(validResolutionAttrs).Draw(t, "resAttr"),
					Confidence:     float64(confidenceInt) / 100.0,
				})
			} else {
				unmatched = append(unmatched, tfRef.FieldName)
			}
		}

		output := &MatchTerraformRefsOutput{
			Matches:   matches,
			Unmatched: unmatched,
		}

		// Property: every TF reference field must appear in either matches or unmatched
		err := ValidateMatchTerraformRefsCompleteness(output, tfRefs)
		if err != nil {
			t.Fatalf("completeness check failed: %v", err)
		}
	})
}

// TestProperty_IncompleteTerraformRefsMatchDetected verifies that
// ValidateMatchTerraformRefsCompleteness correctly detects when a TF
// reference field is silently dropped.
//
// **Validates: Requirements 12.5**
func TestProperty_IncompleteTerraformRefsMatchDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate TF reference fields
		numTFRefs := rapid.IntRange(2, 10).Draw(t, "numTFRefs")
		tfRefs := make([]TerraformReferenceInfo, numTFRefs)
		for i := range tfRefs {
			nameLen := rapid.IntRange(3, 15).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "nameByte"))
			}
			tfRefs[i] = TerraformReferenceInfo{
				FieldName:      string(nameBytes) + fmt.Sprintf("_%d", i),
				TargetResource: "aws_test",
				ResolutionAttr: ".id",
				SignalType:     "hcl_example",
				Confidence:     0.9,
			}
		}

		// Deliberately drop one field from the output
		dropIdx := rapid.IntRange(0, numTFRefs-1).Draw(t, "dropIdx")

		var unmatched []string
		for i, tf := range tfRefs {
			if i == dropIdx {
				continue // silently drop
			}
			unmatched = append(unmatched, tf.FieldName)
		}

		output := &MatchTerraformRefsOutput{
			Matches:   []TerraformRefFieldMatch{},
			Unmatched: unmatched,
		}

		// Property: ValidateMatchTerraformRefsCompleteness must detect the dropped field
		err := ValidateMatchTerraformRefsCompleteness(output, tfRefs)
		if err == nil {
			t.Fatalf("expected completeness error for dropped field %q, got nil", tfRefs[dropIdx].FieldName)
		}
	})
}

// TestProperty_FilterFieldsForReferenceMatching_NeverIncludesAnnotated verifies
// that filterFieldsForReferenceMatching never includes fields with IsDocument,
// IsIAMPolicy, or HasReference set to true.
//
// **Validates: Requirements 12.2**
func TestProperty_FilterFieldsForReferenceMatching_NeverIncludesAnnotated(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numFields := rapid.IntRange(0, 20).Draw(t, "numFields")
		fields := make([]types.FieldInfo, numFields)

		for i := range fields {
			nameLen := rapid.IntRange(3, 15).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('A', 'Z').Draw(t, "nameByte"))
			}

			fields[i] = types.FieldInfo{
				Name:         string(nameBytes),
				Path:         "spec." + string(nameBytes),
				IsDocument:   rapid.Bool().Draw(t, "isDoc"),
				IsIAMPolicy:  rapid.Bool().Draw(t, "isIAM"),
				HasReference: rapid.Bool().Draw(t, "hasRef"),
			}
		}

		filtered := filterFieldsForReferenceMatching(fields)

		// Property: no filtered field has any annotation set
		for _, f := range filtered {
			if f.IsDocument {
				t.Fatalf("filtered field %q has IsDocument=true", f.Name)
			}
			if f.IsIAMPolicy {
				t.Fatalf("filtered field %q has IsIAMPolicy=true", f.Name)
			}
			if f.HasReference {
				t.Fatalf("filtered field %q has HasReference=true", f.Name)
			}
		}

		// Property: all unannotated fields are present in filtered output
		for _, f := range fields {
			if !f.IsDocument && !f.IsIAMPolicy && !f.HasReference {
				found := false
				for _, ff := range filtered {
					if ff.Name == f.Name {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("unannotated field %q missing from filtered output", f.Name)
				}
			}
		}
	})
}
