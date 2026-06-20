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

// MatchConfig describes how to match source data against ACK resource fields.
// T is the type of source data items (e.g., UpjetReferenceInfo, ModelReferenceInfo).
// R is the type of the per-resource match result.
type MatchConfig[T any, R any] struct {
	// ToolName is the cache directory and logging prefix (e.g., "match_upjet").
	ToolName string
	// BuildPrompt constructs the agent prompt for a single resource.
	BuildPrompt func(resource types.ResourceInfo, sourceFields []T, serviceName string) string
	// ParseResult deserializes the agent's JSON response into the result type.
	ParseResult func(response string) (R, error)
	// ItemKey returns the cache key for a resource (typically "service_kind").
	ItemKey func(serviceName string, resource types.ResourceInfo) string
	// InputParams returns the input parameters for cache hashing.
	InputParams func(resource types.ResourceInfo, sourceFields []T, serviceName string) map[string]any
	// FilterFields optionally filters the resource's string fields before matching.
	// If nil, all fields are passed to the prompt.
	FilterFields func(fields []types.FieldInfo) []types.FieldInfo
}

// MatchItem represents a single resource to be matched.
type MatchItem[T any] struct {
	Controller   types.ControllerInfo
	Resource     types.ResourceInfo
	SourceFields []T
}

// MatchResult holds the aggregated output of MatchAll.
type MatchResult[R any] struct {
	// Results maps resource cache key to its match result.
	Results map[string]R
	// Skipped contains the keys of resources that were skipped due to errors.
	Skipped []string
}

// MatchAll orchestrates matching all resources across all controllers against
// source data using an agent with bounded concurrency, caching, validation,
// and retry. Each resource is processed in a separate agent call.
//
// sourceData maps service name → list of source items for that service.
// serviceMappings maps service name → list of source keys (used to look up
// which source items apply to which controller).
func MatchAll[T any, R any](
	ctx context.Context,
	config MatchConfig[T, R],
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	sourceData map[string][]T,
	serviceMappings map[string][]string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log *logger.Logger,
) (*MatchResult[R], error) {
	if log == nil {
		log = logger.Nop()
	}
	if maxParallel <= 0 {
		maxParallel = 1
	}

	// Build flat list of items to process
	var items []MatchItem[T]
	for _, controller := range controllers {
		// Get source data for this controller
		var controllerSourceData []T
		if serviceMappings != nil {
			// Use service mappings to look up source data by mapped keys
			mappedKeys := serviceMappings[controller.ServiceName]
			for _, key := range mappedKeys {
				if data, ok := sourceData[key]; ok {
					controllerSourceData = append(controllerSourceData, data...)
				}
			}
		} else {
			// Direct lookup by service name
			if data, ok := sourceData[controller.ServiceName]; ok {
				controllerSourceData = data
			}
		}

		if len(controllerSourceData) == 0 {
			log.Debug("%s: skipping controller %s (no source data)", config.ToolName, controller.ServiceName)
			continue
		}

		for _, resource := range controller.Resources {
			items = append(items, MatchItem[T]{
				Controller:   controller,
				Resource:     resource,
				SourceFields: controllerSourceData,
			})
		}
	}

	log.Info("%s: processing %d resources across %d controllers (parallelism: %d)",
		config.ToolName, len(items), len(controllers), maxParallel)

	type itemResult struct {
		key     string
		result  R
		skipped bool
	}

	total := len(items)
	results := make([]itemResult, total)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var cacheHits, cacheMisses, completed atomic.Int32

	for i, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(idx int, mi MatchItem[T]) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			key := config.ItemKey(mi.Controller.ServiceName, mi.Resource)

			// Apply field filtering if configured
			resource := mi.Resource
			if config.FilterFields != nil {
				resource.StringFields = config.FilterFields(resource.StringFields)
			}

			inputParams := config.InputParams(resource, mi.SourceFields, mi.Controller.ServiceName)

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
			log.AgentCall(config.ToolName, mi.Controller.ServiceName+"/"+mi.Resource.Kind)

			// Build prompt and call agent
			prompt := config.BuildPrompt(resource, mi.SourceFields, mi.Controller.ServiceName)
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
		}(i, item)
	}

	wg.Wait()

	// Aggregate results in order
	output := &MatchResult[R]{
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

// MatchOne matches a single resource against source data. Useful for per-item
// CLI subcommands.
func MatchOne[T any, R any](
	ctx context.Context,
	config MatchConfig[T, R],
	ag *agent.Agent,
	resource types.ResourceInfo,
	sourceFields []T,
	serviceName string,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log *logger.Logger,
) (R, error) {
	if log == nil {
		log = logger.Nop()
	}

	// Apply field filtering if configured
	if config.FilterFields != nil {
		resource.StringFields = config.FilterFields(resource.StringFields)
	}

	key := config.ItemKey(serviceName, resource)
	inputParams := config.InputParams(resource, sourceFields, serviceName)

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
	log.AgentCall(config.ToolName, serviceName+"/"+resource.Kind)

	prompt := config.BuildPrompt(resource, sourceFields, serviceName)
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
