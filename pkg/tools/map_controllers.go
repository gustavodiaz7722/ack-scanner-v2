package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const mapControllersTool = "map_controllers"

// MapControllersOutput is the agent's mapping for a single controller.
type MapControllersOutput struct {
	Mapping types.ControllerMapping `json:"mapping"`
}

// MapAllControllersOutput is the aggregated mapping result for all controllers.
type MapAllControllersOutput struct {
	Mappings []types.ControllerMapping `json:"mappings"`
	Skipped  []string                  `json:"skipped,omitempty"`
}

// MapController invokes the agent to map a single ACK controller to its
// corresponding Terraform documentation files. The prompt includes the
// controller's service name, resource kinds, and the full list of TF doc
// filenames for context.
func MapController(
	ctx context.Context,
	ag *agent.Agent,
	controller types.ControllerInfo,
	tfResources []types.TerraformResourceInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*types.ControllerMapping, error) {
	l := resolveLogger(log)

	// Check cache first
	inputParams := buildMapInputParams(controller, tfResources)
	if resultCache != nil {
		entry, err := resultCache.Get(mapControllersTool, controller.ServiceName, inputParams)
		if err == nil && entry != nil {
			var mapping types.ControllerMapping
			if err := json.Unmarshal(entry.Result, &mapping); err == nil {
				l.CacheHit(mapControllersTool + "/" + controller.ServiceName)
				return &mapping, nil
			}
		}
	}

	l.CacheMiss(mapControllersTool + "/" + controller.ServiceName)
	l.AgentCall("map_controllers", controller.ServiceName)

	// Build the prompt
	prompt := buildMapControllerPrompt(controller, tfResources)

	// Call the agent with validation
	result, err := ag.RunWithValidation(ctx, prompt, validator)
	if err != nil {
		l.Error("map_controllers agent call failed for %s: %v", controller.ServiceName, err)
		return nil, err
	}

	// Parse the response
	var output MapControllersOutput
	if err := json.Unmarshal([]byte(result.FinalResponse), &output); err != nil {
		l.Error("map_controllers failed to parse response for %s: %v", controller.ServiceName, err)
		return nil, fmt.Errorf("parsing agent response for controller %q: %w", controller.ServiceName, err)
	}

	// Cache the result
	if resultCache != nil {
		resultJSON, _ := json.Marshal(output.Mapping)
		if err := resultCache.Put(mapControllersTool, controller.ServiceName, inputParams, resultJSON); err != nil {
			l.Warn("map_controllers failed to cache result for %s: %v", controller.ServiceName, err)
		} else {
			l.Debug("map_controllers cached result for %s (%d TF doc mappings)", controller.ServiceName, len(output.Mapping.TFDocFiles))
		}
	}

	return &output.Mapping, nil
}

// MapAllControllers orchestrates mapping all controllers to Terraform docs.
// It uses bounded concurrency (controlled via maxParallel parameter) to process
// controllers in parallel. Pass maxParallel <= 1 for sequential execution.
func MapAllControllers(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	tfResources []types.TerraformResourceInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*MapAllControllersOutput, error) {
	return MapAllControllersParallel(ctx, ag, controllers, tfResources, resultCache, validator, 1, log...)
}

// MapAllControllersParallel orchestrates mapping all controllers to Terraform docs
// with bounded concurrency.
func MapAllControllersParallel(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	tfResources []types.TerraformResourceInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MapAllControllersOutput, error) {
	l := resolveLogger(log)
	output := &MapAllControllersOutput{}

	if maxParallel <= 0 {
		maxParallel = 1
	}

	l.Info("map_controllers: processing %d controllers against %d TF resources (parallelism: %d)",
		len(controllers), len(tfResources), maxParallel)

	type result struct {
		mapping *types.ControllerMapping
		skipped string
		err     error
	}

	total := len(controllers)
	results := make([]result, total)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var cacheHits, cacheMisses atomic.Int32

	for i, ctrl := range controllers {
		select {
		case <-ctx.Done():
			break
		default:
		}

		wg.Add(1)
		go func(idx int, controller types.ControllerInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check cache first (avoid agent call entirely)
			inputParams := buildMapInputParams(controller, tfResources)
			if resultCache != nil {
				entry, err := resultCache.Get(mapControllersTool, controller.ServiceName, inputParams)
				if err == nil && entry != nil {
					var mapping types.ControllerMapping
					if err := json.Unmarshal(entry.Result, &mapping); err == nil {
						l.CacheHit(mapControllersTool + "/" + controller.ServiceName)
						cacheHits.Add(1)
						results[idx] = result{mapping: &mapping}
						return
					}
				}
			}

			cacheMisses.Add(1)

			// Cache miss — call agent
			mapping, err := MapController(ctx, ag, controller, tfResources, resultCache, validator, l)
			if err != nil {
				if err == agent.ErrSkipItem {
					l.Skip(controller.ServiceName, "validation failed after retries")
					results[idx] = result{skipped: controller.ServiceName}
				} else {
					l.Error("mapping %s: %v", controller.ServiceName, err)
					results[idx] = result{err: err, skipped: controller.ServiceName}
				}
				return
			}
			results[idx] = result{mapping: mapping}
		}(i, ctrl)
	}

	wg.Wait()

	// Aggregate results in order
	for i, r := range results {
		if r.mapping != nil {
			output.Mappings = append(output.Mappings, *r.mapping)
		} else if r.skipped != "" {
			output.Skipped = append(output.Skipped, r.skipped)
		} else if r.err != nil {
			output.Skipped = append(output.Skipped, controllers[i].ServiceName)
		}
	}

	l.CacheSummary("map_controllers", int(cacheHits.Load()), int(cacheMisses.Load()), len(output.Skipped))

	return output, nil
}

