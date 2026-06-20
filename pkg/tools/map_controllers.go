package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
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
//
// This function delegates to the generic framework.MapOne with a mapping config
// specific to the controller-to-Terraform-doc mapping.
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

	config := buildControllerMappingConfig()

	result, err := framework.MapOne(ctx, config, ag, controller, tfResources, resultCache, validator, l)
	if err != nil {
		return nil, err
	}

	return &result, nil
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
// with bounded concurrency. It delegates to the generic framework.MapAll with a
// mapping config specific to the controller-to-Terraform-doc mapping.
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

	config := buildControllerMappingConfig()

	frameworkResult, err := framework.MapAll(ctx, config, ag, controllers, tfResources, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type, preserving controller ordering
	output := &MapAllControllersOutput{
		Skipped: frameworkResult.Skipped,
	}

	for _, ctrl := range controllers {
		if mapping, ok := frameworkResult.Results[ctrl.ServiceName]; ok {
			output.Mappings = append(output.Mappings, mapping)
		}
	}

	return output, nil
}

// buildControllerMappingConfig returns the framework.MappingConfig for Controller-to-Terraform mapping.
func buildControllerMappingConfig() framework.MappingConfig[types.TerraformResourceInfo, types.ControllerMapping] {
	return framework.MappingConfig[types.TerraformResourceInfo, types.ControllerMapping]{
		ToolName:    mapControllersTool,
		BuildPrompt: buildMapControllerPrompt,
		ParseResult: parseControllerMappingResult,
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: buildMapInputParams,
	}
}

// parseControllerMappingResult parses the agent's JSON response into a ControllerMapping.
// The agent responds with a wrapper: {"mapping": {...}}, so we unwrap it here.
func parseControllerMappingResult(response string) (types.ControllerMapping, error) {
	var output MapControllersOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return types.ControllerMapping{}, fmt.Errorf("parsing controller mapping response: %w", err)
	}
	return output.Mapping, nil
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

	// Group TF resources by service name (derived from DocFilePath)
	tfByService := make(map[string][]types.TerraformResourceInfo)
	for _, tf := range tfResources {
		service, _, ok := ExtractTerraformFilenameComponents(filepath.Base(tf.DocFilePath))
		if !ok {
			continue
		}
		tfByService[service] = append(tfByService[service], tf)
	}

	// Build mappings only for controllers
	var mappings []types.ControllerMapping
	for _, serviceName := range controllerServiceNames {
		tfDocs := tfByService[serviceName]
		entries := make([]types.MappingEntry, 0, len(tfDocs))
		for _, tf := range tfDocs {
			_, resourceType, _ := ExtractTerraformFilenameComponents(filepath.Base(tf.DocFilePath))
			entries = append(entries, types.MappingEntry{
				TFResourceType: resourceType,
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
