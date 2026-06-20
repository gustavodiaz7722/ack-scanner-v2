package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// mockMapModelsClient implements agent.BedrockClient for map_models tests.
type mockMapModelsClient struct {
	responseFunc func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error)
	callIdx      atomic.Int32
}

func (m *mockMapModelsClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	return m.responseFunc(idx, params)
}

// makeTextResponse creates a Bedrock response containing the given text.
func makeTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func TestMapControllerToModel_SimpleMatch(t *testing.T) {
	// Test a straightforward 1:1 mapping where the service name matches directly
	controller := types.ControllerInfo{
		ServiceName: "elasticache",
		RepoName:    "elasticache-controller",
		Resources: []types.ResourceInfo{
			{Kind: "CacheCluster"},
			{Kind: "ReplicationGroup"},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "elasticache", FilePath: "codegen/sdk-codegen/aws-models/elasticache.json"},
		{ServiceName: "ec2", FilePath: "codegen/sdk-codegen/aws-models/ec2.json"},
		{ServiceName: "s3", FilePath: "codegen/sdk-codegen/aws-models/s3.json"},
	}

	expectedResponse := `{"mapping":{"service_name":"elasticache","model_file":"codegen/sdk-codegen/aws-models/elasticache.json","confidence":1.0}}`

	client := &mockMapModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeTextResponse(expectedResponse), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapControllerToModel(context.Background(), ag, controller, models, nil, validator)
	if err != nil {
		t.Fatalf("MapControllerToModel failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if result.ModelFile != "codegen/sdk-codegen/aws-models/elasticache.json" {
		t.Errorf("expected model_file 'codegen/sdk-codegen/aws-models/elasticache.json', got %q", result.ModelFile)
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", result.Confidence)
	}
	if result.NoMatchReason != "" {
		t.Errorf("expected empty no_match_reason, got %q", result.NoMatchReason)
	}
}

func TestMapControllerToModel_NamingDifference(t *testing.T) {
	// Test that the agent resolves naming differences
	controller := types.ControllerInfo{
		ServiceName: "applicationautoscaling",
		RepoName:    "applicationautoscaling-controller",
		Resources: []types.ResourceInfo{
			{Kind: "ScalableTarget"},
			{Kind: "ScalingPolicy"},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "application-auto-scaling", FilePath: "codegen/sdk-codegen/aws-models/application-auto-scaling.json"},
		{ServiceName: "auto-scaling", FilePath: "codegen/sdk-codegen/aws-models/auto-scaling.json"},
		{ServiceName: "ec2", FilePath: "codegen/sdk-codegen/aws-models/ec2.json"},
	}

	expectedResponse := `{"mapping":{"service_name":"applicationautoscaling","model_file":"codegen/sdk-codegen/aws-models/application-auto-scaling.json","confidence":0.95}}`

	client := &mockMapModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeTextResponse(expectedResponse), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapControllerToModel(context.Background(), ag, controller, models, nil, validator)
	if err != nil {
		t.Fatalf("MapControllerToModel failed: %v", err)
	}

	if result.ServiceName != "applicationautoscaling" {
		t.Errorf("expected service_name 'applicationautoscaling', got %q", result.ServiceName)
	}
	if result.ModelFile != "codegen/sdk-codegen/aws-models/application-auto-scaling.json" {
		t.Errorf("expected model_file for application-auto-scaling, got %q", result.ModelFile)
	}
	if result.Confidence < 0.9 {
		t.Errorf("expected high confidence, got %f", result.Confidence)
	}
}

func TestMapControllerToModel_NoMatch(t *testing.T) {
	// Test when no API model matches the controller
	controller := types.ControllerInfo{
		ServiceName: "nonexistent-service",
		RepoName:    "nonexistent-service-controller",
		Resources: []types.ResourceInfo{
			{Kind: "Thing"},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "ec2", FilePath: "codegen/sdk-codegen/aws-models/ec2.json"},
		{ServiceName: "s3", FilePath: "codegen/sdk-codegen/aws-models/s3.json"},
	}

	expectedResponse := `{"mapping":{"service_name":"nonexistent-service","model_file":"","confidence":0.0,"no_match_reason":"No corresponding AWS API model file found for this service"}}`

	client := &mockMapModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			return makeTextResponse(expectedResponse), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapControllerToModel(context.Background(), ag, controller, models, nil, validator)
	if err != nil {
		t.Fatalf("MapControllerToModel failed: %v", err)
	}

	if result.ModelFile != "" {
		t.Errorf("expected empty model_file for no-match, got %q", result.ModelFile)
	}
	if result.NoMatchReason == "" {
		t.Error("expected non-empty no_match_reason when no model matches")
	}
}

func TestMapAllControllersToModels_MultipleControllers(t *testing.T) {
	// Test mapping multiple controllers
	controllers := []types.ControllerInfo{
		{
			ServiceName: "s3",
			RepoName:    "s3-controller",
			Resources:   []types.ResourceInfo{{Kind: "Bucket"}},
		},
		{
			ServiceName: "ec2",
			RepoName:    "ec2-controller",
			Resources:   []types.ResourceInfo{{Kind: "Instance"}, {Kind: "SecurityGroup"}},
		},
		{
			ServiceName: "iam",
			RepoName:    "iam-controller",
			Resources:   []types.ResourceInfo{{Kind: "Role"}, {Kind: "Policy"}},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "s3", FilePath: "codegen/sdk-codegen/aws-models/s3.json"},
		{ServiceName: "ec2", FilePath: "codegen/sdk-codegen/aws-models/ec2.json"},
		{ServiceName: "iam", FilePath: "codegen/sdk-codegen/aws-models/iam.json"},
	}

	responses := map[string]string{
		"s3":  `{"mapping":{"service_name":"s3","model_file":"codegen/sdk-codegen/aws-models/s3.json","confidence":1.0}}`,
		"ec2": `{"mapping":{"service_name":"ec2","model_file":"codegen/sdk-codegen/aws-models/ec2.json","confidence":1.0}}`,
		"iam": `{"mapping":{"service_name":"iam","model_file":"codegen/sdk-codegen/aws-models/iam.json","confidence":1.0}}`,
	}

	client := &mockMapModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			// Parse prompt to detect which controller this is for
			for _, msg := range input.Messages {
				for _, block := range msg.Content {
					if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
						for svc, resp := range responses {
							if strings.Contains(textBlock.Value, "Service Name: "+svc) {
								return makeTextResponse(resp), nil
							}
						}
					}
				}
			}
			return makeTextResponse(`{"mapping":{"service_name":"unknown","model_file":"","confidence":0.0,"no_match_reason":"unknown"}}`), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapAllControllersToModels(context.Background(), ag, controllers, models, nil, validator, 1)
	if err != nil {
		t.Fatalf("MapAllControllersToModels failed: %v", err)
	}

	if len(result.Mappings) != 3 {
		t.Fatalf("expected 3 mappings, got %d", len(result.Mappings))
	}

	// Verify each controller got mapped
	mappingsByService := make(map[string]ModelMapping)
	for _, m := range result.Mappings {
		mappingsByService[m.ServiceName] = m
	}

	for _, svc := range []string{"s3", "ec2", "iam"} {
		m, ok := mappingsByService[svc]
		if !ok {
			t.Errorf("missing mapping for service %q", svc)
			continue
		}
		expectedFile := fmt.Sprintf("codegen/sdk-codegen/aws-models/%s.json", svc)
		if m.ModelFile != expectedFile {
			t.Errorf("service %s: expected model_file %q, got %q", svc, expectedFile, m.ModelFile)
		}
	}

	if len(result.Skipped) != 0 {
		t.Errorf("expected no skipped controllers, got %v", result.Skipped)
	}
}

