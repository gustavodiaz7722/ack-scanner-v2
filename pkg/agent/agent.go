// Package agent implements the AWS Bedrock Converse API agent loop for ack-scanner-v2.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// DefaultMaxTurns is the maximum number of conversation turns before the agent
// loop terminates with an error.
const DefaultMaxTurns = 20

// ErrMaxTurnsExceeded is returned when the agent loop exceeds the maximum number of turns.
var ErrMaxTurnsExceeded = errors.New("agent: maximum conversation turns exceeded")

// ErrMissingCredentials is returned when AWS credentials are not configured.
var ErrMissingCredentials = errors.New("agent: AWS credentials not configured. " +
	"Please configure credentials via environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), " +
	"AWS profiles (~/.aws/credentials), or IAM role. " +
	"Required permissions: bedrock:InvokeModel on the target model resource")

// BedrockClient abstracts the AWS Bedrock Converse API for testability.
type BedrockClient interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// ToolDefinition describes a tool the agent can invoke.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolRegistry maps tool names to their execution functions.
type ToolRegistry struct {
	tools map[string]ToolFunc
}

// NewToolRegistry creates a new empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolFunc),
	}
}

// Register adds a tool function to the registry.
func (r *ToolRegistry) Register(name string, fn ToolFunc) {
	r.tools[name] = fn
}

// Get retrieves a tool function by name.
func (r *ToolRegistry) Get(name string) (ToolFunc, bool) {
	fn, ok := r.tools[name]
	return fn, ok
}

// ToolFunc is the signature for a tool execution function.
type ToolFunc func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// ToolCall represents a single tool invocation during the agent loop.
type ToolCall struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

// AgentResult contains the final response and metadata from the agent loop.
type AgentResult struct {
	FinalResponse     string     `json:"final_response"`
	ToolCalls         []ToolCall `json:"tool_calls"`
	TotalTokens       int        `json:"total_tokens"`
	ValidationRetries int        `json:"validation_retries"`
}

// Agent orchestrates tool-use conversations with AWS Bedrock.
type Agent struct {
	client   BedrockClient
	modelID  string
	tools    []ToolDefinition
	registry *ToolRegistry
	maxTurns int
}

// AgentOption is a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithMaxTurns sets the maximum number of conversation turns.
func WithMaxTurns(n int) AgentOption {
	return func(a *Agent) {
		if n > 0 {
			a.maxTurns = n
		}
	}
}

// WithTools sets the tool definitions for the agent.
func WithTools(tools []ToolDefinition) AgentOption {
	return func(a *Agent) {
		a.tools = tools
	}
}

// WithRegistry sets the tool registry for the agent.
func WithRegistry(r *ToolRegistry) AgentOption {
	return func(a *Agent) {
		a.registry = r
	}
}

// NewAgent creates a new Agent with the given Bedrock client and options.
// It returns an error if credentials are missing (client is nil).
func NewAgent(client BedrockClient, modelID string, opts ...AgentOption) (*Agent, error) {
	if client == nil {
		return nil, ErrMissingCredentials
	}
	if modelID == "" {
		return nil, fmt.Errorf("agent: modelID must not be empty")
	}

	a := &Agent{
		client:   client,
		modelID:  modelID,
		maxTurns: DefaultMaxTurns,
		registry: NewToolRegistry(),
	}

	for _, opt := range opts {
		opt(a)
	}

	return a, nil
}

// Run executes the agent loop: sends the initial prompt, processes tool-use
// requests, executes tools, returns results, and continues until the model
// produces a final text response or maxTurns is exceeded.
func (a *Agent) Run(ctx context.Context, prompt string) (*AgentResult, error) {
	result := &AgentResult{}

	// Build tool configuration for Bedrock
	toolConfig := a.buildToolConfig()

	// Start the conversation with the user's prompt
	messages := []brtypes.Message{
		{
			Role: brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: prompt},
			},
		},
	}

	retryConfig := DefaultRetryConfig()

	for turn := 0; turn < a.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		input := &bedrockruntime.ConverseInput{
			ModelId:  &a.modelID,
			Messages: messages,
		}
		if toolConfig != nil {
			input.ToolConfig = toolConfig
		}

		// Call Bedrock with retry logic for throttling
		output, err := a.converseWithRetry(ctx, input, retryConfig)
		if err != nil {
			return result, fmt.Errorf("agent: Converse API call failed: %w", err)
		}

		// Accumulate token usage
		if output.Usage != nil && output.Usage.TotalTokens != nil {
			result.TotalTokens += int(*output.Usage.TotalTokens)
		}

		// Check stop reason
		if output.StopReason == brtypes.StopReasonEndTurn || output.StopReason == brtypes.StopReasonMaxTokens || output.StopReason == brtypes.StopReasonStopSequence {
			// Extract final text response
			finalText := extractTextFromOutput(output)
			result.FinalResponse = finalText
			return result, nil
		}

		if output.StopReason == brtypes.StopReasonToolUse {
			// Extract assistant message and add to conversation
			assistantMsg := extractAssistantMessage(output)
			messages = append(messages, assistantMsg)

			// Execute tool calls and build tool result message
			toolResultMsg, toolCalls, err := a.executeToolCalls(ctx, assistantMsg)
			if err != nil {
				return result, fmt.Errorf("agent: tool execution failed: %w", err)
			}
			result.ToolCalls = append(result.ToolCalls, toolCalls...)
			messages = append(messages, toolResultMsg)
			continue
		}

		// Unknown stop reason — treat as end
		finalText := extractTextFromOutput(output)
		result.FinalResponse = finalText
		return result, nil
	}

	// Max turns exceeded
	return result, ErrMaxTurnsExceeded
}

