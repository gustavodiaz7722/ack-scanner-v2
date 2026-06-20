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

// --- Mock Bedrock Client for map_upjet tests ---

type mockUpjetBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockUpjetBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeUpjetFinalTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func newUpjetMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockUpjetBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Tests ---

func TestMapControllerToUpjet_Success(t *testing.T) {
	resp := `{"service_name":"s3","upjet_configs":[{"upjet_service":"s3","file_path":"config/s3/config.go","confidence":0.95}]}`
	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(resp))

	controller := types.ControllerInfo{
		ServiceName: "s3",
		Resources:   []types.ResourceInfo{{Kind: "Bucket"}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
		{ServiceName: "ec2", FilePath: "config/ec2/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapControllerToUpjet(context.Background(), ag, controller, upjetConfigs, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapControllerToUpjet failed: %v", err)
	}

	if result.ServiceName != "s3" {
		t.Errorf("expected service_name 's3', got %q", result.ServiceName)
	}
	if len(result.UpjetConfigs) != 1 {
		t.Fatalf("expected 1 upjet config, got %d", len(result.UpjetConfigs))
	}
	if result.UpjetConfigs[0].UpjetService != "s3" {
		t.Errorf("expected upjet_service 's3', got %q", result.UpjetConfigs[0].UpjetService)
	}
	if result.UpjetConfigs[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.UpjetConfigs[0].Confidence)
	}
}

func TestMapControllerToUpjet_NoMatch(t *testing.T) {
	resp := `{"service_name":"pipes","upjet_configs":[],"no_match_reason":"No corresponding Upjet config exists for the ACK pipes service"}`
	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(resp))

	controller := types.ControllerInfo{
		ServiceName: "pipes",
		Resources:   []types.ResourceInfo{{Kind: "Pipe"}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
		{ServiceName: "ec2", FilePath: "config/ec2/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapControllerToUpjet(context.Background(), ag, controller, upjetConfigs, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapControllerToUpjet failed: %v", err)
	}

	if result.ServiceName != "pipes" {
		t.Errorf("expected service_name 'pipes', got %q", result.ServiceName)
	}
	if len(result.UpjetConfigs) != 0 {
		t.Errorf("expected 0 upjet configs, got %d", len(result.UpjetConfigs))
	}
	if result.NoMatchReason == "" {
		t.Error("expected non-empty no_match_reason")
	}
}

func TestMapControllerToUpjet_NamingDifference(t *testing.T) {
	// Simulates the agent resolving a naming difference
	resp := `{"service_name":"applicationautoscaling","upjet_configs":[{"upjet_service":"autoscaling","file_path":"config/autoscaling/config.go","confidence":0.85}]}`
	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(resp))

	controller := types.ControllerInfo{
		ServiceName: "applicationautoscaling",
		Resources:   []types.ResourceInfo{{Kind: "ScalableTarget"}, {Kind: "ScalingPolicy"}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "autoscaling", FilePath: "config/autoscaling/config.go"},
		{ServiceName: "appautoscaling", FilePath: "config/appautoscaling/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapControllerToUpjet(context.Background(), ag, controller, upjetConfigs, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapControllerToUpjet failed: %v", err)
	}

	if result.ServiceName != "applicationautoscaling" {
		t.Errorf("expected service_name 'applicationautoscaling', got %q", result.ServiceName)
	}
	if len(result.UpjetConfigs) != 1 {
		t.Fatalf("expected 1 upjet config, got %d", len(result.UpjetConfigs))
	}
	if result.UpjetConfigs[0].UpjetService != "autoscaling" {
		t.Errorf("expected upjet_service 'autoscaling', got %q", result.UpjetConfigs[0].UpjetService)
	}
}

func TestMapAllControllersToUpjet_Success(t *testing.T) {
	resp1 := `{"service_name":"s3","upjet_configs":[{"upjet_service":"s3","file_path":"config/s3/config.go","confidence":0.95}]}`
	resp2 := `{"service_name":"iam","upjet_configs":[{"upjet_service":"iam","file_path":"config/iam/config.go","confidence":0.9}]}`
	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(resp1), makeUpjetFinalTextResponse(resp2))

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
		{ServiceName: "iam", Resources: []types.ResourceInfo{{Kind: "Role"}}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
		{ServiceName: "iam", FilePath: "config/iam/config.go"},
		{ServiceName: "ec2", FilePath: "config/ec2/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapAllControllersToUpjet(context.Background(), ag, controllers, upjetConfigs, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAllControllersToUpjet failed: %v", err)
	}

	if len(result.Mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(result.Mappings))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestMapAllControllersToUpjet_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	resp := `{"service_name":"s3","upjet_configs":[{"upjet_service":"s3","file_path":"config/s3/config.go","confidence":0.95}]}`
	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(resp))

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	// First call — agent responds and result is cached
	result, err := MapAllControllersToUpjet(context.Background(), ag, controllers, upjetConfigs, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(result.Mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(result.Mappings))
	}

	// Second call — should use cache (agent with no responses will error if called)
	ag2 := newUpjetMockAgent(t) // no responses
	result2, err := MapAllControllersToUpjet(context.Background(), ag2, controllers, upjetConfigs, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("second call failed (should have used cache): %v", err)
	}
	if len(result2.Mappings) != 1 {
		t.Fatalf("expected 1 cached mapping, got %d", len(result2.Mappings))
	}
	if result2.Mappings[0].ServiceName != "s3" {
		t.Errorf("cached result has wrong service name: %q", result2.Mappings[0].ServiceName)
	}
}

