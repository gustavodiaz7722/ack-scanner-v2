package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// --- Test types for matching ---

type testSourceField struct {
	FieldName      string `json:"field_name"`
	TargetResource string `json:"target_resource"`
}

type testMatchResult struct {
	Matches   []testMatchEntry `json:"matches"`
	Unmatched []string         `json:"unmatched"`
}

type testMatchEntry struct {
	SourceField string  `json:"source_field"`
	ACKField    string  `json:"ack_field"`
	Confidence  float64 `json:"confidence"`
}

// --- Tests ---

func TestMatchAll_Success(t *testing.T) {
	// Use a single controller with two resources - since mock returns in order,
	// we use the same response structure for both to avoid ordering issues
	resp := `{"matches":[{"source_field":"kms_key_id","ack_field":"KMSKeyID","confidence":0.95}],"unmatched":[]}`

	ag := newMockAgent(t,
		makeFinalTextResponse(resp),
		makeFinalTextResponse(resp),
	)

	controllers := []types.ControllerInfo{
		{
			ServiceName: "elasticache",
			Resources: []types.ResourceInfo{
				{
					Kind: "ReplicationGroup",
					StringFields: []types.FieldInfo{
						{Name: "KMSKeyID", Path: "Spec.KMSKeyID", JSONTag: "kmsKeyID"},
					},
				},
				{
					Kind: "CacheCluster",
					StringFields: []types.FieldInfo{
						{Name: "KMSKeyID", Path: "Spec.KMSKeyID", JSONTag: "kmsKeyID"},
					},
				},
			},
		},
	}

	sourceData := map[string][]testSourceField{
		"elasticache": {
			{FieldName: "kms_key_id", TargetResource: "aws_kms_key"},
		},
	}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return fmt.Sprintf("Match %s/%s", serviceName, resource.Kind)
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{
				"service":  serviceName,
				"resource": resource.Kind,
				"fields":   len(sourceFields),
			}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MatchAll(context.Background(), config, ag, controllers, sourceData, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
	}

	// Both resources should have a match
	rgResult := result.Results["elasticache_ReplicationGroup"]
	if len(rgResult.Matches) != 1 {
		t.Errorf("expected 1 match for ReplicationGroup, got %d", len(rgResult.Matches))
	}
	if rgResult.Matches[0].ACKField != "KMSKeyID" {
		t.Errorf("expected match to KMSKeyID, got %q", rgResult.Matches[0].ACKField)
	}

	ccResult := result.Results["elasticache_CacheCluster"]
	if len(ccResult.Matches) != 1 {
		t.Errorf("expected 1 match for CacheCluster, got %d", len(ccResult.Matches))
	}
}

func TestMatchAll_WithServiceMappings(t *testing.T) {
	resp := `{"matches":[{"source_field":"key_id","ack_field":"KeyID","confidence":0.9}],"unmatched":[]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	controllers := []types.ControllerInfo{
		{
			ServiceName: "kms",
			Resources: []types.ResourceInfo{
				{
					Kind: "Key",
					StringFields: []types.FieldInfo{
						{Name: "KeyID", Path: "Status.KeyID", JSONTag: "keyID"},
					},
				},
			},
		},
	}

	// Source data keyed by upjet service name (different from ACK service name)
	sourceData := map[string][]testSourceField{
		"kms_configs": {
			{FieldName: "key_id", TargetResource: "aws_kms_key"},
		},
	}

	// Service mapping resolves the indirection
	serviceMappings := map[string][]string{
		"kms": {"kms_configs"},
	}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{"service": serviceName, "resource": resource.Kind}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MatchAll(context.Background(), config, ag, controllers, sourceData, serviceMappings, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll failed: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	kmsResult := result.Results["kms_Key"]
	if len(kmsResult.Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(kmsResult.Matches))
	}
}

func TestMatchAll_FilterFields(t *testing.T) {
	resp := `{"matches":[{"source_field":"role_arn","ack_field":"RoleARN","confidence":0.9}],"unmatched":[]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	controllers := []types.ControllerInfo{
		{
			ServiceName: "autoscaling",
			Resources: []types.ResourceInfo{
				{
					Kind: "AutoScalingGroup",
					StringFields: []types.FieldInfo{
						{Name: "RoleARN", Path: "Spec.RoleARN", JSONTag: "roleARN"},
						{Name: "PolicyDocument", Path: "Spec.PolicyDocument", JSONTag: "policyDocument"},
						{Name: "Description", Path: "Spec.Description", JSONTag: "description"},
					},
				},
			},
		},
	}

	sourceData := map[string][]testSourceField{
		"autoscaling": {
			{FieldName: "role_arn", TargetResource: "aws_iam_role"},
		},
	}

	var filteredFields []string

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			// Record what fields were passed to the prompt
			for _, f := range resource.StringFields {
				filteredFields = append(filteredFields, f.Name)
			}
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{"service": serviceName, "resource": resource.Kind}
		},
		FilterFields: func(fields []types.FieldInfo) []types.FieldInfo {
			// Simulate filtering out PolicyDocument (already annotated)
			var result []types.FieldInfo
			for _, f := range fields {
				if f.Name != "PolicyDocument" {
					result = append(result, f)
				}
			}
			return result
		},
	}

	validator := &agent.JSONValidator{}

	_, err := MatchAll(context.Background(), config, ag, controllers, sourceData, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll failed: %v", err)
	}

	// Verify that PolicyDocument was filtered out
	for _, name := range filteredFields {
		if name == "PolicyDocument" {
			t.Error("PolicyDocument should have been filtered out")
		}
	}
	// Verify that other fields remained
	found := map[string]bool{}
	for _, name := range filteredFields {
		found[name] = true
	}
	if !found["RoleARN"] || !found["Description"] {
		t.Errorf("expected RoleARN and Description in filtered fields, got %v", filteredFields)
	}
}