// buildToolConfig converts ToolDefinitions to the Bedrock API tool configuration.
func (a *Agent) buildToolConfig() *brtypes.ToolConfiguration {
	if len(a.tools) == 0 {
		return nil
	}

	tools := make([]brtypes.Tool, 0, len(a.tools))
	for _, td := range a.tools {
		name := td.Name
		desc := td.Description
		var inputSchema brtypes.ToolInputSchema

		if len(td.InputSchema) > 0 {
			// Parse the JSON schema into a generic interface for the document
			var schemaMap interface{}
			if err := json.Unmarshal(td.InputSchema, &schemaMap); err == nil {
				inputSchema = &brtypes.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(schemaMap),
				}
			}
		}

		if inputSchema == nil {
			// Default to empty object schema
			inputSchema = &brtypes.ToolInputSchemaMemberJson{
				Value: document.NewLazyDocument(map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				}),
			}
		}

		tools = append(tools, &brtypes.ToolMemberToolSpec{
			Value: brtypes.ToolSpecification{
				Name:        &name,
				Description: &desc,
				InputSchema: inputSchema,
			},
		})
	}

	return &brtypes.ToolConfiguration{
		Tools: tools,
	}
}

// extractTextFromOutput extracts the text content from a Converse response.
func extractTextFromOutput(output *bedrockruntime.ConverseOutput) string {
	if output == nil {
		return ""
	}

	msgOutput, ok := output.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok || msgOutput == nil {
		return ""
	}

	for _, block := range msgOutput.Value.Content {
		if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
			return textBlock.Value
		}
	}
	return ""
}

// extractAssistantMessage builds a Message from the Converse output.
func extractAssistantMessage(output *bedrockruntime.ConverseOutput) brtypes.Message {
	msgOutput, ok := output.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok || msgOutput == nil {
		return brtypes.Message{
			Role:    brtypes.ConversationRoleAssistant,
			Content: []brtypes.ContentBlock{},
		}
	}
	return msgOutput.Value
}

// executeToolCalls processes tool use blocks from the assistant message,
// executes each tool, and returns the tool result message.
func (a *Agent) executeToolCalls(ctx context.Context, assistantMsg brtypes.Message) (brtypes.Message, []ToolCall, error) {
	var resultBlocks []brtypes.ContentBlock
	var toolCalls []ToolCall

	for _, block := range assistantMsg.Content {
		toolUseBlock, ok := block.(*brtypes.ContentBlockMemberToolUse)
		if !ok {
			continue
		}

		toolName := ""
		if toolUseBlock.Value.Name != nil {
			toolName = *toolUseBlock.Value.Name
		}
		toolUseID := ""
		if toolUseBlock.Value.ToolUseId != nil {
			toolUseID = *toolUseBlock.Value.ToolUseId
		}

		// Marshal the tool input to JSON
		var inputJSON json.RawMessage
		if toolUseBlock.Value.Input != nil {
			inputBytes, err := toolUseBlock.Value.Input.MarshalSmithyDocument()
			if err != nil {
				inputJSON = []byte("{}")
			} else {
				inputJSON = inputBytes
			}
		} else {
			inputJSON = []byte("{}")
		}

		// Look up and execute the tool
		toolFn, exists := a.registry.Get(toolName)
		var outputJSON json.RawMessage
		var toolErr error

		if !exists {
			toolErr = fmt.Errorf("unknown tool: %s", toolName)
		} else {
			outputJSON, toolErr = toolFn(ctx, inputJSON)
		}

		tc := ToolCall{
			Name:   toolName,
			Input:  inputJSON,
			Output: outputJSON,
		}
		toolCalls = append(toolCalls, tc)

		// Build tool result content block
		var resultContent []brtypes.ToolResultContentBlock
		var status brtypes.ToolResultStatus

		if toolErr != nil {
			resultContent = append(resultContent, &brtypes.ToolResultContentBlockMemberText{
				Value: fmt.Sprintf("Error: %s", toolErr.Error()),
			})
			status = brtypes.ToolResultStatusError
		} else {
			resultContent = append(resultContent, &brtypes.ToolResultContentBlockMemberText{
				Value: string(outputJSON),
			})
			status = brtypes.ToolResultStatusSuccess
		}

		resultBlocks = append(resultBlocks, &brtypes.ContentBlockMemberToolResult{
			Value: brtypes.ToolResultBlock{
				ToolUseId: &toolUseID,
				Content:   resultContent,
				Status:    status,
			},
		})
	}

	toolResultMsg := brtypes.Message{
		Role:    brtypes.ConversationRoleUser,
		Content: resultBlocks,
	}

	return toolResultMsg, toolCalls, nil
}