func TestMapAllControllersToUpjet_EmptyControllers(t *testing.T) {
	ag := newUpjetMockAgent(t)

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapAllControllersToUpjet(context.Background(), ag, nil, upjetConfigs, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAllControllersToUpjet failed: %v", err)
	}
	if len(result.Mappings) != 0 {
		t.Errorf("expected 0 mappings for empty controllers, got %d", len(result.Mappings))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestMapAllControllersToUpjet_SkipsOnInvalidJSON(t *testing.T) {
	// Agent returns invalid JSON for one controller, valid for another
	ag := newUpjetMockAgent(t,
		makeUpjetFinalTextResponse(`{"service_name":"s3","upjet_configs":[{"upjet_service":"s3","file_path":"config/s3/config.go","confidence":0.9}]}`),
		// For the "bad" controller, validator will reject this, then retry.
		// After retries fail (no more mock responses), it will be skipped.
		makeUpjetFinalTextResponse(`not valid json`),
		makeUpjetFinalTextResponse(`not valid json`),
		makeUpjetFinalTextResponse(`not valid json`),
	)

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
		{ServiceName: "bad", Resources: []types.ResourceInfo{{Kind: "Thing"}}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapAllControllersToUpjet(context.Background(), ag, controllers, upjetConfigs, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAllControllersToUpjet failed: %v", err)
	}

	// s3 should succeed, bad should be skipped
	if len(result.Mappings) < 1 {
		t.Fatalf("expected at least 1 mapping, got %d", len(result.Mappings))
	}
	if len(result.Skipped) < 1 {
		t.Fatalf("expected at least 1 skipped, got %d", len(result.Skipped))
	}
}

func TestMapControllerToUpjet_MultipleUpjetConfigs(t *testing.T) {
	// A controller that maps to multiple Upjet configs
	resp := `{"service_name":"ec2","upjet_configs":[{"upjet_service":"ec2","file_path":"config/ec2/config.go","confidence":0.95},{"upjet_service":"vpc","file_path":"config/vpc/config.go","confidence":0.7}]}`
	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(resp))

	controller := types.ControllerInfo{
		ServiceName: "ec2",
		Resources: []types.ResourceInfo{
			{Kind: "Instance"},
			{Kind: "VPC"},
			{Kind: "Subnet"},
		},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "ec2", FilePath: "config/ec2/config.go"},
		{ServiceName: "vpc", FilePath: "config/vpc/config.go"},
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	result, err := MapControllerToUpjet(context.Background(), ag, controller, upjetConfigs, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapControllerToUpjet failed: %v", err)
	}

	if result.ServiceName != "ec2" {
		t.Errorf("expected service_name 'ec2', got %q", result.ServiceName)
	}
	if len(result.UpjetConfigs) != 2 {
		t.Fatalf("expected 2 upjet configs, got %d", len(result.UpjetConfigs))
	}
}

func TestMapAllControllersToUpjet_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ag := newUpjetMockAgent(t, makeUpjetFinalTextResponse(`{}`))

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"service_name"}}

	_, err := MapAllControllersToUpjet(ctx, ag, controllers, upjetConfigs, nil, validator, 1, logger.Nop())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestBuildMapUpjetPrompt_ContainsRequiredInfo(t *testing.T) {
	controller := types.ControllerInfo{
		ServiceName: "elasticache",
		Resources: []types.ResourceInfo{
			{Kind: "CacheCluster"},
			{Kind: "ReplicationGroup"},
		},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "elasticache", FilePath: "config/elasticache/config.go"},
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
	}

	prompt := buildMapUpjetPrompt(controller, upjetConfigs)

	// Verify prompt contains the controller service name
	if !contains(prompt, "elasticache") {
		t.Error("prompt does not contain controller service name 'elasticache'")
	}

	// Verify prompt contains resource kinds
	if !contains(prompt, "CacheCluster") {
		t.Error("prompt does not contain resource kind 'CacheCluster'")
	}
	if !contains(prompt, "ReplicationGroup") {
		t.Error("prompt does not contain resource kind 'ReplicationGroup'")
	}

	// Verify prompt contains Upjet service names
	if !contains(prompt, "elasticache") {
		t.Error("prompt does not contain upjet service 'elasticache'")
	}
	if !contains(prompt, "s3") {
		t.Error("prompt does not contain upjet service 's3'")
	}

	// Verify prompt contains JSON output schema
	if !contains(prompt, "upjet_configs") {
		t.Error("prompt does not contain JSON schema field 'upjet_configs'")
	}
	if !contains(prompt, "upjet_service") {
		t.Error("prompt does not contain JSON schema field 'upjet_service'")
	}
	if !contains(prompt, "confidence") {
		t.Error("prompt does not contain JSON schema field 'confidence'")
	}
	if !contains(prompt, "no_match_reason") {
		t.Error("prompt does not contain JSON schema field 'no_match_reason'")
	}
}

