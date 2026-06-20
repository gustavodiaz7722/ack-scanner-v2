package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrSkipItem is a sentinel error indicating the item should be skipped
// after all validation retry attempts have been exhausted.
var ErrSkipItem = errors.New("agent: validation failed after max retries, skipping item")

// SkipError wraps ErrSkipItem with details about why validation failed.
// Callers can use errors.Is(err, ErrSkipItem) to detect skips, and
// type-assert to *SkipError for the validation error and response snippet.
type SkipError struct {
	Cause          error  // the validation error (JSON parse failure, reference error, etc.)
	ResponsePrefix string // first N chars of the raw agent response
}

func (e *SkipError) Error() string {
	return fmt.Sprintf("validation failed: %v (response prefix: %.200s)", e.Cause, e.ResponsePrefix)
}

func (e *SkipError) Unwrap() error {
	return ErrSkipItem
}

// newSkipError creates a SkipError with a truncated response preview.
func newSkipError(cause error, rawResponse string) *SkipError {
	prefix := rawResponse
	if len(prefix) > 500 {
		prefix = prefix[:500] + "..."
	}
	return &SkipError{
		Cause:          cause,
		ResponsePrefix: prefix,
	}
}

// ResponseValidator validates an agent's JSON response.
type ResponseValidator interface {
	// ValidateJSON checks that the response is valid JSON.
	ValidateJSON(response []byte) error
	// ValidateReferences checks that controller/resource names in the response
	// exist in the known set.
	ValidateReferences(response []byte) error
}

// JSONValidator validates that a response is parseable as valid JSON.
// Optionally checks that required top-level fields are present.
type JSONValidator struct {
	RequiredFields []string
}

// ValidateJSON checks that the response is valid JSON. If RequiredFields is set,
// it also checks that those fields are present as top-level keys.
func (v *JSONValidator) ValidateJSON(response []byte) error {
	if !json.Valid(response) {
		return fmt.Errorf("response is not valid JSON")
	}

	if len(v.RequiredFields) > 0 {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(response, &obj); err != nil {
			return fmt.Errorf("response is not a JSON object: %w", err)
		}
		for _, field := range v.RequiredFields {
			if _, ok := obj[field]; !ok {
				return fmt.Errorf("required field %q missing from response", field)
			}
		}
	}

	return nil
}

// ValidateReferences is a no-op for JSONValidator since it only validates structure.
func (v *JSONValidator) ValidateReferences(response []byte) error {
	return nil
}

// KnownReferences contains the set of valid controller service names and
// the valid resource kinds per controller.
type KnownReferences struct {
	// Controllers is the set of valid controller service names.
	Controllers map[string]bool
	// ResourcesByController maps controller service name to a set of valid resource Kinds.
	ResourcesByController map[string]map[string]bool
}

// ReferenceValidator validates that controller/resource names in agent responses
// exist in the known set of discovered controllers and resources.
type ReferenceValidator struct {
	Known          KnownReferences
	RequiredFields []string
}

// ValidateJSON checks that the response is valid JSON. If RequiredFields is set,
// it also checks that those fields are present as top-level keys.
func (v *ReferenceValidator) ValidateJSON(response []byte) error {
	if !json.Valid(response) {
		return fmt.Errorf("response is not valid JSON")
	}

	if len(v.RequiredFields) > 0 {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(response, &obj); err != nil {
			return fmt.Errorf("response is not a JSON object: %w", err)
		}
		for _, field := range v.RequiredFields {
			if _, ok := obj[field]; !ok {
				return fmt.Errorf("required field %q missing from response", field)
			}
		}
	}

	return nil
}

// ValidateReferences extracts service_name and resource kind fields from the
// JSON response and checks they exist in the known set.
func (v *ReferenceValidator) ValidateReferences(response []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(response, &obj); err != nil {
		// If response is not a JSON object, skip reference validation
		return nil
	}

	// Check service_name if present
	if raw, ok := obj["service_name"]; ok {
		var serviceName string
		if err := json.Unmarshal(raw, &serviceName); err == nil && serviceName != "" {
			if !v.Known.Controllers[serviceName] {
				validNames := v.validControllerNames()
				return fmt.Errorf("unknown controller service name %q; valid names are: %s",
					serviceName, validNames)
			}

			// Check resource kind if present (validated against this controller)
			if kindRaw, ok := obj["kind"]; ok {
				var kind string
				if err := json.Unmarshal(kindRaw, &kind); err == nil && kind != "" {
					resources := v.Known.ResourcesByController[serviceName]
					if resources != nil && !resources[kind] {
						validKinds := v.validResourceKinds(serviceName)
						return fmt.Errorf("unknown resource kind %q for controller %q; valid kinds are: %s",
							kind, serviceName, validKinds)
					}
				}
			}
		}
	}

	// Also check if response contains a mapping with nested service_name references
	if raw, ok := obj["mapping"]; ok {
		var mapping map[string]json.RawMessage
		if err := json.Unmarshal(raw, &mapping); err == nil {
			if snRaw, ok := mapping["service_name"]; ok {
				var serviceName string
				if err := json.Unmarshal(snRaw, &serviceName); err == nil && serviceName != "" {
					if !v.Known.Controllers[serviceName] {
						validNames := v.validControllerNames()
						return fmt.Errorf("unknown controller service name %q in mapping; valid names are: %s",
							serviceName, validNames)
					}
				}
			}
		}
	}

	return nil
}