// buildMapControllerPrompt constructs the prompt sent to the agent for mapping
// a single controller to Terraform documentation files.
func buildMapControllerPrompt(controller types.ControllerInfo, tfResources []types.TerraformResourceInfo) string {
	var sb strings.Builder

	sb.WriteString("You are mapping an ACK (AWS Controllers for Kubernetes) controller to its corresponding Terraform AWS provider documentation files.\n\n")

	sb.WriteString("## ACK Controller\n")
	sb.WriteString(fmt.Sprintf("Service Name: %s\n", controller.ServiceName))
	sb.WriteString("Resource Kinds:\n")
	for _, r := range controller.Resources {
		sb.WriteString(fmt.Sprintf("  - %s\n", r.Kind))
	}

	sb.WriteString("\n## Terraform Documentation Files\n")
	sb.WriteString("Below is the complete list of Terraform AWS provider resource documentation filenames:\n")
	for _, tf := range tfResources {
		sb.WriteString(fmt.Sprintf("  - %s\n", tf.DocFilePath))
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Map the ACK controller to the Terraform documentation files that correspond to the same AWS service and resources.\n")
	sb.WriteString("Use semantic understanding to resolve naming differences (e.g., 'applicationautoscaling' maps to 'appautoscaling' in Terraform).\n")
	sb.WriteString("If there is no corresponding Terraform documentation, leave terraform_doc_files as an empty array and provide a no_match_reason.\n")
	sb.WriteString("Only include Terraform resources that genuinely correspond to this ACK controller.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"mapping":{"service_name":"<the ACK controller service name>","terraform_doc_files":[{"terraform_resource_type":"<e.g. aws_appautoscaling_target>","doc_file_path":"<exact path from the list above>","confidence":<0.0 to 1.0>}],"no_match_reason":"<optional: reason if no TF docs match>"}}`)
	sb.WriteString("\n")

	return sb.String()
}

// FilterMappings produces controller mappings that only include TF resources
// matching the given controllers. Terraform resources whose service name does
// not correspond to any controller are excluded from the output. This function
// is used for deterministic validation that the property holds.
func FilterMappings(controllerServiceNames []string, tfResources []types.TerraformResourceInfo) []types.ControllerMapping {
	controllerSet := make(map[string]bool, len(controllerServiceNames))
	for _, name := range controllerServiceNames {
		controllerSet[name] = true
	}

	// Group TF resources by service name
	tfByService := make(map[string][]types.TerraformResourceInfo)
	for _, tf := range tfResources {
		tfByService[tf.ServiceName] = append(tfByService[tf.ServiceName], tf)
	}

	// Build mappings only for controllers
	var mappings []types.ControllerMapping
	for _, serviceName := range controllerServiceNames {
		tfDocs := tfByService[serviceName]
		entries := make([]types.MappingEntry, 0, len(tfDocs))
		for _, tf := range tfDocs {
			entries = append(entries, types.MappingEntry{
				TFResourceType: tf.ResourceType,
				DocFilePath:    tf.DocFilePath,
				Confidence:     1.0,
			})
		}

		noMatchReason := ""
		if len(entries) == 0 {
			noMatchReason = "No corresponding Terraform resources found"
		}

		mappings = append(mappings, types.ControllerMapping{
			ServiceName:   serviceName,
			TFDocFiles:    entries,
			NoMatchReason: noMatchReason,
		})
	}

	return mappings
}

// buildMapInputParams creates the input parameters used for cache hashing.
// It includes the controller's service name and resource kinds, plus a
// representation of the TF resources list for invalidation purposes.
func buildMapInputParams(controller types.ControllerInfo, tfResources []types.TerraformResourceInfo) map[string]interface{} {
	kinds := make([]string, 0, len(controller.Resources))
	for _, r := range controller.Resources {
		kinds = append(kinds, r.Kind)
	}

	tfDocs := make([]string, 0, len(tfResources))
	for _, tf := range tfResources {
		tfDocs = append(tfDocs, tf.DocFilePath)
	}

	return map[string]interface{}{
		"service_name":   controller.ServiceName,
		"resource_kinds": kinds,
		"tf_doc_count":   len(tfResources),
	}
}