func TestBuildUpjetInputParams_Structure(t *testing.T) {
	controller := types.ControllerInfo{
		ServiceName: "s3",
		Resources: []types.ResourceInfo{
			{Kind: "Bucket"},
			{Kind: "Object"},
		},
	}

	upjetConfigs := []UpjetConfigInfo{
		{ServiceName: "s3", FilePath: "config/s3/config.go"},
		{ServiceName: "ec2", FilePath: "config/ec2/config.go"},
	}

	params := buildUpjetInputParams(controller, upjetConfigs)

	if params["service_name"] != "s3" {
		t.Errorf("expected service_name 's3', got %v", params["service_name"])
	}

	kinds, ok := params["resource_kinds"].([]string)
	if !ok {
		t.Fatal("resource_kinds is not []string")
	}
	if len(kinds) != 2 || kinds[0] != "Bucket" || kinds[1] != "Object" {
		t.Errorf("unexpected resource_kinds: %v", kinds)
	}

	if params["upjet_config_count"] != 2 {
		t.Errorf("expected upjet_config_count 2, got %v", params["upjet_config_count"])
	}
}

func TestParseUpjetMappingResult_ValidJSON(t *testing.T) {
	input := `{"service_name":"s3","upjet_configs":[{"upjet_service":"s3","file_path":"config/s3/config.go","confidence":0.95}]}`

	result, err := parseUpjetMappingResult(input)
	if err != nil {
		t.Fatalf("parseUpjetMappingResult failed: %v", err)
	}

	if result.ServiceName != "s3" {
		t.Errorf("expected service_name 's3', got %q", result.ServiceName)
	}
	if len(result.UpjetConfigs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(result.UpjetConfigs))
	}
}

func TestParseUpjetMappingResult_InvalidJSON(t *testing.T) {
	_, err := parseUpjetMappingResult("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseUpjetMappingResult_EmptyConfigs(t *testing.T) {
	input := `{"service_name":"pipes","upjet_configs":[],"no_match_reason":"No match found"}`

	result, err := parseUpjetMappingResult(input)
	if err != nil {
		t.Fatalf("parseUpjetMappingResult failed: %v", err)
	}

	if result.ServiceName != "pipes" {
		t.Errorf("expected service_name 'pipes', got %q", result.ServiceName)
	}
	if len(result.UpjetConfigs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(result.UpjetConfigs))
	}
	if result.NoMatchReason == "" {
		t.Error("expected non-empty no_match_reason")
	}
}

func TestUpjetMapping_JSONRoundTrip(t *testing.T) {
	original := UpjetMapping{
		ServiceName: "elasticache",
		UpjetConfigs: []UpjetMappingEntry{
			{UpjetService: "elasticache", FilePath: "config/elasticache/config.go", Confidence: 0.95},
			{UpjetService: "memorydb", FilePath: "config/memorydb/config.go", Confidence: 0.6},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded UpjetMapping
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ServiceName != original.ServiceName {
		t.Errorf("service_name mismatch: %q vs %q", decoded.ServiceName, original.ServiceName)
	}
	if len(decoded.UpjetConfigs) != len(original.UpjetConfigs) {
		t.Fatalf("upjet_configs length mismatch: %d vs %d", len(decoded.UpjetConfigs), len(original.UpjetConfigs))
	}
	for i, entry := range decoded.UpjetConfigs {
		if entry.UpjetService != original.UpjetConfigs[i].UpjetService {
			t.Errorf("entry %d upjet_service mismatch", i)
		}
		if entry.FilePath != original.UpjetConfigs[i].FilePath {
			t.Errorf("entry %d file_path mismatch", i)
		}
		if entry.Confidence != original.UpjetConfigs[i].Confidence {
			t.Errorf("entry %d confidence mismatch", i)
		}
	}
}