// validControllerNames returns a comma-separated list of valid controller names.
func (v *ReferenceValidator) validControllerNames() string {
	names := make([]string, 0, len(v.Known.Controllers))
	for name := range v.Known.Controllers {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// validResourceKinds returns a comma-separated list of valid resource kinds for a controller.
func (v *ReferenceValidator) validResourceKinds(serviceName string) string {
	resources := v.Known.ResourcesByController[serviceName]
	if resources == nil {
		return "(none)"
	}
	kinds := make([]string, 0, len(resources))
	for kind := range resources {
		kinds = append(kinds, kind)
	}
	return strings.Join(kinds, ", ")
}

// ValidationConfig controls retry behavior for invalid responses.
type ValidationConfig struct {
	MaxRetries int // Default: 2 (3 total attempts)
	Verbose    bool
	LogWriter  io.Writer
}

// DefaultValidationConfig returns a ValidationConfig with default settings.
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MaxRetries: 2,
		Verbose:    false,
		LogWriter:  os.Stderr,
	}
}

// RunWithValidation executes the agent loop with response validation.
// After receiving the final response, it validates JSON structure and static
// references. On failure, retries up to MaxRetries times (default 2, for 3 total
// attempts) with the error fed back to the model. After all retries are exhausted,
// returns ErrSkipItem.
func (a *Agent) RunWithValidation(ctx context.Context, prompt string, validator ResponseValidator, opts ...func(*ValidationConfig)) (*AgentResult, error) {
	config := DefaultValidationConfig()
	for _, opt := range opts {
		opt(&config)
	}

	maxAttempts := config.MaxRetries + 1 // 3 total by default
	currentPrompt := prompt

	for attempt := range maxAttempts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := a.Run(ctx, currentPrompt)
		if err != nil {
			return nil, err
		}

		// Validate the response
		responseBytes := []byte(result.FinalResponse)

		// Check JSON validity
		if jsonErr := validator.ValidateJSON(responseBytes); jsonErr != nil {
			if attempt < maxAttempts-1 {
				if config.Verbose && config.LogWriter != nil {
					fmt.Fprintf(config.LogWriter, "[validation] attempt %d/%d failed: %v\n",
						attempt+1, maxAttempts, jsonErr)
				}
				// Build retry prompt with error context
				currentPrompt = buildRetryPrompt(prompt, jsonErr.Error(), "")
				result.ValidationRetries++
				continue
			}
			// All attempts exhausted
			if config.Verbose && config.LogWriter != nil {
				fmt.Fprintf(config.LogWriter, "[validation] all %d attempts failed (JSON): %v; raw response: %s\n",
					maxAttempts, jsonErr, result.FinalResponse)
			}
			return nil, newSkipError(jsonErr, result.FinalResponse)
		}

		// Check reference validity
		if refErr := validator.ValidateReferences(responseBytes); refErr != nil {
			if attempt < maxAttempts-1 {
				if config.Verbose && config.LogWriter != nil {
					fmt.Fprintf(config.LogWriter, "[validation] attempt %d/%d failed: %v\n",
						attempt+1, maxAttempts, refErr)
				}
				// Build retry prompt with error context and valid names
				currentPrompt = buildRetryPrompt(prompt, refErr.Error(), "")
				result.ValidationRetries++
				continue
			}
			// All attempts exhausted
			if config.Verbose && config.LogWriter != nil {
				fmt.Fprintf(config.LogWriter, "[validation] all %d attempts failed (references): %v; raw response: %s\n",
					maxAttempts, refErr, result.FinalResponse)
			}
			return nil, newSkipError(refErr, result.FinalResponse)
		}

		// Both validations passed
		result.ValidationRetries = attempt
		return result, nil
	}

	// Should not reach here, but safety net
	return nil, ErrSkipItem
}

// buildRetryPrompt constructs a new prompt that includes the original prompt,
// the validation error, and optionally the list of valid names.
func buildRetryPrompt(originalPrompt, validationError, validNames string) string {
	var sb strings.Builder
	sb.WriteString(originalPrompt)
	sb.WriteString("\n\n---\nYour previous response was invalid. Error: ")
	sb.WriteString(validationError)
	if validNames != "" {
		sb.WriteString("\n\nValid names: ")
		sb.WriteString(validNames)
	}
	sb.WriteString("\n\nPlease provide a corrected response.")
	return sb.String()
}

// WithVerbose sets verbose logging on the ValidationConfig.
func WithVerbose(verbose bool) func(*ValidationConfig) {
	return func(c *ValidationConfig) {
		c.Verbose = verbose
	}
}

// WithLogWriter sets the log writer on the ValidationConfig.
func WithLogWriter(w io.Writer) func(*ValidationConfig) {
	return func(c *ValidationConfig) {
		c.LogWriter = w
	}
}

// WithMaxValidationRetries sets the max retries on the ValidationConfig.
func WithMaxValidationRetries(n int) func(*ValidationConfig) {
	return func(c *ValidationConfig) {
		if n >= 0 {
			c.MaxRetries = n
		}
	}
}
