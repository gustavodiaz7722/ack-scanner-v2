//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
)

// TestBedrockClient_RealAgent tests that a real Bedrock client can be created
// and that the agent loop works end-to-end with a real model.
// This test requires:
// - ACK_SCANNER_INTEGRATION=1 environment variable
// - Valid AWS credentials with bedrock:InvokeModel permissions
func TestBedrockClient_RealAgent(t *testing.T) {
	if os.Getenv("ACK_SCANNER_INTEGRATION") == "" {
		t.Skip("skipping Bedrock integration test: ACK_SCANNER_INTEGRATION not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a real Bedrock client
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	client, err := agent.NewBedrockClient(ctx, region)
	if err != nil {
		t.Fatalf("NewBedrockClient failed: %v", err)
	}

	// Create an agent with the real client
	modelID := os.Getenv("ACK_SCANNER_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-sonnet-4-20250514-v1:0"
	}

	ag, err := agent.NewAgent(client, modelID, agent.WithMaxTurns(5))
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	// Send a simple prompt and verify the agent produces a response
	result, err := ag.Run(ctx, "Reply with exactly the word 'hello' and nothing else.")
	if err != nil {
		t.Fatalf("Agent.Run failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil AgentResult")
	}
	if result.FinalResponse == "" {
		t.Fatal("expected non-empty FinalResponse from the agent")
	}

	t.Logf("Agent response: %q", result.FinalResponse)
	t.Logf("Total tokens used: %d", result.TotalTokens)

	// Verify token usage was tracked
	if result.TotalTokens <= 0 {
		t.Error("expected positive TotalTokens")
	}
}

// TestBedrockClient_WithToolUse tests that the agent can handle tool-use
// interactions with a real model.
func TestBedrockClient_WithToolUse(t *testing.T) {
	if os.Getenv("ACK_SCANNER_INTEGRATION") == "" {
		t.Skip("skipping Bedrock integration test: ACK_SCANNER_INTEGRATION not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	client, err := agent.NewBedrockClient(ctx, region)
	if err != nil {
		t.Fatalf("NewBedrockClient failed: %v", err)
	}

	modelID := os.Getenv("ACK_SCANNER_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-sonnet-4-20250514-v1:0"
	}

	// Register a simple echo tool
	registry := agent.NewToolRegistry()
	registry.Register("echo", func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return input, nil
	})

	toolDefs := []agent.ToolDefinition{
		{
			Name:        "echo",
			Description: "Echoes back the input as-is. Use this tool to confirm communication works.",
			InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string","description":"Message to echo"}},"required":["message"]}`),
		},
	}

	ag, err := agent.NewAgent(client, modelID,
		agent.WithMaxTurns(10),
		agent.WithTools(toolDefs),
		agent.WithRegistry(registry),
	)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	result, err := ag.Run(ctx, `Use the echo tool with the message "integration-test-ok", then respond with "done".`)
	if err != nil {
		t.Fatalf("Agent.Run with tool failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil AgentResult")
	}

	// The agent should have made at least one tool call
	if len(result.ToolCalls) == 0 {
		t.Error("expected at least one tool call, got 0")
	} else {
		t.Logf("Agent made %d tool call(s)", len(result.ToolCalls))
		for i, tc := range result.ToolCalls {
			t.Logf("  Tool call %d: %s (input: %s)", i+1, tc.Name, string(tc.Input))
		}
	}

	if result.FinalResponse == "" {
		t.Error("expected non-empty FinalResponse")
	}
	t.Logf("Final response: %q", result.FinalResponse)
}
