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

// --- Mock Bedrock Client for map_terraform_refs tests ---

type mockTFRefsBedrockClient struct {
	responses []*bedrockruntime.ConverseOutput
	errors    []error
	callIdx   atomic.Int32
}

func (m *mockTFRefsBedrockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx)
	}
	if m.errors != nil && idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	return m.responses[idx], nil
}

func makeTFRefsFinalTextResponse(text string) *bedrockruntime.ConverseOutput {
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

func newTFRefsMockAgent(t *testing.T, responses ...*bedrockruntime.ConverseOutput) *agent.Agent {
	t.Helper()
	client := &mockTFRefsBedrockClient{responses: responses}
	ag, err := agent.NewAgent(client, "test-model")
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return ag
}

// --- Unit Tests ---

func TestMapControllerToTerraformRefs_Success(t *testing.T) {
	expectedMapping := TerraformRefMapping{
		ServiceName: "elasticache",
		TFDocFiles: []TerraformRefMappingEntry{
			{
				TFResourceType: "aws_elasticache_cluster",
				DocFilePath:    "website/docs/r/elasticache_cluster.html.markdown",
				Confidence:     0.95,
			},
			{
				TFResourceType: "aws_elasticache_replication_group",
				DocFilePath:    "website/docs/r/elasticache_replication_group.html.markdown",
				Confidence:     0.9,
			},
		},
	}

	responseJSON, _ := json.Marshal(MapTerraformRefsOutput{Mapping: expectedMapping})
	ag := newTFRefsMockAgent(t, makeTFRefsFinalTextResponse(string(responseJSON)))

	controller := types.ControllerInfo{
		ServiceName: "elasticache",
		RepoName:    "elasticache-controller",
		Resources: []types.ResourceInfo{
			{Kind: "CacheCluster"},
			{Kind: "ReplicationGroup"},
		},
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "website/docs/r/elasticache_cluster.html.markdown"},
		{DocFilePath: "website/docs/r/elasticache_replication_group.html.markdown"},
		{DocFilePath: "website/docs/r/instance.html.markdown"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapControllerToTerraformRefs(context.Background(), ag, controller, tfResources, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapControllerToTerraformRefs failed: %v", err)
	}

	if result.ServiceName != "elasticache" {
		t.Errorf("expected service_name 'elasticache', got %q", result.ServiceName)
	}
	if len(result.TFDocFiles) != 2 {
		t.Fatalf("expected 2 TF doc files, got %d", len(result.TFDocFiles))
	}
	if result.TFDocFiles[0].TFResourceType != "aws_elasticache_cluster" {
		t.Errorf("unexpected first TF resource type: %q", result.TFDocFiles[0].TFResourceType)
	}
	if result.TFDocFiles[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", result.TFDocFiles[0].Confidence)
	}
}

func TestMapControllerToTerraformRefs_NoMatch(t *testing.T) {
	expectedMapping := TerraformRefMapping{
		ServiceName:   "customservice",
		TFDocFiles:    []TerraformRefMappingEntry{},
		NoMatchReason: "No corresponding Terraform resources found for this custom service",
	}

	responseJSON, _ := json.Marshal(MapTerraformRefsOutput{Mapping: expectedMapping})
	ag := newTFRefsMockAgent(t, makeTFRefsFinalTextResponse(string(responseJSON)))

	controller := types.ControllerInfo{
		ServiceName: "customservice",
		RepoName:    "customservice-controller",
		Resources:   []types.ResourceInfo{{Kind: "CustomResource"}},
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "website/docs/r/instance.html.markdown"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapControllerToTerraformRefs(context.Background(), ag, controller, tfResources, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("MapControllerToTerraformRefs failed: %v", err)
	}

	if result.ServiceName != "customservice" {
		t.Errorf("expected service_name 'customservice', got %q", result.ServiceName)
	}
	if len(result.TFDocFiles) != 0 {
		t.Errorf("expected 0 TF doc files, got %d", len(result.TFDocFiles))
	}
	if result.NoMatchReason == "" {
		t.Error("expected a no_match_reason for unmatched controller")
	}
}

func TestMapControllerToTerraformRefs_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	expectedMapping := TerraformRefMapping{
		ServiceName: "s3",
		TFDocFiles: []TerraformRefMappingEntry{
			{TFResourceType: "aws_s3_bucket", DocFilePath: "website/docs/r/s3_bucket.html.markdown", Confidence: 0.95},
		},
	}

	responseJSON, _ := json.Marshal(MapTerraformRefsOutput{Mapping: expectedMapping})
	ag := newTFRefsMockAgent(t, makeTFRefsFinalTextResponse(string(responseJSON)))

	controller := types.ControllerInfo{
		ServiceName: "s3",
		RepoName:    "s3-controller",
		Resources:   []types.ResourceInfo{{Kind: "Bucket"}},
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "website/docs/r/s3_bucket.html.markdown"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	// First call — should hit agent
	result, err := MapControllerToTerraformRefs(context.Background(), ag, controller, tfResources, resultCache, validator, logger.Nop())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if result.ServiceName != "s3" {
		t.Errorf("expected 's3', got %q", result.ServiceName)
	}

	// Second call — should use cache (agent with no responses will error if called)
	ag2 := newTFRefsMockAgent(t) // no responses available
	result2, err := MapControllerToTerraformRefs(context.Background(), ag2, controller, tfResources, resultCache, validator, logger.Nop())
	if err != nil {
		t.Fatalf("second call failed (should have used cache): %v", err)
	}
	if result2.ServiceName != "s3" {
		t.Errorf("cached result has wrong service_name: %q", result2.ServiceName)
	}
	if len(result2.TFDocFiles) != 1 {
		t.Errorf("cached result has wrong number of TF doc files: %d", len(result2.TFDocFiles))
	}
}

func TestMapAllControllersToTerraformRefs_Success(t *testing.T) {
	// Create responses for 2 controllers
	mapping1 := TerraformRefMapping{
		ServiceName: "s3",
		TFDocFiles:  []TerraformRefMappingEntry{{TFResourceType: "aws_s3_bucket", DocFilePath: "website/docs/r/s3_bucket.html.markdown", Confidence: 0.95}},
	}
	mapping2 := TerraformRefMapping{
		ServiceName: "iam",
		TFDocFiles:  []TerraformRefMappingEntry{{TFResourceType: "aws_iam_role", DocFilePath: "website/docs/r/iam_role.html.markdown", Confidence: 0.9}},
	}

	resp1, _ := json.Marshal(MapTerraformRefsOutput{Mapping: mapping1})
	resp2, _ := json.Marshal(MapTerraformRefsOutput{Mapping: mapping2})

	ag := newTFRefsMockAgent(t,
		makeTFRefsFinalTextResponse(string(resp1)),
		makeTFRefsFinalTextResponse(string(resp2)),
	)

	controllers := []types.ControllerInfo{
		{ServiceName: "s3", RepoName: "s3-controller", Resources: []types.ResourceInfo{{Kind: "Bucket"}}},
		{ServiceName: "iam", RepoName: "iam-controller", Resources: []types.ResourceInfo{{Kind: "Role"}}},
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "website/docs/r/s3_bucket.html.markdown"},
		{DocFilePath: "website/docs/r/iam_role.html.markdown"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapAllControllersToTerraformRefs(context.Background(), ag, controllers, tfResources, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAllControllersToTerraformRefs failed: %v", err)
	}

	if len(result.Mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(result.Mappings))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}
}

func TestMapAllControllersToTerraformRefs_EmptyControllers(t *testing.T) {
	ag := newTFRefsMockAgent(t) // no responses needed

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapAllControllersToTerraformRefs(context.Background(), ag, nil, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("MapAllControllersToTerraformRefs failed: %v", err)
	}

	if len(result.Mappings) != 0 {
		t.Errorf("expected 0 mappings for empty controllers, got %d", len(result.Mappings))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped for empty controllers, got %d", len(result.Skipped))
	}
}

func TestMapAllControllersToTerraformRefs_Parallel(t *testing.T) {
	// Create responses for 4 controllers
	responses := make([]*bedrockruntime.ConverseOutput, 4)
	for i := range 4 {
		mapping := TerraformRefMapping{
			ServiceName: fmt.Sprintf("svc%d", i),
			TFDocFiles:  []TerraformRefMappingEntry{{TFResourceType: fmt.Sprintf("aws_svc%d_resource", i), DocFilePath: fmt.Sprintf("website/docs/r/svc%d_resource.html.markdown", i), Confidence: 0.9}},
		}
		resp, _ := json.Marshal(MapTerraformRefsOutput{Mapping: mapping})
		responses[i] = makeTFRefsFinalTextResponse(string(resp))
	}

	ag := newTFRefsMockAgent(t, responses...)

	controllers := make([]types.ControllerInfo, 4)
	for i := range 4 {
		controllers[i] = types.ControllerInfo{
			ServiceName: fmt.Sprintf("svc%d", i),
			Resources:   []types.ResourceInfo{{Kind: "Resource"}},
		}
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "website/docs/r/svc0_resource.html.markdown"},
	}

	validator := &agent.JSONValidator{RequiredFields: []string{"mapping"}}

	result, err := MapAllControllersToTerraformRefs(context.Background(), ag, controllers, tfResources, nil, validator, 2, logger.Nop())
	if err != nil {
		t.Fatalf("MapAllControllersToTerraformRefs parallel failed: %v", err)
	}

	if len(result.Mappings) != 4 {
		t.Fatalf("expected 4 mappings, got %d", len(result.Mappings))
	}
}

func TestBuildMapTerraformRefsPrompt_ContainsReferenceContext(t *testing.T) {
	controller := types.ControllerInfo{
		ServiceName: "elasticache",
		Resources: []types.ResourceInfo{
			{Kind: "CacheCluster"},
			{Kind: "ReplicationGroup"},
		},
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "website/docs/r/elasticache_cluster.html.markdown"},
		{DocFilePath: "website/docs/r/instance.html.markdown"},
	}

	prompt := buildMapTerraformRefsPrompt(controller, tfResources)

	// Verify the prompt contains reference-specific context
	expectedPhrases := []string{
		"cross-resource reference patterns",
		"elasticache",
		"CacheCluster",
		"ReplicationGroup",
		"website/docs/r/elasticache_cluster.html.markdown",
		"HCL examples",
		"field = aws_other_resource.name.id",
		"_arn",
		"_id",
		"_name",
		`"mapping"`,
	}

	for _, phrase := range expectedPhrases {
		if !contains(prompt, phrase) {
			t.Errorf("prompt missing expected phrase: %q", phrase)
		}
	}
}

func TestBuildMapTerraformRefsInputParams(t *testing.T) {
	controller := types.ControllerInfo{
		ServiceName: "s3",
		Resources: []types.ResourceInfo{
			{Kind: "Bucket"},
			{Kind: "Object"},
		},
	}

	tfResources := []types.TerraformResourceInfo{
		{DocFilePath: "a.md"},
		{DocFilePath: "b.md"},
		{DocFilePath: "c.md"},
	}

	params := buildMapTerraformRefsInputParams(controller, tfResources)

	if params["service_name"] != "s3" {
		t.Errorf("expected service_name 's3', got %v", params["service_name"])
	}
	if params["tf_doc_count"] != 3 {
		t.Errorf("expected tf_doc_count 3, got %v", params["tf_doc_count"])
	}

	kinds, ok := params["resource_kinds"].([]string)
	if !ok {
		t.Fatal("resource_kinds is not []string")
	}
	if len(kinds) != 2 || kinds[0] != "Bucket" || kinds[1] != "Object" {
		t.Errorf("unexpected resource_kinds: %v", kinds)
	}
}

func TestTerraformRefsMappingConfig_ToolName(t *testing.T) {
	config := TerraformRefsMappingConfig()
	if config.ToolName != "map_terraform_refs" {
		t.Errorf("expected ToolName 'map_terraform_refs', got %q", config.ToolName)
	}
}

func TestTerraformRefsMappingConfig_ItemKey(t *testing.T) {
	config := TerraformRefsMappingConfig()
	controller := types.ControllerInfo{ServiceName: "elasticache"}
	key := config.ItemKey(controller)
	if key != "elasticache" {
		t.Errorf("expected key 'elasticache', got %q", key)
	}
}

func TestParseTerraformRefsResponse_ValidJSON(t *testing.T) {
	input := `{"mapping":{"service_name":"s3","terraform_doc_files":[{"terraform_resource_type":"aws_s3_bucket","doc_file_path":"website/docs/r/s3_bucket.html.markdown","confidence":0.95}]}}`

	result, err := parseTerraformRefsResponse(input)
	if err != nil {
		t.Fatalf("parseTerraformRefsResponse failed: %v", err)
	}

	if result.ServiceName != "s3" {
		t.Errorf("expected service_name 's3', got %q", result.ServiceName)
	}
	if len(result.TFDocFiles) != 1 {
		t.Fatalf("expected 1 TF doc file, got %d", len(result.TFDocFiles))
	}
	if result.TFDocFiles[0].TFResourceType != "aws_s3_bucket" {
		t.Errorf("unexpected TF resource type: %q", result.TFDocFiles[0].TFResourceType)
	}
}

func TestParseTerraformRefsResponse_InvalidJSON(t *testing.T) {
	_, err := parseTerraformRefsResponse("not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
