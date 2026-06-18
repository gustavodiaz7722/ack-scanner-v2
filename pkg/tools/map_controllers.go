package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
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
) (*types.ControllerMapping, error) {
	// Check cache first
	inputParams := buildMapInputParams(controller, tfResources)
	if resultCache != nil {
		entry, err := resultCache.Get(mapControllersTool, controller.ServiceName, inputParams)
		if err == nil && entry != nil {
			var mapping types.ControllerMapping
			if err := json.Unmarshal(entry.Result, &mapping); err == nil {
				return &mapping, nil
			}
		}
	}

	// Build the prompt
	prompt := buildMapControllerPrompt(controller, tfResources)

	// Call the agent with validation
	result, err := ag.RunWithValidation(ctx, prompt, validator)
	if err != nil {
		return nil, err
	}

	// Parse the response
	var output MapControllersOutput
	if err := json.Unmarshal([]byte(result.FinalResponse), &output); err != nil {
		return nil, fmt.Errorf("parsing agent response for controller %q: %w", controller.ServiceName, err)
	}

	// Cache the result
	if resultCache != nil {
		resultJSON, _ := json.Marshal(output.Mapping)
		_ = resultCache.Put(mapControllersTool, controller.ServiceName, inputParams, resultJSON)
	}

	return &output.Mapping, nil
}

// MapAllControllers orchestrates mapping all controllers to Terraform docs.
// It iterates over each controller, checks the cache, calls the agent for
// cache misses, and aggregates all mapping results.
func MapAllControllers(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	tfResources []types.TerraformResourceInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
) (*MapAllControllersOutput, error) {
	output := &MapAllControllersOutput{}

	for _, controller := range controllers {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		default:
		}

		// Check cache
		inputParams := buildMapInputParams(controller, tfResources)
		if resultCache != nil {
			entry, err := resultCache.Get(mapControllersTool, controller.ServiceName, inputParams)
			if err == nil && entry != nil {
				var mapping types.ControllerMapping
				if err := json.Unmarshal(entry.Result, &mapping); err == nil {
					output.Mappings = append(output.Mappings, mapping)
					continue
				}
			}
		}

		// Cache miss — call agent
		mapping, err := MapController(ctx, ag, controller, tfResources, resultCache, validator)
		if err != nil {
			if err == agent.ErrSkipItem {
				output.Skipped = append(output.Skipped, controller.ServiceName)
				continue
			}
			return output, fmt.Errorf("mapping controller %q: %w", controller.ServiceName, err)
		}

		output.Mappings = append(output.Mappings, *mapping)
	}

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

	sb.WriteString("## Required Output JSON Schema\n")
	sb.WriteString("Respond with ONLY valid JSON matching this exact schema:\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{
  "mapping": {
    "service_name": "<the ACK controller service name>",
    "terraform_doc_files": [
      {
        "terraform_resource_type": "<e.g. aws_appautoscaling_target>",
        "doc_file_path": "<exact path from the list above>",
        "confidence": <0.0 to 1.0>
      }
    ],
    "no_match_reason": "<optional: reason if no TF docs match>"
  }
}
`)
	sb.WriteString("```\n")

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
