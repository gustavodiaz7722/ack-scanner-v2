package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// --- Test types for analysis ---

type testAnalysisResult struct {
	ServiceName string   `json:"service_name"`
	References  []string `json:"references"`
}

// --- Tests ---

func TestAnalyzeAll_Success(t *testing.T) {
	// Single file test to avoid ordering issues with mock client
	resp := `{"service_name":"elasticache","references":["kms_key_id","subnet_group"]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	files := []FileToAnalyze{
		{Key: "elasticache", FilePath: "config/elasticache/config.go", Content: "package elasticache"},
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return fmt.Sprintf("Analyze %s", file.FilePath)
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key, "length": len(file.Content)}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := AnalyzeAll(context.Background(), config, ag, files, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll failed: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
	}

	ecResult := result.Results["elasticache"]
	if ecResult.ServiceName != "elasticache" {
		t.Errorf("expected service_name elasticache, got %q", ecResult.ServiceName)
	}
	if len(ecResult.References) != 2 {
		t.Errorf("expected 2 references, got %d", len(ecResult.References))
	}
}

func TestAnalyzeAll_MultipleFiles(t *testing.T) {
	// With multiple files, use identical responses to avoid ordering issues
	resp := `{"service_name":"generic","references":["ref1"]}`
	ag := newMockAgent(t,
		makeFinalTextResponse(resp),
		makeFinalTextResponse(resp),
	)

	files := []FileToAnalyze{
		{Key: "svc1", FilePath: "config/svc1/config.go", Content: "package svc1"},
		{Key: "svc2", FilePath: "config/svc2/config.go", Content: "package svc2"},
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return fmt.Sprintf("Analyze %s", file.FilePath)
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key, "length": len(file.Content)}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := AnalyzeAll(context.Background(), config, ag, files, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll failed: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	// Both should be present
	if _, ok := result.Results["svc1"]; !ok {
		t.Error("expected result for 'svc1'")
	}
	if _, ok := result.Results["svc2"]; !ok {
		t.Error("expected result for 'svc2'")
	}
}

func TestAnalyzeAll_WithCache(t *testing.T) {
	cacheDir := t.TempDir()
	resultCache, err := cache.NewResultCache(cacheDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	resp := `{"service_name":"s3","references":["bucket_arn"]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	files := []FileToAnalyze{
		{Key: "s3", FilePath: "config/s3/config.go", Content: "package s3"},
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return "prompt"
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key, "length": len(file.Content)}
		},
	}

	validator := &agent.JSONValidator{}

	// First call
	result, err := AnalyzeAll(context.Background(), config, ag, files, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll first call failed: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	// Second call: should use cache
	ag2 := newMockAgent(t) // No responses - would error if called
	result2, err := AnalyzeAll(context.Background(), config, ag2, files, resultCache, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll second call failed (should use cache): %v", err)
	}
	if len(result2.Results) != 1 {
		t.Fatalf("expected 1 cached result, got %d", len(result2.Results))
	}
	if result2.Results["s3"].ServiceName != "s3" {
		t.Errorf("cached result has wrong service name")
	}
}

func TestAnalyzeAll_ParallelExecution(t *testing.T) {
	responses := make([]*bedrockruntime.ConverseOutput, 5)
	for i := range 5 {
		resp := fmt.Sprintf(`{"service_name":"svc%d","references":["ref%d"]}`, i, i)
		responses[i] = makeFinalTextResponse(resp)
	}

	ag := newMockAgent(t, responses...)

	files := make([]FileToAnalyze, 5)
	for i := range 5 {
		files[i] = FileToAnalyze{
			Key:      fmt.Sprintf("svc%d", i),
			FilePath: fmt.Sprintf("config/svc%d/config.go", i),
			Content:  fmt.Sprintf("package svc%d", i),
		}
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return fmt.Sprintf("Analyze %s", file.Key)
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := AnalyzeAll(context.Background(), config, ag, files, nil, validator, 3, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll parallel failed: %v", err)
	}

	if len(result.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(result.Results))
	}
}

func TestAnalyzeAll_SkipsOnAgentError(t *testing.T) {
	// First response succeeds, second triggers validation failure (invalid JSON)
	ag := newMockAgent(t,
		makeFinalTextResponse(`{"service_name":"s3","references":[]}`),
		makeFinalTextResponse(`invalid json`),
		makeFinalTextResponse(`invalid json`), // retry 1
		makeFinalTextResponse(`invalid json`), // retry 2
	)

	files := []FileToAnalyze{
		{Key: "s3", FilePath: "config/s3/config.go", Content: "ok"},
		{Key: "bad", FilePath: "config/bad/config.go", Content: "bad"},
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return "prompt"
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := AnalyzeAll(context.Background(), config, ag, files, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll failed: %v", err)
	}

	if len(result.Results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(result.Results))
	}
	if len(result.Skipped) < 1 {
		t.Fatalf("expected at least 1 skipped, got %d", len(result.Skipped))
	}
}

func TestAnalyzeAll_EmptyFiles(t *testing.T) {
	ag := newMockAgent(t)
	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return "prompt"
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := AnalyzeAll(context.Background(), config, ag, nil, nil, validator, 1, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeAll failed: %v", err)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results for empty files, got %d", len(result.Results))
	}
}

func TestAnalyzeOne_Success(t *testing.T) {
	resp := `{"service_name":"kms","references":["key_id"]}`
	ag := newMockAgent(t, makeFinalTextResponse(resp))

	file := FileToAnalyze{
		Key:      "kms",
		FilePath: "config/kms/config.go",
		Content:  "package kms",
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return "prompt"
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key}
		},
	}

	validator := &agent.JSONValidator{}

	result, err := AnalyzeOne(context.Background(), config, ag, file, nil, validator, logger.Nop())
	if err != nil {
		t.Fatalf("AnalyzeOne failed: %v", err)
	}

	if result.ServiceName != "kms" {
		t.Errorf("expected service_name kms, got %q", result.ServiceName)
	}
	if len(result.References) != 1 || result.References[0] != "key_id" {
		t.Errorf("unexpected references: %v", result.References)
	}
}

func TestAnalyzeAll_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ag := newMockAgent(t, makeFinalTextResponse(`{}`))

	files := []FileToAnalyze{
		{Key: "s3", FilePath: "config/s3/config.go", Content: "ok"},
	}

	config := AnalysisConfig[testAnalysisResult]{
		ToolName: "test_analyze",
		BuildPrompt: func(file FileToAnalyze) string {
			return "prompt"
		},
		ParseResult: func(response string) (testAnalysisResult, error) {
			var r testAnalysisResult
			err := json.Unmarshal([]byte(response), &r)
			return r, err
		},
		InputParams: func(file FileToAnalyze) map[string]any {
			return map[string]any{"key": file.Key}
		},
	}

	validator := &agent.JSONValidator{}

	_, err := AnalyzeAll(ctx, config, ag, files, nil, validator, 1, logger.Nop())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
