package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// RetryConfig controls exponential backoff retry behavior for transient errors.
type RetryConfig struct {
	InitialDelay  time.Duration
	BackoffFactor float64
	MaxDelay      time.Duration
	MaxAttempts   int
}

// DefaultRetryConfig returns sensible defaults for retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		InitialDelay:  1 * time.Second,
		BackoffFactor: 2.0,
		MaxDelay:      30 * time.Second,
		MaxAttempts:   5,
	}
}

// NextDelay computes the delay for the given attempt number (0-indexed) using
// exponential backoff, capped at MaxDelay.
func NextDelay(attempt int, config RetryConfig) time.Duration {
	if attempt <= 0 {
		return config.InitialDelay
	}

	delay := config.InitialDelay
	for i := 0; i < attempt; i++ {
		delay = time.Duration(float64(delay) * config.BackoffFactor)
		if delay > config.MaxDelay {
			return config.MaxDelay
		}
	}
	return delay
}

// ShouldRetry checks if an error is a throttling/rate limit error that should
// be retried.
func ShouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Check for HTTP response errors with throttling status codes
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case http.StatusTooManyRequests, // 429
			http.StatusServiceUnavailable: // 503
			return true
		}
	}

	// Check error message for common throttling indicators
	errMsg := err.Error()
	throttlingIndicators := []string{
		"ThrottlingException",
		"TooManyRequestsException",
		"RequestLimitExceeded",
		"Throttling",
		"Rate exceeded",
		"rate limit",
	}
	for _, indicator := range throttlingIndicators {
		if strings.Contains(errMsg, indicator) {
			return true
		}
	}

	return false
}

// Sleeper is an interface for sleeping, enabling testing without real delays.
type Sleeper interface {
	Sleep(d time.Duration)
}

// RealSleeper implements Sleeper using time.Sleep.
type RealSleeper struct{}

// Sleep pauses the current goroutine for at least the duration d.
func (RealSleeper) Sleep(d time.Duration) {
	time.Sleep(d)
}

// sleeper is the package-level sleeper used by converseWithRetry.
// It can be replaced in tests.
var sleeper Sleeper = RealSleeper{}

// converseWithRetry wraps the Converse API call with exponential backoff retry
// for throttling errors.
func (a *Agent) converseWithRetry(ctx context.Context, input *bedrockruntime.ConverseInput, config RetryConfig) (*bedrockruntime.ConverseOutput, error) {
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		output, err := a.client.Converse(ctx, input)
		if err == nil {
			return output, nil
		}

		lastErr = err

		// Only retry throttling errors
		if !ShouldRetry(err) {
			return nil, err
		}

		// Don't sleep after the last attempt
		if attempt < config.MaxAttempts-1 {
			delay := NextDelay(attempt, config)
			sleeper.Sleep(delay)
		}
	}

	return nil, fmt.Errorf("agent: max retry attempts (%d) exceeded: %w", config.MaxAttempts, lastErr)
}