func TestMapAllControllersToModels_WithSkippedController(t *testing.T) {
	// Test that a controller that fails validation is skipped
	controllers := []types.ControllerInfo{
		{
			ServiceName: "s3",
			RepoName:    "s3-controller",
			Resources:   []types.ResourceInfo{{Kind: "Bucket"}},
		},
		{
			ServiceName: "badservice",
			RepoName:    "badservice-controller",
			Resources:   []types.ResourceInfo{{Kind: "Thing"}},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "s3", FilePath: "codegen/sdk-codegen/aws-models/s3.json"},
	}

	callCount := atomic.Int32{}
	client := &mockMapModelsClient{
		responseFunc: func(idx int, input *bedrockruntime.ConverseInput) (*bedrockruntime.ConverseOutput, error) {
			count := int(callCount.Add(1))
			// First prompt for s3 → valid response
			// For badservice → always return invalid JSON to trigger skip
			for _, msg := range input.Messages {
				for _, block := range msg.Content {
					if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
						if strings.Contains(textBlock.Value, "Service Name: s3") {
							return makeTextResponse(`{"mapping":{"service_name":"s3","model_file":"codegen/sdk-codegen/aws-models/s3.json","confidence":1.0}}`), nil
						}
					}
				}
			}
			// Return invalid JSON for badservice (will fail validation and be skipped after retries)
			_ = count
			return makeTextResponse(`not valid json`), nil
		},
	}

	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapAllControllersToModels(context.Background(), ag, controllers, models, nil, validator, 1)
	if err != nil {
		t.Fatalf("MapAllControllersToModels failed: %v", err)
	}

	// s3 should succeed
	if len(result.Mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(result.Mappings))
	}
	if result.Mappings[0].ServiceName != "s3" {
		t.Errorf("expected first mapping to be s3, got %q", result.Mappings[0].ServiceName)
	}

	// badservice should be skipped
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}
	if result.Skipped[0] != "badservice" {
		t.Errorf("expected skipped controller 'badservice', got %q", result.Skipped[0])
	}
}

