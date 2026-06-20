package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"pgregory.net/rapid"
)

// validationMockClient allows configuring per-call responses for validation tests.
type validationMockClient struct {
	responseFn func(callIdx int, prompt string) (*bedrockruntime.ConverseOutput, error)
	callIdx    atomic.Int32
}

func (m *validationMockClient) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	idx := int(m.callIdx.Add(1)) - 1

	// Extract prompt text from messages for retry detection
	prompt := ""
	if len(params.Messages) > 0 {
		for _, block := range params.Messages[0].Content {
			if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
				prompt = textBlock.Value
				break
			}
		}
	}

	return m.responseFn(idx, prompt)
}

// Property 17: JSON validation retry
// **Validates: Requirements 11.1, 11.2, 11.3**
//
// For any agent response that is not valid JSON, the system SHALL retry the request
// up to 2 additional times (3 total attempts). If all 3 attempts produce invalid JSON,
// the item SHALL be skipped and an error logged. If any attempt produces valid JSON,
// that response SHALL be accepted.
func TestProperty17_JSONValidationRetry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate the attempt at which valid JSON is produced (0-indexed)
		// 0 = first attempt succeeds, 1 = second attempt, 2 = third attempt
		// 3 = all attempts fail (exceed 3 total)
		successAttempt := rapid.IntRange(0, 3).Draw(t, "successAttempt")

		// Generate a valid JSON response for the success case
		validServiceName := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "validServiceName")
		validJSON := fmt.Sprintf(`{"service_name":"%s","status":"ok"}`, validServiceName)

		// Generate invalid JSON responses (random non-JSON strings)
		invalidResponse := rapid.StringMatching(`[a-zA-Z0-9 ]{5,50}`).Draw(t, "invalidResponse")
		// Ensure it's actually not valid JSON
		if json.Valid([]byte(invalidResponse)) {
			invalidResponse = "not-json{{{" + invalidResponse
		}

		callCount := &atomic.Int32{}

		client := &validationMockClient{
			responseFn: func(callIdx int, prompt string) (*bedrockruntime.ConverseOutput, error) {
				callCount.Add(1)
				if callIdx < successAttempt {
					// Return invalid JSON
					return makeFinalTextResponse(invalidResponse), nil
				}
				// Return valid JSON
				return makeFinalTextResponse(validJSON), nil
			},
		}

		agent, err := NewAgent(client, "test-model")
		if err != nil {
			t.Fatalf("NewAgent failed: %v", err)
		}

		validator := &JSONValidator{}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := agent.RunWithValidation(ctx, "test prompt", validator)

		if successAttempt <= 2 {
			// Should succeed — valid JSON produced within 3 attempts
			if err != nil {
				t.Fatalf("expected success at attempt %d, got error: %v", successAttempt, err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.FinalResponse != validJSON {
				t.Fatalf("expected response %q, got %q", validJSON, result.FinalResponse)
			}
			// Verify the number of calls: successAttempt+1 (0-indexed)
			expectedCalls := int32(successAttempt + 1)
			actualCalls := callCount.Load()
			if actualCalls != expectedCalls {
				t.Fatalf("expected %d calls, got %d", expectedCalls, actualCalls)
			}
		} else {
			// All 3 attempts failed — should return ErrSkipItem
			if err == nil {
				t.Fatal("expected ErrSkipItem when all attempts fail, got nil error")
			}
			if !errors.Is(err, ErrSkipItem) {
				t.Fatalf("expected ErrSkipItem, got: %v", err)
			}
			// Should have made exactly 3 calls
			actualCalls := callCount.Load()
			if actualCalls != 3 {
				t.Fatalf("expected 3 total calls, got %d", actualCalls)
			}
		}
	})
}

