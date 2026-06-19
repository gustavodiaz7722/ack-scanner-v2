package tools

import "github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"

// DefaultMaxParallel is the default number of concurrent agent calls.
const DefaultMaxParallel = 3

// RunConfig holds options for tool execution (parallelism, logging).
type RunConfig struct {
	MaxParallel int
	Log         *logger.Logger
}

// DefaultRunConfig returns a RunConfig with default settings.
func DefaultRunConfig() RunConfig {
	return RunConfig{
		MaxParallel: 1,
		Log:         logger.Nop(),
	}
}

// resolveLogger returns the first logger from the variadic slice, or a no-op
// logger if none is provided. This allows tool functions to accept an optional
// logger parameter while maintaining backward compatibility.
func resolveLogger(logs []*logger.Logger) *logger.Logger {
	if len(logs) > 0 && logs[0] != nil {
		return logs[0]
	}
	return logger.Nop()
}