func TestBuildMapModelPrompt_ContainsControllerInfo(t *testing.T) {
	controller := types.ControllerInfo{
		ServiceName: "elasticache",
		RepoName:    "elasticache-controller",
		Resources: []types.ResourceInfo{
			{Kind: "CacheCluster"},
			{Kind: "ReplicationGroup"},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "elasticache", FilePath: "codegen/sdk-codegen/aws-models/elasticache.json"},
		{ServiceName: "ec2", FilePath: "codegen/sdk-codegen/aws-models/ec2.json"},
	}

	prompt := buildMapModelPrompt(controller, models)

	// Verify prompt contains controller info
	if !strings.Contains(prompt, "Service Name: elasticache") {
		t.Error("prompt missing controller service name")
	}
	if !strings.Contains(prompt, "CacheCluster") {
		t.Error("prompt missing resource kind CacheCluster")
	}
	if !strings.Contains(prompt, "ReplicationGroup") {
		t.Error("prompt missing resource kind ReplicationGroup")
	}

	// Verify prompt contains model file info
	if !strings.Contains(prompt, "codegen/sdk-codegen/aws-models/elasticache.json") {
		t.Error("prompt missing elasticache model file path")
	}
	if !strings.Contains(prompt, "codegen/sdk-codegen/aws-models/ec2.json") {
		t.Error("prompt missing ec2 model file path")
	}

	// Verify prompt contains instructions about naming differences
	if !strings.Contains(prompt, "applicationautoscaling") {
		t.Error("prompt missing naming difference example")
	}

	// Verify prompt contains JSON output format
	if !strings.Contains(prompt, "model_file") {
		t.Error("prompt missing required output format with model_file")
	}
}

func TestParseMapModelResult_ValidJSON(t *testing.T) {
	input := `{"mapping":{"service_name":"elasticache","model_file":"codegen/sdk-codegen/aws-models/elasticache.json","confidence":0.95,"no_match_reason":""}}`

	result, err := parseMapModelResult(input)
	if err != nil {
		t.Fatalf("parseMapModelResult failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if result.ModelFile != "codegen/sdk-codegen/aws-models/elasticache.json" {
		t.Errorf("expected model_file path, got %q", result.ModelFile)
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.Confidence)
	}
}

func TestParseMapModelResult_InvalidJSON(t *testing.T) {
	_, err := parseMapModelResult("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestBuildMapModelInputParams(t *testing.T) {
	controller := types.ControllerInfo{
		ServiceName: "s3",
		RepoName:    "s3-controller",
		Resources: []types.ResourceInfo{
			{Kind: "Bucket"},
			{Kind: "Object"},
		},
	}

	models := []APIModelInfo{
		{ServiceName: "s3", FilePath: "s3.json"},
		{ServiceName: "ec2", FilePath: "ec2.json"},
		{ServiceName: "iam", FilePath: "iam.json"},
	}

	params := buildMapModelInputParams(controller, models)

	if params["service_name"] != "s3" {
		t.Errorf("expected service_name 's3', got %v", params["service_name"])
	}
	if params["model_count"] != 3 {
		t.Errorf("expected model_count 3, got %v", params["model_count"])
	}
	kinds, ok := params["resource_kinds"].([]string)
	if !ok {
		t.Fatal("expected resource_kinds to be []string")
	}
	if len(kinds) != 2 {
		t.Errorf("expected 2 resource_kinds, got %d", len(kinds))
	}

	// Verify it's JSON-serializable (for cache hashing)
	_, err := json.Marshal(params)
	if err != nil {
		t.Errorf("params not JSON-serializable: %v", err)
	}
}