// Property 18: Static reference validation
// **Validates: Requirements 11.4, 11.5, 11.6, 11.7**
//
// For any agent response containing a controller service name or resource kind,
// validation SHALL pass if and only if the name exists in the discovered controller/resource
// set. On validation failure, the system SHALL retry up to 2 additional times with the
// valid names included in the prompt. If all 3 attempts fail, the item SHALL be skipped.
func TestProperty18_StaticReferenceValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of known controller names (2 to 5 controllers)
		numControllers := rapid.IntRange(2, 5).Draw(t, "numControllers")
		knownControllers := make(map[string]bool, numControllers)
		knownResources := make(map[string]map[string]bool, numControllers)
		controllerNames := make([]string, 0, numControllers)

		for i := range numControllers {
			name := fmt.Sprintf("svc%d", i)
			knownControllers[name] = true
			controllerNames = append(controllerNames, name)

			// Each controller has 1-3 resources
			numResources := rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("numResources_%d", i))
			resources := make(map[string]bool, numResources)
			for j := range numResources {
				kind := fmt.Sprintf("Resource%d%d", i, j)
				resources[kind] = true
			}
			knownResources[name] = resources
		}

		// Choose whether the response will contain valid or invalid references
		validRefAtAttempt := rapid.IntRange(0, 3).Draw(t, "validRefAtAttempt")
		// 0 = first attempt has valid refs, 1 = second, 2 = third, 3 = all invalid

		// Pick a valid controller name to use in the successful response
		validControllerIdx := rapid.IntRange(0, numControllers-1).Draw(t, "validControllerIdx")
		validControllerName := controllerNames[validControllerIdx]

		// Pick a valid resource kind for that controller
		validKinds := make([]string, 0)
		for kind := range knownResources[validControllerName] {
			validKinds = append(validKinds, kind)
		}
		validKind := validKinds[0]

		// Construct valid and invalid responses
		validResponse := fmt.Sprintf(`{"service_name":"%s","kind":"%s","data":"result"}`,
			validControllerName, validKind)
		invalidResponse := fmt.Sprintf(`{"service_name":"%s","kind":"%s","data":"result"}`,
			"nonexistent_service", "NonexistentKind")

		callCount := &atomic.Int32{}
		retryPromptContainedValidNames := &atomic.Int32{}

		client := &validationMockClient{
			responseFn: func(callIdx int, prompt string) (*bedrockruntime.ConverseOutput, error) {
				callCount.Add(1)
				// Check if retry prompt contains valid names hint
				if callIdx > 0 {
					for _, name := range controllerNames {
						if len(prompt) > 0 && contains(prompt, name) {
							retryPromptContainedValidNames.Add(1)
							break
						}
					}
				}
				if callIdx < validRefAtAttempt {
					return makeFinalTextResponse(invalidResponse), nil
				}
				return makeFinalTextResponse(validResponse), nil
			},
		}

		agent, err := NewAgent(client, "test-model")
		if err != nil {
			t.Fatalf("NewAgent failed: %v", err)
		}

		validator := &ReferenceValidator{
			Known: KnownReferences{
				Controllers:           knownControllers,
				ResourcesByController: knownResources,
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := agent.RunWithValidation(ctx, "test prompt", validator)

		if validRefAtAttempt <= 2 {
			// Should succeed — valid references produced within 3 attempts
			if err != nil {
				t.Fatalf("expected success at attempt %d, got error: %v", validRefAtAttempt, err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.FinalResponse != validResponse {
				t.Fatalf("expected response %q, got %q", validResponse, result.FinalResponse)
			}

			// Verify call count
			expectedCalls := int32(validRefAtAttempt + 1)
			actualCalls := callCount.Load()
			if actualCalls != expectedCalls {
				t.Fatalf("expected %d calls, got %d", expectedCalls, actualCalls)
			}

			// Verify retry prompts contain valid names (for retries > 0)
			if validRefAtAttempt > 0 {
				retryCount := retryPromptContainedValidNames.Load()
				if retryCount == 0 {
					t.Fatal("expected retry prompts to contain valid names, but none did")
				}
			}
		} else {
			// All 3 attempts failed — should return ErrSkipItem
			if err == nil {
				t.Fatal("expected ErrSkipItem when all attempts fail, got nil error")
			}
			if !errors.Is(err, ErrSkipItem) {
				t.Fatalf("expected ErrSkipItem, got: %v", err)
			}
			// Should have made exactly 3 calls
			actualCalls := callCount.Load()
			if actualCalls != 3 {
				t.Fatalf("expected 3 total calls, got %d", actualCalls)
			}
		}
	})
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
