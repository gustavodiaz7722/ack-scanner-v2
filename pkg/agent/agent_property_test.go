package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"pgregory.net/rapid"
)

// mockBedrockClient implements BedrockClient for testing.
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

// makeToolUseResponse creates a Converse response with tool use blocks.
func makeToolUseResponse(toolName, toolUseID string, inputJSON map[string]interface{}) *bedrockruntime.ConverseOutput {
	name := toolName
	id := toolUseID
	tokens := int32(100)
	return &bedrockruntime.ConverseOutput{
		StopReason: brtypes.StopReasonToolUse,
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberToolUse{
						Value: brtypes.ToolUseBlock{
							Name:      &name,
							ToolUseId: &id,
							Input:     document.NewLazyDocument(inputJSON),
						},
					},
				},
			},
		},
		Usage: &brtypes.TokenUsage{TotalTokens: &tokens},
	}
}

// makeFinalTextResponse creates a Converse response with final text.
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

// Property 15: Agent loop termination
// **Validates: Requirements 8.3**
//
// For any sequence of Bedrock responses consisting of zero or more toolUse
// responses followed by exactly one final text response, the agent loop SHALL
// terminate with the final text response — never hanging or looping indefinitely.
func TestProperty15_AgentLoopTermination(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of tool-use turns (0 to 15)
		numToolUseTurns := rapid.IntRange(0, 15).Draw(t, "numToolUseTurns")

		// Generate the final text response
		finalText := rapid.StringMatching(`[a-zA-Z0-9 ]{1,100}`).Draw(t, "finalText")

		// Build mock responses: N tool-use responses + 1 final text response
		responses := make([]*bedrockruntime.ConverseOutput, 0, numToolUseTurns+1)
		for i := 0; i < numToolUseTurns; i++ {
			toolName := fmt.Sprintf("tool_%d", i)
			toolID := fmt.Sprintf("id_%d", i)
			responses = append(responses, makeToolUseResponse(toolName, toolID, map[string]interface{}{"key": "value"}))
		}
		responses = append(responses, makeFinalTextResponse(finalText))

		client := &mockBedrockClient{responses: responses}

		// Register a dummy tool for each tool call
		registry := NewToolRegistry()
		for i := 0; i < numToolUseTurns; i++ {
			toolName := fmt.Sprintf("tool_%d", i)
			registry.Register(toolName, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"result": "ok"}`), nil
			})
		}

		agent, err := NewAgent(client, "test-model",
			WithTools([]ToolDefinition{{Name: "dummy", Description: "dummy", InputSchema: []byte(`{"type":"object"}`)}}),
			WithRegistry(registry),
			WithMaxTurns(numToolUseTurns+5), // Give enough headroom
		)
		if err != nil {
			t.Fatalf("NewAgent failed: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := agent.Run(ctx, "test prompt")
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

		// Verify the agent terminated with the correct final text
		if result.FinalResponse != finalText {
			t.Fatalf("expected final response %q, got %q", finalText, result.FinalResponse)
		}

		// Verify we got exactly the right number of tool calls
		if len(result.ToolCalls) != numToolUseTurns {
			t.Fatalf("expected %d tool calls, got %d", numToolUseTurns, len(result.ToolCalls))
		}

		// Verify tool calls were executed in order
		for i, tc := range result.ToolCalls {
			expectedName := fmt.Sprintf("tool_%d", i)
			if tc.Name != expectedName {
				t.Fatalf("tool call %d: expected name %q, got %q", i, expectedName, tc.Name)
			}
		}
	})
}

// Property 16: Exponential backoff retry
// **Validates: Requirements 8.7**
//
// For any number of consecutive throttling errors N (where N ≤ max retries),
// followed by a successful response, the agent SHALL retry exactly N times with
// increasing delays and ultimately return the successful response. If N exceeds
// max retries, it SHALL return an error.
func TestProperty16_ExponentialBackoffRetry(t *testing.T) {
	// Replace the global sleeper with a no-op for testing speed
	originalSleeper := sleeper
	mockSleep := &trackingSleeper{}
	sleeper = mockSleep
	defer func() { sleeper = originalSleeper }()

	rapid.Check(t, func(t *rapid.T) {
		// Generate retry configuration
		maxAttempts := rapid.IntRange(1, 10).Draw(t, "maxAttempts")
		initialDelayMs := rapid.IntRange(100, 2000).Draw(t, "initialDelayMs")
		maxDelayMs := rapid.IntRange(initialDelayMs*2, initialDelayMs*100).Draw(t, "maxDelayMs")
		backoffFactor := rapid.Float64Range(1.5, 4.0).Draw(t, "backoffFactor")

		config := RetryConfig{
			InitialDelay:  time.Duration(initialDelayMs) * time.Millisecond,
			BackoffFactor: backoffFactor,
			MaxDelay:      time.Duration(maxDelayMs) * time.Millisecond,
			MaxAttempts:   maxAttempts,
		}

		// Generate number of throttling errors before success
		// Choose either: succeed within maxAttempts, or exceed them
		exceedsMax := rapid.Bool().Draw(t, "exceedsMax")

		var numErrors int
		if exceedsMax {
			numErrors = maxAttempts // Will use all attempts as errors, no success
		} else {
			numErrors = rapid.IntRange(0, maxAttempts-1).Draw(t, "numErrors")
		}

		// Build mock responses
		responses := make([]*bedrockruntime.ConverseOutput, 0, numErrors+1)
		errs := make([]error, 0, numErrors+1)

		throttleErr := fmt.Errorf("ThrottlingException: Rate exceeded")
		for i := 0; i < numErrors; i++ {
			responses = append(responses, nil)
			errs = append(errs, throttleErr)
		}

		if !exceedsMax {
			// Add successful response after errors
			responses = append(responses, makeFinalTextResponse("success"))
			errs = append(errs, nil)
		}

		client := &mockBedrockClient{responses: responses, errors: errs}

		agent, err := NewAgent(client, "test-model")
		if err != nil {
			t.Fatalf("NewAgent failed: %v", err)
		}

		// Reset mock sleeper tracking
		mockSleep.reset()

		// Call converseWithRetry directly
		ctx := context.Background()
		input := &bedrockruntime.ConverseInput{
			ModelId:  strPtr("test-model"),
			Messages: []brtypes.Message{},
		}

		output, err := agent.converseWithRetry(ctx, input, config)

		if exceedsMax {
			// Should return error when retries are exceeded
			if err == nil {
				t.Fatal("expected error when max retries exceeded, got nil")
			}
			// Verify that we attempted maxAttempts times
			callCount := int(client.callIdx.Load())
			if callCount != maxAttempts {
				t.Fatalf("expected %d attempts, got %d", maxAttempts, callCount)
			}
		} else {
			// Should succeed after N retries
			if err != nil {
				t.Fatalf("expected success after %d retries, got error: %v", numErrors, err)
			}
			if output == nil {
				t.Fatal("expected non-nil output")
			}

			// Verify call count = numErrors + 1 (errors + final success)
			callCount := int(client.callIdx.Load())
			if callCount != numErrors+1 {
				t.Fatalf("expected %d calls (%d errors + 1 success), got %d", numErrors+1, numErrors, callCount)
			}

			// Verify delays are increasing (for numErrors > 1)
			delays := mockSleep.getDelays()
			if len(delays) != numErrors {
				t.Fatalf("expected %d sleep calls, got %d", numErrors, len(delays))
			}

			// Verify each delay is >= previous (monotonically non-decreasing due to cap)
			for i := 1; i < len(delays); i++ {
				if delays[i] < delays[i-1] {
					t.Fatalf("delays not non-decreasing: delay[%d]=%v < delay[%d]=%v",
						i, delays[i], i-1, delays[i-1])
				}
			}

			// Verify no delay exceeds MaxDelay
			for i, d := range delays {
				if d > config.MaxDelay {
					t.Fatalf("delay[%d]=%v exceeds MaxDelay=%v", i, d, config.MaxDelay)
				}
			}
		}
	})
}

// trackingSleeper records sleep calls without actually sleeping.
type trackingSleeper struct {
	delays []time.Duration
}

func (s *trackingSleeper) Sleep(d time.Duration) {
	s.delays = append(s.delays, d)
}

func (s *trackingSleeper) getDelays() []time.Duration {
	return s.delays
}

func (s *trackingSleeper) reset() {
	s.delays = nil
}

func strPtr(s string) *string {
	return &s
}
