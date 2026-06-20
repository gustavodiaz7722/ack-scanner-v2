package framework

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

// --- Mock Bedrock Client ---

// mockBedrockClient implements agent.BedrockClient for unit testing.
type mockBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeFinalTextResponse(text string) *bedrockruntime.ConverseOutput {
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

// newMockAgent creates an agent with a mock client that returns the given responses.
func newMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// newMockAgentWithErrors creates an agent with a mock client that returns errors for some calls.
func newMockAgentWithErrors(t *testing.T, responses []*bedrockruntime.ConverseOutput, errors []error) *agent.Agent {
	t.Helper()
	client := &mockBedrockClient{responses: responses, errors: errors}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Test types ---

type testTarget struct {
	Name string `json:"name"`
}

type testMappingResult struct {
	ServiceName string   `json:"service_name"`
	Targets     []string `json:"targets"`
}

// --- Tests ---

func TestMapAll_Success(t *testing.T) {
	// The mock agent returns responses based on a shared atomic counter.
	// With concurrent goroutines, we can't guarantee which controller gets which response.
	// Instead, we make the response deterministic by having BuildPrompt control
	// the "input" to the mock agent, and use a mock that returns per-controller
	// responses based on the prompt content. Since that's complex with our simple mock,
	// we instead test with a single controller per call to avoid ordering issues.

	resp := `{"service_name":"s3","targets":["bucket","object"]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
	}

	targets := []testTarget{
		{Name: "target1"},
		{Name: "target2"},
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return fmt.Sprintf("Map controller %s", controller.ServiceName)
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MapAll(context.Background(), config, ag, controllers, targets, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll failed: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
	}

	s3Result := result.Results["s3"]
	if s3Result.ServiceName != "s3" {
		t.Errorf("expected service_name s3, got %q", s3Result.ServiceName)
	}
	if len(s3Result.Targets) != 2 || s3Result.Targets[0] != "bucket" {
		t.Errorf("unexpected targets for s3: %v", s3Result.Targets)
	}
}

func TestMapAll_MultipleControllers(t *testing.T) {
	// Test multiple controllers: since concurrent goroutines may consume
	// mock responses in any order, we use identical responses and verify counts.
	resp := `{"service_name":"generic","targets":["t1"]}`
	ag := newMockAgent(t,
		makeFinalTextResponse(resp),
		makeFinalTextResponse(resp),
	)

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
		{ServiceName: "iam", Resources: []types.ResourceInfo{{Kind: "Role"}}},
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return fmt.Sprintf("Map controller %s", controller.ServiceName)
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MapAll(context.Background(), config, ag, controllers, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
	}

	// Both results should be present (keyed by their ItemKey)
	if _, ok := result.Results["s3"]; !ok {
		t.Error("expected result for 's3'")
	}
	if _, ok := result.Results["iam"]; !ok {
		t.Error("expected result for 'iam'")
	}
}

func TestMapAll_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	// First call: agent responds
	resp := `{"service_name":"s3","targets":["bucket"]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MapAll(context.Background(), config, ag, controllers, nil, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll first call failed: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	// Second call: should use cache (agent with no responses will error if called)
	ag2 := newMockAgent(t) // no responses available
	result2, err := MapAll(context.Background(), config, ag2, controllers, nil, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll second call failed (should have used cache): %v", err)
	}
	if len(result2.Results) != 1 {
		t.Fatalf("expected 1 cached result, got %d", len(result2.Results))
	}
	if result2.Results["s3"].ServiceName != "s3" {
		t.Errorf("cached result has wrong service name")
	}
}

func TestMapAll_ParallelExecution(t *testing.T) {
	// Create responses for 4 controllers
	responses := make([]*bedrockruntime.ConverseOutput, 4)
	for i := range 4 {
		resp := fmt.Sprintf(`{"service_name":"svc%d","targets":["t%d"]}`, i, i)
		responses[i] = makeFinalTextResponse(resp)
	}

	ag := newMockAgent(t, responses...)

	controllers := make([]types.ControllerInfo, 4)
	for i := range 4 {
		controllers[i] = types.ControllerInfo{
			ServiceName: fmt.Sprintf("svc%d", i),
			Resources:   []types.ResourceInfo{{Kind: "Resource"}},
		}
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return fmt.Sprintf("Map %s", controller.ServiceName)
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	validator := &agent.JSONValidator{}

	// Run with maxParallel=2
	result, err := MapAll(context.Background(), config, ag, controllers, nil, nil, validator, 2, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll parallel failed: %v", err)
	}

	if len(result.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result.Results))
	}
}

func TestMapAll_SkipsOnParseError(t *testing.T) {
	// Agent returns invalid JSON for one controller
	ag := newMockAgent(t,
		makeFinalTextResponse(`{"service_name":"s3","targets":["bucket"]}`),
		makeFinalTextResponse(`not valid json at all`),
	)

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
		{ServiceName: "bad", Resources: []types.ResourceInfo{{Kind: "Thing"}}},
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	// Use a validator that accepts anything (the parse step will catch the error)
	validator := &agent.JSONValidator{}

	result, err := MapAll(context.Background(), config, ag, controllers, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll failed: %v", err)
	}

	// "bad" should be skipped because the validator rejects it, and after retries it's skipped
	// Actually, with JSONValidator and the response "not valid json at all",
	// validation will fail and trigger retries. After 3 attempts (all using same mock),
	// it will return ErrSkipItem.
	// But our mock only has 2 responses total, so the retry will fail with "no more responses"
	// Let's adjust: the controller "bad" will be skipped either way.
	if len(result.Results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(result.Results))
	}
	if len(result.Skipped) < 1 {
		t.Fatalf("expected at least 1 skipped, got %d", len(result.Skipped))
	}
}

func TestMapAll_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ag := newMockAgent(t, makeFinalTextResponse(`{}`))

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	validator := &agent.JSONValidator{}

	_, err := MapAll(ctx, config, ag, controllers, nil, nil, validator, 1, logger.Nop())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestMapOne_Success(t *testing.T) {
	resp := `{"service_name":"s3","targets":["bucket","object"]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	controller := types.ControllerInfo{
		ServiceName: "s3",
		Resources:   []types.ResourceInfo{{Kind: "Bucket"}},
	}

	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{"service": controller.ServiceName}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MapOne(context.Background(), config, ag, controller, nil, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapOne failed: %v", err)
	}

	if result.ServiceName != "s3" {
		t.Errorf("expected service_name s3, got %q", result.ServiceName)
	}
}

func TestMapAll_EmptyControllers(t *testing.T) {
	ag := newMockAgent(t)
	config := MappingConfig[testTarget, testMappingResult]{
		ToolName: "test_map",
		BuildPrompt: func(controller types.ControllerInfo, targets []testTarget) string {
			return "prompt"
		},
		ParseResult: func(response string) (testMappingResult, error) {
			var r testMappingResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: func(controller types.ControllerInfo, targets []testTarget) map[string]any {
			return map[string]any{}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := MapAll(context.Background(), config, ag, nil, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAll failed: %v", err)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty controllers, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped for empty controllers, got %d", len(result.Skipped))
	}
}