func TestMatchAll_SkipsControllerWithNoSourceData(t *testing.T) {
	ag := newMockAgent(t) // No responses needed

	controllers := []types.ControllerInfo{
		{
			ServiceName: "orphan",
			Resources: []types.ResourceInfo{
				{Kind: "Thing", StringFields: []types.FieldInfo{{Name: "Field1"}}},
			},
		},
	}

	// No source data for this controller
	sourceData := map[string][]testSourceField{}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			t.Error("BuildPrompt should not be called for controllers with no source data")
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MatchAll(context.Background(), config, ag, controllers, sourceData, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll failed: %v", err)
	}

	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for controller with no source data, got %d", len(result.Results))
	}
}

func TestMatchAll_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	resp := `{"matches":[{"source_field":"f1","ack_field":"F1","confidence":0.8}],"unmatched":[]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	controllers := []types.ControllerInfo{
		{
			ServiceName: "ec2",
			Resources: []types.ResourceInfo{
				{Kind: "Instance", StringFields: []types.FieldInfo{{Name: "F1", Path: "Spec.F1"}}},
			},
		},
	}

	sourceData := map[string][]testSourceField{
		"ec2": {{FieldName: "f1", TargetResource: "aws_iam_role"}},
	}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{"service": serviceName, "resource": resource.Kind}
		},
	}

	validator := &agent.JSONValidator{}

	// First call
	result, err := MatchAll(context.Background(), config, ag, controllers, sourceData, nil, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll first call failed: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	// Second call: should use cache
	ag2 := newMockAgent(t)
	result2, err := MatchAll(context.Background(), config, ag2, controllers, sourceData, nil, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll second call failed: %v", err)
	}
	if len(result2.Results) != 1 {
		t.Fatalf("expected 1 cached result, got %d", len(result2.Results))
	}

	cachedResult := result2.Results["ec2_Instance"]
	if len(cachedResult.Matches) != 1 || cachedResult.Matches[0].ACKField != "F1" {
		t.Errorf("cached result mismatch: %+v", cachedResult)
	}
}

func TestMatchAll_ParallelExecution(t *testing.T) {
	responses := make([]*bedrockruntime.ConverseOutput, 4)
	for i := range 4 {
		resp := fmt.Sprintf(`{"matches":[{"source_field":"f%d","ack_field":"F%d","confidence":0.9}],"unmatched":[]}`, i, i)
		responses[i] = makeFinalTextResponse(resp)
	}

	ag := newMockAgent(t, responses...)

	controllers := []types.ControllerInfo{
		{
			ServiceName: "svc",
			Resources: []types.ResourceInfo{
				{Kind: "R0", StringFields: []types.FieldInfo{{Name: "F0"}}},
				{Kind: "R1", StringFields: []types.FieldInfo{{Name: "F1"}}},
				{Kind: "R2", StringFields: []types.FieldInfo{{Name: "F2"}}},
				{Kind: "R3", StringFields: []types.FieldInfo{{Name: "F3"}}},
			},
		},
	}

	sourceData := map[string][]testSourceField{
		"svc": {
			{FieldName: "f0"}, {FieldName: "f1"}, {FieldName: "f2"}, {FieldName: "f3"},
		},
	}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{"service": serviceName, "resource": resource.Kind}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MatchAll(context.Background(), config, ag, controllers, sourceData, nil, nil, validator, 3, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll parallel failed: %v", err)
	}

	if len(result.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result.Results))
	}
}

func TestMatchOne_Success(t *testing.T) {
	resp := `{"matches":[{"source_field":"vpc_id","ack_field":"VPCID","confidence":0.95}],"unmatched":[]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	resource := types.ResourceInfo{
		Kind: "Subnet",
		StringFields: []types.FieldInfo{
			{Name: "VPCID", Path: "Spec.VPCID", JSONTag: "vpcID"},
		},
	}

	sourceFields := []testSourceField{
		{FieldName: "vpc_id", TargetResource: "aws_vpc"},
	}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{"service": serviceName, "resource": resource.Kind}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MatchOne(context.Background(), config, ag, resource, sourceFields, "ec2", nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MatchOne failed: %v", err)
	}

	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(result.Matches))
	}
	if result.Matches[0].ACKField != "VPCID" {
		t.Errorf("expected ACKField VPCID, got %q", result.Matches[0].ACKField)
	}
}

func TestMatchAll_EmptyControllers(t *testing.T) {
	ag := newMockAgent(t)
	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MatchAll(context.Background(), config, ag, nil, nil, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MatchAll failed: %v", err)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(result.Results))
	}
}

func TestMatchAll_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ag := newMockAgent(t, makeFinalTextResponse(`{}`))

	controllers := []types.ControllerInfo{
		{
			ServiceName: "s3",
			Resources:   []types.ResourceInfo{{Kind: "Bucket", StringFields: []types.FieldInfo{{Name: "F"}}}},
		},
	}

	sourceData := map[string][]testSourceField{"s3": {{FieldName: "f"}}}

	config := MatchConfig[testSourceField, testMatchResult]{
		ToolName: "test_match",
		BuildPrompt: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMatchResult, error) {
			var r testMatchResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(serviceName string, resource types.ResourceInfo) string {
			return serviceName + "_" + resource.Kind
		},
		InputParams: func(resource types.ResourceInfo, sourceFields []testSourceField, serviceName string) map[string]any {
			return map[string]any{}
		},
	}

	validator := &agent.JSONValidator{}

	_, err := MatchAll(ctx, config, ag, controllers, sourceData, nil, nil, validator, 1, logger.Nop())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
