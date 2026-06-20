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
)

// FileToAnalyze represents a file that needs agent-powered analysis.
type FileToAnalyze struct {
	// Key is the cache key for this file (e.g., the service name or resource type).
	Key string
	// FilePath is the path to the file (used for logging/prompt building).
	FilePath string
	// Content is the file's text content to be analyzed.
	Content string
}

// AnalysisConfig describes how to analyze files using an agent.
// R is the type of the per-file analysis result.
type AnalysisConfig[R any] struct {
	// ToolName is the cache directory and logging prefix (e.g., "analyze_upjet").
	ToolName string
	// BuildPrompt constructs the agent prompt for a single file.
	BuildPrompt func(file FileToAnalyze) string
	// ParseResult deserializes the agent's JSON response into the result type.
	ParseResult func(response string) (R, error)
	// InputParams returns the input parameters for cache hashing.
	InputParams func(file FileToAnalyze) map[string]any
}

// AnalysisResult holds the aggregated output of AnalyzeAll.
type AnalysisResult[R any] struct {
	// Results maps file cache key to its analysis result.
	Results map[string]R
	// Skipped contains the keys of files that were skipped due to errors.
	Skipped []string
}

// AnalyzeAll orchestrates analyzing all files using an agent with bounded
// concurrency, caching, validation, and retry. Each file is processed in
// a separate agent call.
func AnalyzeAll[R any](
	ctx context.Context,
	config AnalysisConfig[R],
	ag *agent.Agent,
	files []FileToAnalyze,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log *logger.Logger,
) (*AnalysisResult[R], error) {
	if log == nil {
		log = logger.Nop()
	}
	if maxParallel <= 0 {
		maxParallel = 1
	}

	log.Info("%s: processing %d files (parallelism: %d)", config.ToolName, len(files), maxParallel)

	type itemResult struct {
		key     string
		result  R
		skipped bool
	}

	total := len(files)
	results := make([]itemResult, total)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var cacheHits, cacheMisses atomic.Int32

	for i, file := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(idx int, f FileToAnalyze) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			key := f.Key
			inputParams := config.InputParams(f)

			// Check cache
			if resultCache != nil {
				entry, err := resultCache.Get(config.ToolName, key, inputParams)
				if err == nil && entry != nil {
					var r R
					if err := json.Unmarshal(entry.Result, &r); err == nil {
						log.CacheHit(config.ToolName + "/" + key)
						cacheHits.Add(1)
						results[idx] = itemResult{key: key, result: r}
						return
					}
				}
			}

			cacheMisses.Add(1)
			log.CacheMiss(config.ToolName + "/" + key)
			log.AgentCall(config.ToolName, key)

			// Build prompt and call agent
			prompt := config.BuildPrompt(f)
			agentResult, err := ag.RunWithValidation(ctx, prompt, validator)
			if err != nil {
				if err == agent.ErrSkipItem {
					log.Skip(key, "validation failed after retries")
				} else {
					log.Error("%s agent call failed for %s: %v", config.ToolName, key, err)
				}
				results[idx] = itemResult{key: key, skipped: true}
				return
			}

			// Parse response
			r, err := config.ParseResult(agentResult.FinalResponse)
			if err != nil {
				log.Error("%s failed to parse response for %s: %v", config.ToolName, key, err)
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

			results[idx] = itemResult{key: key, result: r}
		}(i, file)
	}

	wg.Wait()

	// Aggregate results in order
	output := &AnalysisResult[R]{
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

// AnalyzeOne analyzes a single file. Useful for per-item CLI subcommands.
func AnalyzeOne[R any](
	ctx context.Context,
	config AnalysisConfig[R],
	ag *agent.Agent,
	file FileToAnalyze,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log *logger.Logger,
) (R, error) {
	if log == nil {
		log = logger.Nop()
	}

	key := file.Key
	inputParams := config.InputParams(file)

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

	prompt := config.BuildPrompt(file)
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
