package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// MappingConfig describes how to map controllers to target items using an agent.
// T is the type of target items (e.g., UpjetConfigInfo, APIModelInfo).
// R is the type of the per-controller mapping result.
type MappingConfig[T any, R any] struct {
	// ToolName is the cache directory and logging prefix (e.g., "map_upjet").
	ToolName string
	// BuildPrompt constructs the agent prompt for a single controller.
	BuildPrompt func(controller types.ControllerInfo, targets []T) string
	// ParseResult deserializes the agent's JSON response into the result type.
	ParseResult func(response string) (R, error)
	// ItemKey returns the cache key for a controller (typically its ServiceName).
	ItemKey func(controller types.ControllerInfo) string
	// InputParams returns the input parameters for cache hashing.
	InputParams func(controller types.ControllerInfo, targets []T) map[string]any
}

// MappingResult holds the aggregated output of MapAll.
type MappingResult[R any] struct {
	// Results maps controller cache key to its mapping result.
	Results map[string]R
	// Skipped contains the keys of controllers that were skipped due to errors.
	Skipped []string
}

// MapAll orchestrates mapping all controllers to target items using an agent
// with bounded concurrency, caching, validation, and retry. Each controller
// is processed in a separate agent call.
func MapAll[T any, R any](
	ctx context.Context,
	config MappingConfig[T, R],
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	targets []T,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log *logger.Logger,
) (*MappingResult[R], error) {
	if log == nil {
		log = logger.Nop()
	}
	if maxParallel <= 0 {
		maxParallel = 1
	}

	log.Info("%s: processing %d controllers (parallelism: %d)", config.ToolName, len(controllers), maxParallel)

	type itemResult struct {
		key     string
		result  R
		skipped bool
	}

	total := len(controllers)
	results := make([]itemResult, total)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var cacheHits, cacheMisses, completed atomic.Int32

	for i, ctrl := range controllers {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(idx int, controller types.ControllerInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			key := config.ItemKey(controller)
			inputParams := config.InputParams(controller, targets)

			// Check cache
			if resultCache != nil {
				entry, err := resultCache.Get(config.ToolName, key, inputParams)
				if err == nil && entry != nil {
					var r R
					if err := json.Unmarshal(entry.Result, &r); err == nil {
						log.CacheHit(config.ToolName + "/" + key)
						cacheHits.Add(1)
						done := int(completed.Add(1))
						log.Progress(done, total, "%s", config.ToolName)
						results[idx] = itemResult{key: key, result: r}
						return
					}
				}
			}

			cacheMisses.Add(1)
			log.CacheMiss(config.ToolName + "/" + key)
			log.AgentCall(config.ToolName, key)

			// Build prompt and call agent
			prompt := config.BuildPrompt(controller, targets)
			agentResult, err := ag.RunWithValidation(ctx, prompt, validator)
			if err != nil {
				if err == agent.ErrSkipItem {
					log.Skip(key, "validation failed after retries")
				} else {
					log.Error("%s agent call failed for %s: %v", config.ToolName, key, err)
				}
				done := int(completed.Add(1))
				log.Progress(done, total, "%s", config.ToolName)
				results[idx] = itemResult{key: key, skipped: true}
				return
			}

			// Parse response
			r, err := config.ParseResult(agentResult.FinalResponse)
			if err != nil {
				log.Error("%s failed to parse response for %s: %v", config.ToolName, key, err)
				done := int(completed.Add(1))
				log.Progress(done, total, "%s", config.ToolName)
				results[idx] = itemResult{key: key, skipped: true}
				return
			}

			// Store in cache
			if resultCache != nil {
				resultJSON, _ := json.Marshal(r)
				if err := resultCache.Put(config.ToolName, key, inputParams, resultJSON); err != nil {
					log.Warn("%s failed to cache result for %s: %v", config.ToolName, key, err)
				}
			}

			done := int(completed.Add(1))
			log.Progress(done, total, "%s", config.ToolName)
			results[idx] = itemResult{key: key, result: r}
		}(i, ctrl)
	}

	wg.Wait()

	// Aggregate results in order
	output := &MappingResult[R]{
		Results: make(map[string]R, total),
	}
	for _, r := range results {
		if r.skipped {
			output.Skipped = append(output.Skipped, r.key)
		} else if r.key != "" {
			output.Results[r.key] = r.result
		}
	}

	log.CacheSummary(config.ToolName, int(cacheHits.Load()), int(cacheMisses.Load()), len(output.Skipped))

	return output, nil
}

// MapOne maps a single controller to targets. It uses the same config as MapAll
// but processes only one controller. Useful for per-item CLI subcommands.
func MapOne[T any, R any](
	ctx context.Context,
	config MappingConfig[T, R],
	ag *agent.Agent,
	controller types.ControllerInfo,
	targets []T,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log *logger.Logger,
) (R, error) {
	if log == nil {
		log = logger.Nop()
	}

	key := config.ItemKey(controller)
	inputParams := config.InputParams(controller, targets)

	// Check cache
	if resultCache != nil {
		entry, err := resultCache.Get(config.ToolName, key, inputParams)
		if err == nil && entry != nil {
			var r R
			if err := json.Unmarshal(entry.Result, &r); err == nil {
				log.CacheHit(config.ToolName + "/" + key)
				return r, nil
			}
		}
	}

	log.CacheMiss(config.ToolName + "/" + key)
	log.AgentCall(config.ToolName, key)

	prompt := config.BuildPrompt(controller, targets)
	agentResult, err := ag.RunWithValidation(ctx, prompt, validator)
	if err != nil {
		var zero R
		return zero, fmt.Errorf("%s agent call failed for %s: %w", config.ToolName, key, err)
	}

	r, err := config.ParseResult(agentResult.FinalResponse)
	if err != nil {
		var zero R
		return zero, fmt.Errorf("%s failed to parse response for %s: %w", config.ToolName, key, err)
	}

	// Store in cache
	if resultCache != nil {
		resultJSON, _ := json.Marshal(r)
		if err := resultCache.Put(config.ToolName, key, inputParams, resultJSON); err != nil {
			log.Warn("%s failed to cache result for %s: %v", config.ToolName, key, err)
		}
	}

	return r, nil
}
