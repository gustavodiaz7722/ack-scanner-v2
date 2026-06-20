package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const mapModelsTool = "map_models"

// ModelMapping maps an ACK controller to its corresponding AWS Smithy API model file.
type ModelMapping struct {
	ServiceName   string  `json:"service_name"`
	ModelFile     string  `json:"model_file"`
	Confidence    float64 `json:"confidence"`
	NoMatchReason string  `json:"no_match_reason,omitempty"`
}

// MapModelsOutput is the agent's mapping response for a single controller.
type MapModelsOutput struct {
	Mapping ModelMapping `json:"mapping"`
}

// MapAllModelsOutput is the aggregated mapping result for all controllers.
type MapAllModelsOutput struct {
	Mappings []ModelMapping `json:"mappings"`
	Skipped  []string       `json:"skipped,omitempty"`
}

// MapControllerToModel invokes the agent to map a single ACK controller to its
// corresponding AWS Smithy API model file. The prompt includes the controller's
// service name, resource kinds, and the full list of API model filenames.
func MapControllerToModel(
	ctx context.Context,
	ag *agent.Agent,
	controller types.ControllerInfo,
	models []APIModelInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*ModelMapping, error) {
	l := resolveLogger(log)

	config := buildMapModelsConfig()
	result, err := framework.MapOne(ctx, config, ag, controller, models, resultCache, validator, l)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// MapAllControllersToModels orchestrates mapping all controllers to API model files
// with bounded concurrency.
func MapAllControllersToModels(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	models []APIModelInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MapAllModelsOutput, error) {
	l := resolveLogger(log)

	config := buildMapModelsConfig()
	mappingResult, err := framework.MapAll(ctx, config, ag, controllers, models, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	output := &MapAllModelsOutput{
		Skipped: mappingResult.Skipped,
	}

	// Collect results preserving controller order
	for _, ctrl := range controllers {
		key := ctrl.ServiceName
		if result, ok := mappingResult.Results[key]; ok {
			output.Mappings = append(output.Mappings, result)
		}
	}

	return output, nil
}

// buildMapModelsConfig creates the MappingConfig for controller-to-API-model mapping.
func buildMapModelsConfig() framework.MappingConfig[APIModelInfo, ModelMapping] {
	return framework.MappingConfig[APIModelInfo, ModelMapping]{
		ToolName:    mapModelsTool,
		BuildPrompt: buildMapModelPrompt,
		ParseResult: parseMapModelResult,
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: buildMapModelInputParams,
	}
}

// buildMapModelPrompt constructs the prompt sent to the agent for mapping a
// single controller to its corresponding AWS API model file.
func buildMapModelPrompt(controller types.ControllerInfo, models []APIModelInfo) string {
	var sb strings.Builder

	sb.WriteString("You are mapping an ACK (AWS Controllers for Kubernetes) controller to its corresponding AWS Smithy API model file.\n\n")

	sb.WriteString("## ACK Controller\n")
	fmt.Fprintf(&sb, "Service Name: %s\n", controller.ServiceName)
	sb.WriteString("Resource Kinds:\n")
	for _, r := range controller.Resources {
		fmt.Fprintf(&sb, "  - %s\n", r.Kind)
	}

	sb.WriteString("\n## AWS API Model Files\n")
	sb.WriteString("Below is the complete list of AWS Smithy API model filenames (service names extracted from the filename):\n")
	for _, m := range models {
		fmt.Fprintf(&sb, "  - %s (service: %s)\n", m.FilePath, m.ServiceName)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Map the ACK controller to the single AWS API model file that corresponds to the same AWS service.\n")
	sb.WriteString("This is often a 1:1 mapping (e.g., ACK 'elasticache' → model 'elasticache.json').\n")
	sb.WriteString("However, some names differ between ACK and the model files. Use semantic understanding to resolve these differences:\n")
	sb.WriteString("  - ACK 'applicationautoscaling' → model 'application-auto-scaling.json'\n")
	sb.WriteString("  - ACK 'sfn' → model 'sfn.json'\n")
	sb.WriteString("  - ACK 'sagemaker' → model 'sagemaker.json'\n")
	sb.WriteString("If there is no corresponding API model file, set model_file to an empty string and provide a no_match_reason.\n")
	sb.WriteString("Only pick a single model file — the one that best matches this ACK controller.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"mapping":{"service_name":"<the ACK controller service name>","model_file":"<exact file path from the list above, or empty string if no match>","confidence":<0.0 to 1.0>,"no_match_reason":"<optional: reason if no model matches>"}}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseMapModelResult deserializes the agent's JSON response into a ModelMapping.
func parseMapModelResult(response string) (ModelMapping, error) {
	var output MapModelsOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return ModelMapping{}, fmt.Errorf("parsing agent response: %w", err)
	}
	return output.Mapping, nil
}

// buildMapModelInputParams creates the input parameters used for cache hashing.
func buildMapModelInputParams(controller types.ControllerInfo, models []APIModelInfo) map[string]any {
	kinds := make([]string, 0, len(controller.Resources))
	for _, r := range controller.Resources {
		kinds = append(kinds, r.Kind)
	}

	return map[string]any{
		"service_name":   controller.ServiceName,
		"resource_kinds": kinds,
		"model_count":    len(models),
	}
}
